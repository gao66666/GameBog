#!/usr/bin/env python3
"""
Agent 直连测评：跑 turn_cases.yaml，从 SSE fact 聚合 turn，按 metrics_spec 判定。

用法（仓库根或 agent 目录）:
  python agent/scripts/run_agent_eval.py
  python agent/scripts/run_agent_eval.py --url http://127.0.0.1:9091/chat --token <JWT>
  python agent/scripts/run_agent_eval.py --case general_time,public_search
  python agent/scripts/run_agent_eval.py --out eval_result.json
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path

import yaml

AGENT_DIR = Path(__file__).resolve().parents[1]
if str(AGENT_DIR) not in sys.path:
    sys.path.insert(0, str(AGENT_DIR))

from eval.scorer import aggregate_batch, score_turn  # noqa: E402
from eval.sse_collector import SseTurnCollector  # noqa: E402

CASES_PATH = AGENT_DIR / "cases" / "turn_cases.yaml"
METRICS_PATH = AGENT_DIR / "eval" / "metrics_spec.yaml"


def _load_yaml(path: Path) -> dict:
    with open(path, "r", encoding="utf-8") as f:
        return yaml.safe_load(f) or {}


def _chat_collect(url: str, message: str, token: str, timeout: int) -> dict:
    body = json.dumps({"message": message, "token": token or ""}).encode()
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


def _parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Agent 直连测评")
    default_url = os.getenv("AGENT_URL", "http://127.0.0.1:9091").rstrip("/")
    if not default_url.endswith("/chat"):
        default_url = default_url + "/chat"
    p.add_argument("--url", default=default_url)
    p.add_argument("--token", default=os.getenv("AGENT_EVAL_TOKEN", ""))
    p.add_argument("--case", default="", help="逗号分隔 case id，默认全部")
    p.add_argument("--timeout", type=int, default=120)
    p.add_argument("--out", default="", help="JSON 报告路径")
    return p.parse_args()


def main() -> int:
    args = _parse_args()
    url = args.url
    if not url.endswith("/chat"):
        url = url.rstrip("/") + "/chat"

    spec = _load_yaml(METRICS_PATH)
    thresholds = spec.get("thresholds") or {}
    case_blob = _load_yaml(CASES_PATH)
    defaults = case_blob.get("defaults") or {}
    cases = case_blob.get("cases") or []

    filter_ids = {x.strip() for x in args.case.split(",") if x.strip()} if args.case else None
    if filter_ids:
        cases = [c for c in cases if c.get("id") in filter_ids]

    token = (args.token or "").strip()
    results: list[dict] = []

    print(f"Agent URL: {url}")
    print(f"Cases: {len(cases)}  Token: {'yes' if token else 'no'}\n")

    for c in cases:
        cid = c.get("id", "?")
        name = c.get("name", cid)
        msg = str(c.get("message", "")).strip()
        need_token = bool(c.get("require_token"))
        if need_token and not token:
            print(f"[SKIP] {cid} ({name}) — 需要 --token")
            results.append({
                "case_id": cid,
                "passed": False,
                "skipped": True,
                "failures": ["missing token"],
                "metrics": {},
            })
            continue

        case_token = (c.get("token") or token or "").strip()
        expect = dict(defaults)
        expect.update(c.get("expect") or {})

        print(f"▶ {cid} ({name}) … ", end="", flush=True)
        try:
            turn = _chat_collect(url, msg, case_token, args.timeout)
            scored = score_turn(turn, expect, has_token=bool(case_token))
            scored["case_id"] = cid
            scored["name"] = name
            scored["message"] = msg
            results.append(scored)
            mark = "PASS" if scored["passed"] else "FAIL"
            m = scored["metrics"]
            task = "done" if m.get("task_completed") else "open"
            print(
                mark,
                f" task={task} total_ms={m.get('timing_total_ms')} tools={m.get('tool_call_count')}",
            )
            if scored["failures"]:
                for f in scored["failures"]:
                    print(f"    ✗ {f}")
        except urllib.error.URLError as e:
            print(f"ERROR {e}")
            results.append({
                "case_id": cid,
                "passed": False,
                "failures": [f"request failed: {e}"],
                "metrics": {},
            })
            return 1

    runnable = [r for r in results if not r.get("skipped")]
    agg = aggregate_batch(runnable, thresholds)

    print("\n=== 批次指标 ===")
    print(
        f"任务完成率 {agg.get('task_completion_rate', 0):.1%}  "
        f"断言通过率 {agg.get('case_pass_rate', 0):.1%}  "
        f"平均耗时 {agg.get('avg_total_ms', 0)}ms  "
        f"P95 {agg.get('total_ms_p95', 0)}ms"
    )
    print(json.dumps(agg, ensure_ascii=False, indent=2))
    print(f"\n批次判定: {'PASS' if agg.get('batch_pass') else 'FAIL'}")

    if args.out:
        Path(args.out).write_text(
            json.dumps({"aggregate": agg, "results": results}, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
        print(f"报告: {args.out}")

    return 0 if agg.get("batch_pass") else 1


if __name__ == "__main__":
    raise SystemExit(main())
