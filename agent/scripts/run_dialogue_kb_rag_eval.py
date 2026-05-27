#!/usr/bin/env python3
"""
对话轮次的 KB RAG 后验评估：对 dialogue_metrics_cases.yaml 中带 kb_expectations 的轮次，
直接调 retrieve_kb_documents 计算 Recall@k 和 MRR，不依赖 SSE 事件。
"""

from __future__ import annotations

import asyncio
import json
import sys
from pathlib import Path

import yaml

AGENT_DIR = Path(__file__).resolve().parents[1]
if str(AGENT_DIR) not in sys.path:
    sys.path.insert(0, str(AGENT_DIR))

from eval.scorer import score_kb_retrieval  # noqa: E402
from memory.memory_manager import init_memory_services, retrieve_kb_documents  # noqa: E402
from core.rag import rewrite_query  # noqa: E402
from core.agent_core import _route  # noqa: E402

CASES_PATH = AGENT_DIR / "cases" / "dialogue_metrics_cases.yaml"


async def _kb_retrieve_for_query(raw_msg: str) -> list[dict]:
    """模拟 Agent 流水线：rewrite → route → retrieve_kb_documents"""
    # 阶段 0: rewrite
    rewritten = await rewrite_query(raw_msg)
    query = rewritten if rewritten and rewritten != raw_msg else raw_msg

    # 阶段 1: route（获取 memory_hints）
    route_result = await _route(query, has_token=False)
    hints = route_result.get("memory_hints") or []

    # 阶段 2: KB 检索（用改写后的 query + route 生成的 memory_hints）
    return await retrieve_kb_documents(query, limit=10, memory_hints=hints or None)


async def main_async() -> int:
    await init_memory_services()
    blob = yaml.safe_load(CASES_PATH.read_text(encoding="utf-8")) or {}
    defaults = blob.get("defaults") or {}
    base_expect = dict(defaults.get("expect") or {})

    all_results: list[dict] = []

    for sess in blob.get("sessions") or []:
        sid = sess.get("id", "?")
        sname = sess.get("name", sid)
        turns = [str(t).strip() for t in (sess.get("turns") or []) if str(t).strip()]
        kb_expects: list[dict] = sess.get("kb_expectations") or []
        if not kb_expects:
            continue

        print(f"\n=== {sname} ({sid}) ===")

        for i, msg in enumerate(turns, 1):
            if (i - 1) >= len(kb_expects):
                continue
            ekb = kb_expects[i - 1]
            expected = ekb.get("expected_sections") if isinstance(ekb, dict) else None
            if not expected:
                continue

            print(f"  [{i}/{len(turns)}] {msg[:40]}… ", end="", flush=True)

            # Agent 流水线：rewrite → route → retrieve
            hits = await _kb_retrieve_for_query(msg)
            scored = score_kb_retrieval(hits, expected, k=10)

            res = {
                "session_id": sid,
                "turn_index": i,
                "query": msg[:80],
                "expected_sections": expected,
                **scored,
            }
            all_results.append(res)

            print(
                "PASS" if scored.get("kb_passed") else "FAIL",
                f" R={scored.get('kb_recall_at_k', 0):.1%} MRR={scored.get('kb_mrr', 0):.3f}",
            )

    if not all_results:
        print("没有找到带 kb_expectations 的轮次")
        return 2

    recalls = [r["kb_recall_at_k"] for r in all_results if r.get("kb_recall_at_k") is not None]
    mrrs = [r["kb_mrr"] for r in all_results if r.get("kb_mrr") is not None]
    passed = sum(1 for r in all_results if r.get("kb_passed"))
    n = len(all_results)

    print(f"\n=== KB RAG 后验汇总 ===")
    print(f"轮次: {n}")
    print(f"通过率: {passed}/{n} ({passed/n:.1%})")
    print(f"平均 Recall@k: {sum(recalls)/len(recalls):.2%}" if recalls else "N/A")
    print(f"平均 MRR: {sum(mrrs)/len(mrrs):.4f}" if mrrs else "N/A")

    out = Path(AGENT_DIR / "eval_reports" / "dialogue_kb_rag_report.json")
    out.write_text(
        json.dumps({"aggregate": {
            "cases": n,
            "passed": passed,
            "failed": n - passed,
            "pass_rate": round(passed / n, 3),
            "mean_recall_at_k": round(sum(recalls) / len(recalls), 4) if recalls else 0,
            "mean_mrr": round(sum(mrrs) / len(mrrs), 4) if mrrs else 0,
        }, "results": all_results}, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )
    print(f"\n报告: {out}")
    return 0 if passed == n else 1


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main_async()))
