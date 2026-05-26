#!/usr/bin/env python3
"""工具调用准确性测评：cases/tool_cases.yaml + tool_scorer + 基础 turn 断言。"""

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

from eval.scorer import aggregate_batch, score_turn  # noqa: E402  # aggregate_batch: task_completion_rate, avg_total_ms
from eval.sse_collector import SseTurnCollector  # noqa: E402
from eval.tool_scorer import score_tool_accuracy  # noqa: E402

CASES_PATH = AGENT_DIR / "cases" / "tool_cases.yaml"
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


def main() -> int:
    p = argparse.ArgumentParser(description="工具调用准确性测评")
    default_url = os.getenv("AGENT_URL", "http://127.0.0.1:9091").rstrip("/")
    if not default_url.endswith("/chat"):
        default_url += "/chat"
    p.add_argument("--url", default=default_url)
    p.add_argument("--token", default=os.getenv("AGENT_EVAL_TOKEN", ""))
    p.add_argument("--case", default="")
    p.add_argument("--timeout", type=int, default=120)
    p.add_argument("--out", default="")
    args = p.parse_args()

    spec = _load_yaml(METRICS_PATH)
    thresholds = spec.get("tool_thresholds") or spec.get("thresholds") or {}
    blob = _load_yaml(CASES_PATH)
    defaults = blob.get("defaults") or {}
    cases = blob.get("cases") or []

    filter_ids = {x.strip() for x in args.case.split(",") if x.strip()} if args.case else None
    if filter_ids:
        cases = [c for c in cases if c.get("id") in filter_ids]

    token = (args.token or "").strip()
    results: list[dict] = []

    print(f"Tool eval URL: {args.url}\n")
    for c in cases:
        cid = c.get("id", "?")
        msg = str(c.get("message", "")).strip()
        need_token = bool(c.get("require_token"))
        if need_token and not token:
            print(f"[SKIP] {cid} — 需要 --token")
            results.append({
                "case_id": cid,
                "passed": False,
                "skipped": True,
                "failures": ["missing token"],
                "metrics": {},
            })
            continue

        case_token = (c.get("token") if c.get("token") is not None else token or "").strip()
        expect = dict(defaults)
        expect.update(c.get("expect") or {})

        print(f"▶ {cid} … ", end="", flush=True)
        try:
            turn = _chat_collect(args.url, msg, case_token, args.timeout)
            base = score_turn(turn, expect, has_token=bool(case_token))
            tool = score_tool_accuracy(turn, expect)
            passed = base["passed"] and tool["passed"]
            failures = list(base["failures"]) + list(tool["failures"])
            metrics = {**base["metrics"], **tool["metrics"]}
            rec = {
                "case_id": cid,
                "passed": passed,
                "failures": failures,
                "metrics": metrics,
            }
            results.append(rec)
            print("PASS" if passed else "FAIL", f" tools={metrics.get('tool_call_count')}")
            for f in failures:
                print(f"    ✗ {f}")
        except urllib.error.URLError as e:
            print(f"ERROR {e}")
            return 1

    agg = aggregate_batch([r for r in results if not r.get("skipped")], thresholds)
    n = agg.get("cases") or len(results)
    args_ok = sum(
        1 for r in results
        if not r.get("skipped") and r.get("metrics", {}).get("tool_args_match")
    )
    runnable_n = sum(1 for r in results if not r.get("skipped"))
    agg["tool_args_match_rate"] = round(args_ok / runnable_n, 3) if runnable_n else 0
    agg["batch_pass"] = bool(agg.get("batch_pass")) and (
        agg["tool_args_match_rate"] >= float(thresholds.get("tool_args_match_rate_min", 0.85))
    )

    print("\n=== 工具测评汇总 ===")
    print(
        f"任务完成率 {agg.get('task_completion_rate', 0):.1%}  "
        f"平均耗时 {agg.get('avg_total_ms', 0)}ms"
    )
    print(json.dumps(agg, ensure_ascii=False, indent=2))
    if args.out:
        Path(args.out).write_text(
            json.dumps({"aggregate": agg, "results": results}, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
    return 0 if agg.get("batch_pass") else 1


if __name__ == "__main__":
    raise SystemExit(main())
