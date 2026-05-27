#!/usr/bin/env python3
"""
多轮对话 E2E 测评：POST /chat 走完整 Agent 链路（改写→路由→分析→计划→执行→成稿）。

  conda activate langchain_agent
  python scripts/run_dialogue_metrics_eval.py
  python scripts/run_dialogue_metrics_eval.py --token <JWT>
  # Windows 也可: M:\\conda_envs\\langchain_agent\\python.exe scripts/run_dialogue_metrics_eval.py
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

import yaml

AGENT_DIR = Path(__file__).resolve().parents[1]
if str(AGENT_DIR) not in sys.path:
    sys.path.insert(0, str(AGENT_DIR))

from eval.scorer import aggregate_batch, score_turn  # noqa: E402
from eval.sse_collector import SseTurnCollector  # noqa: E402

CASES_PATH = AGENT_DIR / "cases" / "dialogue_metrics_cases.yaml"
METRICS_PATH = AGENT_DIR / "eval" / "metrics_spec.yaml"


def _load_yaml(path: Path) -> dict:
    with open(path, "r", encoding="utf-8") as f:
        return yaml.safe_load(f) or {}


def _normalize_turn(turn: str | dict) -> tuple[str, dict, bool]:
    """(message, per_turn_expect, require_token)。"""
    if isinstance(turn, str):
        return turn.strip(), {}, False
    if isinstance(turn, dict):
        return (
            str(turn.get("message", "")).strip(),
            dict(turn.get("expect") or {}),
            bool(turn.get("require_token")),
        )
    return "", {}, False


def _chat_turn(
    url: str,
    *,
    message: str,
    user_id: int,
    chat_session_id: str,
    token: str,
    timeout: int,
) -> dict:
    body = json.dumps(
        {
            "message": message,
            "user_id": user_id,
            "token": token or "",
            "chat_session_id": chat_session_id,
            "history_owned_by_gateway": False,
        }
    ).encode()
    req = urllib.request.Request(
        url,
        data=body,
        headers={"Content-Type": "application/json", "Accept": "text/event-stream"},
    )
    col = SseTurnCollector(user_message=message)
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        for raw in resp:
            col.observe_line(raw.decode("utf-8", errors="replace"))
    return col.finalize()


def _flatten_turns(blob: dict) -> list[dict]:
    """按 session 顺序展平全部 turn，保留 session 元数据。"""
    defaults = blob.get("defaults") or {}
    user_id = int(defaults.get("user_id") or 88001)
    base_expect = dict(defaults.get("expect") or {})
    flat: list[dict] = []
    global_idx = 0
    for sess in blob.get("sessions") or []:
        sid = str(sess.get("id", "?"))
        sname = str(sess.get("name", sid))
        csid = str(sess.get("chat_session_id") or sid)
        cap = int(sess.get("max_total_ms") or 60000)
        kb_expects: list[dict] = sess.get("kb_expectations") or []
        raw_turns = sess.get("turns") or []
        parsed: list[tuple[str, dict, bool]] = []
        for t in raw_turns:
            msg, texp, need_tok = _normalize_turn(t)
            if msg:
                parsed.append((msg, texp, need_tok))
        for i, (msg, turn_expect, need_token) in enumerate(parsed, 1):
            global_idx += 1
            expect = dict(base_expect)
            expect.update(turn_expect)
            expect["max_total_ms"] = cap
            if kb_expects and (i - 1) < len(kb_expects):
                ekb = kb_expects[i - 1]
                if isinstance(ekb, dict) and ekb.get("expected_sections"):
                    expect["kb_expected_sections"] = ekb["expected_sections"]
            flat.append({
                "global_index": global_idx,
                "session_id": sid,
                "session_name": sname,
                "chat_session_id": csid,
                "turn_index": i,
                "session_turn_count": len(parsed),
                "message": msg,
                "expect": expect,
                "require_token": need_token,
                "case_id": f"{sid}_t{i}",
            })
    flat.sort(key=lambda x: x["global_index"])
    return flat


def _merge_batch_reports(report_dir: Path, thresholds: dict) -> dict:
    results: list[dict] = []
    batch_files = sorted(report_dir.glob("dialogue_batch_*.json"))
    for fp in batch_files:
        data = json.loads(fp.read_text(encoding="utf-8"))
        results.extend(data.get("results") or [])
    results.sort(key=lambda r: (r.get("global_index") or 0, r.get("case_id", "")))
    runnable = [r for r in results if not r.get("skipped")]
    agg_all = aggregate_batch(runnable, thresholds)
    agg_all["skipped_turns"] = len(results) - len(runnable)
    agg_all["batch_files"] = [str(p.name) for p in batch_files]
    agg_all["total_turns"] = len(results)

    by_session: dict[str, list[dict]] = {}
    for r in results:
        sid = str(r.get("session_id", ""))
        by_session.setdefault(sid, []).append(r)
    session_summaries = []
    for sid, sess_results in by_session.items():
        agg_sess = aggregate_batch([x for x in sess_results if not x.get("skipped")], thresholds)
        agg_sess["session_id"] = sid
        agg_sess["session_name"] = sess_results[0].get("session_id", sid) if sess_results else sid
        agg_sess["turn_count"] = len(sess_results)
        session_summaries.append(agg_sess)
    agg_all["sessions"] = session_summaries

    tok_list = [
        int((r.get("metrics", {}).get("usage") or {}).get("total_tokens") or 0)
        for r in results
        if isinstance(r.get("metrics", {}).get("usage"), dict)
    ]
    if tok_list:
        agg_all["usage_total_tokens_sum"] = sum(tok_list)
        agg_all["usage_total_tokens_avg"] = int(round(sum(tok_list) / len(tok_list)))
    return {"aggregate": agg_all, "results": results}


def _parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="多轮对话 E2E（15 短 + 15 长，完整链路 + 工具 + KB RAG）")
    default_url = os.getenv("AGENT_URL", "http://127.0.0.1:9091").rstrip("/")
    if not default_url.endswith("/chat"):
        default_url = default_url + "/chat"
    p.add_argument("--url", default=default_url)
    p.add_argument("--token", default=os.getenv("AGENT_EVAL_TOKEN", ""))
    p.add_argument("--timeout", type=int, default=180)
    p.add_argument("--out", default=str(AGENT_DIR / "eval_reports" / "dialogue_metrics.json"))
    p.add_argument("--batch-size", type=int, default=5, help="每批轮数；0 表示一次跑完全部")
    p.add_argument("--batch", type=int, default=0, help="批次序号 1..N；0 表示跑全部（忽略 batch-size）")
    p.add_argument("--merge", action="store_true", help="合并 eval_reports/dialogue_batch_*.json 并汇总")
    return p.parse_args()


def main() -> int:
    args = _parse_args()
    spec = _load_yaml(METRICS_PATH)
    thresholds = spec.get("thresholds") or {}
    report_dir = AGENT_DIR / "eval_reports"

    if args.merge:
        merged = _merge_batch_reports(report_dir, thresholds)
        agg_all = merged["aggregate"]
        results = merged["results"]
        out_path = report_dir / "dialogue_metrics_final.json"
        out_path.write_text(json.dumps(merged, ensure_ascii=False, indent=2), encoding="utf-8")
        print(f"=== 合并 {len(merged['aggregate'].get('batch_files', []))} 个批次，共 {len(results)} 轮 ===")
        print(
            f"断言通过率 {agg_all.get('case_pass_rate', 0):.1%}  "
            f"任务完成率 {agg_all.get('task_completion_rate', 0):.1%}  "
            f"平均耗时 {agg_all.get('avg_total_ms', 0)}ms  "
            f"P95 {agg_all.get('total_ms_p95', 0)}ms"
        )
        if agg_all.get("kb_cases"):
            print(
                f"KB RAG: Recall@k={agg_all.get('kb_mean_recall_at_k', 0):.2%}  "
                f"MRR={agg_all.get('kb_mean_mrr', 0):.4f}  "
                f"通过率={agg_all.get('kb_pass_rate', 0):.1%}"
            )
        if agg_all.get("pipeline_pass_rate") is not None:
            print(f"完整链路通过率: {agg_all.get('pipeline_pass_rate', 0):.1%}")
        if agg_all.get("usage_total_tokens_sum"):
            print(
                f"Token 合计 {agg_all.get('usage_total_tokens_sum')}  "
                f"均值 {agg_all.get('usage_total_tokens_avg')}"
            )
        print(f"\n汇总报告: {out_path}")
        print(f"批次判定: {'PASS' if agg_all.get('batch_pass') else 'FAIL'}")
        return 0 if agg_all.get("batch_pass") else 1

    url = args.url if args.url.endswith("/chat") else args.url.rstrip("/") + "/chat"
    token = (args.token or "").strip()

    blob = _load_yaml(CASES_PATH)
    user_id = int((blob.get("defaults") or {}).get("user_id") or 88001)
    flat = _flatten_turns(blob)
    total = len(flat)

    if args.batch and args.batch_size > 0:
        batch_idx = args.batch
        start = (batch_idx - 1) * args.batch_size
        end = min(start + args.batch_size, total)
        if start >= total:
            print(f"批次 {batch_idx} 超出范围（共 {total} 轮）")
            return 2
        work = flat[start:end]
        batch_total = (total + args.batch_size - 1) // args.batch_size
        out_path = Path(args.out) if args.out != str(AGENT_DIR / "eval_reports" / "dialogue_metrics.json") else (
            report_dir / f"dialogue_batch_{batch_idx:02d}.json"
        )
        print(f"=== 批次 {batch_idx}/{batch_total}：全局第 {start + 1}-{end} 轮 / 共 {total} 轮 ===")
    elif args.batch_size > 0 and not args.batch:
        print("请指定 --batch N（配合 --batch-size 5 分批跑）")
        return 2
    else:
        work = flat
        out_path = Path(args.out)

    all_results: list[dict] = []

    print(f"Agent URL: {url}")
    print(f"user_id={user_id}  Token: {'yes' if token else 'no'}\n")

    for item in work:
        gidx = item["global_index"]
        sid = item["session_id"]
        csid = item["chat_session_id"]
        i = item["turn_index"]
        n_turns = item["session_turn_count"]
        msg = item["message"]
        expect = item["expect"]
        need_token = item["require_token"]
        label = item["case_id"]

        if need_token and not token:
            print(f"  [G{gidx} {sid} {i}/{n_turns}] SKIP (需要 --token): {msg[:40]}…")
            rec = {
                "global_index": gidx,
                "case_id": label,
                "session_id": sid,
                "turn_index": i,
                "message": msg,
                "passed": False,
                "skipped": True,
                "failures": ["missing token"],
                "metrics": {},
            }
            all_results.append(rec)
            continue

        turn_token = token if need_token or token else ""
        print(f"  [G{gidx}/{total} {sid} {i}/{n_turns}] {msg[:48]}{'…' if len(msg) > 48 else ''} … ", end="", flush=True)
        t0 = time.perf_counter()
        try:
            turn = _chat_turn(
                url,
                message=msg,
                user_id=user_id,
                chat_session_id=csid,
                token=turn_token,
                timeout=args.timeout,
            )
            scored = score_turn(turn, expect, has_token=bool(turn_token))
            usage = turn.get("usage")
            if isinstance(usage, dict) and usage:
                scored["metrics"]["usage"] = usage
            scored["global_index"] = gidx
            scored["case_id"] = label
            scored["session_id"] = sid
            scored["turn_index"] = i
            scored["message"] = msg
            scored["chat_session_id"] = csid
            elapsed = int((time.perf_counter() - t0) * 1000)
            scored["wall_ms"] = elapsed
            all_results.append(scored)
            m = scored["metrics"]
            mark = "PASS" if scored["passed"] else "FAIL"
            kb_info = ""
            if m.get("kb_recall_at_k") is not None:
                kb_info = f" kb_R={m['kb_recall_at_k']:.1%} MRR={m['kb_mrr']:.3f}"
            pipe_info = ""
            if m.get("pipeline_ok") is not None:
                pipe_info = f" pipe={'Y' if m.get('pipeline_ok') else 'N'}"
            tok = (m.get("usage") or turn.get("usage") or {})
            total_tok = tok.get("total_tokens", "-") if isinstance(tok, dict) else "-"
            print(
                mark,
                f" total_ms={m.get('timing_total_ms')} wall={elapsed}ms "
                f"tools={m.get('tool_call_count')} tokens={total_tok}{kb_info}{pipe_info}",
            )
            if scored["failures"]:
                for f in scored["failures"]:
                    print(f"      FAIL: {f}")
        except urllib.error.URLError as e:
            print(f"ERROR {e}")
            return 1

    runnable = [r for r in all_results if not r.get("skipped")]
    agg = aggregate_batch(runnable, thresholds)
    tok_list = [
        int((r.get("metrics", {}).get("usage") or {}).get("total_tokens") or 0)
        for r in all_results
        if isinstance(r.get("metrics", {}).get("usage"), dict)
    ]
    if tok_list:
        agg["usage_total_tokens_sum"] = sum(tok_list)
        agg["usage_total_tokens_avg"] = int(round(sum(tok_list) / len(tok_list)))

    print(f"\n=== 本批 {len(all_results)} 轮汇总 ===")
    print(
        f"通过率 {agg.get('case_pass_rate', 0):.1%}  "
        f"任务完成 {agg.get('task_completion_rate', 0):.1%}  "
        f"avg_ms={agg.get('avg_total_ms')} p95={agg.get('total_ms_p95')}"
    )
    if agg.get("kb_cases"):
        print(
            f"KB: recall={agg.get('kb_mean_recall_at_k', 0):.1%} "
            f"mrr={agg.get('kb_mean_mrr', 0):.3f}"
        )

    out_path.parent.mkdir(parents=True, exist_ok=True)
    payload = {"batch": args.batch or None, "aggregate": agg, "results": all_results}
    out_path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"\n本批报告: {out_path}")
    if args.batch:
        print("全部跑完后执行: python scripts/run_dialogue_metrics_eval.py --merge")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
