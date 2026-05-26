#!/usr/bin/env python3
"""长期记忆检索精度测评（离线直调 get_long_term_context）。"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
from pathlib import Path

import yaml

AGENT_DIR = Path(__file__).resolve().parents[1]
if str(AGENT_DIR) not in sys.path:
    sys.path.insert(0, str(AGENT_DIR))

from eval.memory_scorer import score_memory_case  # noqa: E402
from memory.memory_manager import get_long_term_context, init_memory_services  # noqa: E402

CASES_PATH = AGENT_DIR / "cases" / "memory_cases.yaml"
METRICS_PATH = AGENT_DIR / "eval" / "metrics_spec.yaml"


def _load_yaml(path: Path) -> dict:
    with open(path, "r", encoding="utf-8") as f:
        return yaml.safe_load(f) or {}


async def _run_cases(cases: list[dict], defaults: dict, eval_user_id: int) -> list[dict]:
    await init_memory_services()
    results: list[dict] = []
    for c in cases:
        cid = c.get("id", "?")
        if c.get("skip"):
            print(f"[SKIP] {cid} — marked skip")
            results.append({"case_id": cid, "passed": True, "skipped": True, "metrics": {}})
            continue

        uid = int(c.get("user_id") or eval_user_id or 0)
        if uid <= 0:
            print(f"[SKIP] {cid} — 需设置 user_id 或 AGENT_EVAL_USER_ID")
            results.append({
                "case_id": cid,
                "passed": False,
                "skipped": True,
                "failures": ["missing user_id"],
                "metrics": {},
            })
            continue

        query = str(c.get("query", "")).strip()
        hints = c.get("memory_hints") if isinstance(c.get("memory_hints"), list) else None
        expect = dict(defaults)
        expect.update(c.get("expect") or {})
        expected = expect.get("expected_ids") or []
        if not expected:
            print(f"[SKIP] {cid} — expected_ids 为空，请先写入种子记忆并配置 ID")
            results.append({
                "case_id": cid,
                "passed": False,
                "skipped": True,
                "failures": ["empty expected_ids"],
                "metrics": {},
            })
            continue

        print(f"▶ {cid} user={uid} query={query[:40]!r} … ", end="", flush=True)
        hits = await get_long_term_context(uid, query, memory_hints=hints)
        scored = score_memory_case(hits, expect)
        scored["case_id"] = cid
        results.append(scored)
        m = scored["metrics"]
        print(
            "PASS" if scored["passed"] else "FAIL",
            f" P@{expect.get('k',10)}={m.get('precision_at_k')} R={m.get('recall_at_k')} MRR={m.get('mrr')}",
        )
        for f in scored.get("failures") or []:
            print(f"    ✗ {f}")
    return results


def main() -> int:
    p = argparse.ArgumentParser(description="长期记忆检索精度测评")
    p.add_argument("--case", default="")
    p.add_argument("--out", default="")
    args = p.parse_args()

    spec = _load_yaml(METRICS_PATH)
    thresholds = spec.get("memory_thresholds") or {}
    blob = _load_yaml(CASES_PATH)
    defaults = blob.get("defaults") or {}
    eval_user_id = int(blob.get("eval_user_id") or 0) or int(os.getenv("AGENT_EVAL_USER_ID", "0") or 0)
    cases = blob.get("cases") or []

    if args.case:
        ids = {x.strip() for x in args.case.split(",") if x.strip()}
        cases = [c for c in cases if c.get("id") in ids]

    results = asyncio.run(_run_cases(cases, defaults, eval_user_id))
    runnable = [r for r in results if not r.get("skipped")]
    if not runnable:
        print("\n无可运行 case：请配置 memory_cases.yaml 中的 expected_ids 与 user_id")
        return 2

    recalls = [r["metrics"]["recall_at_k"] for r in runnable if r.get("metrics")]
    precs = [r["metrics"]["precision_at_k"] for r in runnable if r.get("metrics")]
    mrrs = [r["metrics"]["mrr"] for r in runnable if r.get("metrics")]
    passed_n = sum(1 for r in runnable if r.get("passed"))
    n = len(runnable)
    agg = {
        "cases": n,
        "case_pass_rate": round(passed_n / n, 3),
        "mean_recall_at_k": round(sum(recalls) / len(recalls), 4) if recalls else 0,
        "mean_precision_at_k": round(sum(precs) / len(precs), 4) if precs else 0,
        "mean_mrr": round(sum(mrrs) / len(mrrs), 4) if mrrs else 0,
        "passed": passed_n,
        "failed": n - passed_n,
    }
    agg["batch_pass"] = (
        agg["case_pass_rate"] >= float(thresholds.get("case_pass_rate_min", 0.8))
        and agg["mean_recall_at_k"] >= float(thresholds.get("mean_recall_at_k_min", 0.9))
        and agg["mean_precision_at_k"] >= float(thresholds.get("mean_precision_at_k_min", 0.5))
        and agg["mean_mrr"] >= float(thresholds.get("mean_mrr_min", 0.8))
    )

    print("\n=== 记忆检索汇总 ===")
    print(json.dumps(agg, ensure_ascii=False, indent=2))
    if args.out:
        Path(args.out).write_text(
            json.dumps({"aggregate": agg, "results": results}, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
    return 0 if agg.get("batch_pass") else 1


if __name__ == "__main__":
    raise SystemExit(main())
