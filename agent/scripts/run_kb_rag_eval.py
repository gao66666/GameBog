#!/usr/bin/env python3
"""
公共知识库 RAG 召回准确率测评（仅 goblog_kb_documents，检索与用户库相同 hybrid 方案）。

用法:
  python agent/scripts/seed_kb_eval.py
  python agent/scripts/run_kb_rag_eval.py
  python agent/scripts/run_kb_rag_eval.py --case kb_elden_guide --out kb_eval.json
"""

from __future__ import annotations

import argparse
import asyncio
import json
import sys
from pathlib import Path

import yaml

AGENT_DIR = Path(__file__).resolve().parents[1]
if str(AGENT_DIR) not in sys.path:
    sys.path.insert(0, str(AGENT_DIR))

from eval.kb_scorer import score_kb_rag_case  # noqa: E402
from memory.memory_manager import init_memory_services, retrieve_kb_documents  # noqa: E402

CASES_PATH = AGENT_DIR / "cases" / "kb_rag_cases.yaml"
CORPUS_PATH = AGENT_DIR / "cases" / "kb_rag_corpus.yaml"
METRICS_PATH = AGENT_DIR / "eval" / "metrics_spec.yaml"


def _load_yaml(path: Path) -> dict:
    with open(path, "r", encoding="utf-8") as f:
        return yaml.safe_load(f) or {}


async def _run(cases: list[dict], defaults: dict, section_order: list[str]) -> list[dict]:
    await init_memory_services()
    results: list[dict] = []
    for c in cases:
        cid = c.get("id", "?")
        if c.get("skip"):
            results.append({"case_id": cid, "passed": True, "skipped": True, "metrics": {}})
            continue
        query = str(c.get("query", "")).strip()
        hints = c.get("memory_hints") if isinstance(c.get("memory_hints"), list) else None
        expect = dict(defaults)
        expect.update(c.get("expect") or {})
        if not expect.get("expected_chunks"):
            print(f"[SKIP] {cid} — 无 expected_chunks")
            results.append({"case_id": cid, "passed": False, "skipped": True, "failures": ["no expected_chunks"]})
            continue

        print(f"▶ {cid} q={query[:36]!r} … ", end="", flush=True)
        hits = await retrieve_kb_documents(query, limit=int(expect.get("k", 10)), memory_hints=hints)
        scored = score_kb_rag_case(hits, expect, section_order=section_order)
        scored["case_id"] = cid
        scored["name"] = c.get("name", cid)
        scored["retrieval_ms_note"] = "see batch from timings if E2E"
        results.append(scored)
        m = scored["metrics"]
        print(
            "PASS" if scored["passed"] else "FAIL",
            f" R={m.get('recall_at_k')} P={m.get('precision_at_k')} MRR={m.get('mrr')}",
        )
        for f in scored.get("failures") or []:
            print(f"    ✗ {f}")
    return results


def main() -> int:
    p = argparse.ArgumentParser(description="公共知识库 RAG 召回测评")
    p.add_argument("--case", default="")
    p.add_argument("--out", default="")
    args = p.parse_args()

    spec = _load_yaml(METRICS_PATH)
    th = spec.get("kb_rag_thresholds") or {}
    blob = _load_yaml(CASES_PATH)
    corpus = _load_yaml(CORPUS_PATH)
    section_order = corpus.get("section_order") or []
    defaults = blob.get("defaults") or {}
    cases = blob.get("cases") or []
    if args.case:
        ids = {x.strip() for x in args.case.split(",") if x.strip()}
        cases = [c for c in cases if c.get("id") in ids]

    results = asyncio.run(_run(cases, defaults, section_order))
    runnable = [r for r in results if not r.get("skipped")]
    if not runnable:
        print("\n请先 seed_kb_eval.py 并配置 kb_rag_cases.yaml")
        return 2

    recalls = [r["metrics"]["recall_at_k"] for r in runnable]
    mrrs = [r["metrics"]["mrr"] for r in runnable]
    precs = [r["metrics"]["precision_at_k"] for r in runnable]
    passed_n = sum(1 for r in runnable if r.get("passed"))
    n = len(runnable)
    agg = {
        "suite": "kb_rag",
        "cases": n,
        "case_pass_rate": round(passed_n / n, 3),
        "mean_recall_at_k": round(sum(recalls) / n, 4),
        "mean_precision_at_k": round(sum(precs) / n, 4),
        "mean_mrr": round(sum(mrrs) / n, 4),
        "passed": passed_n,
        "failed": n - passed_n,
    }
    agg["batch_pass"] = (
        agg["mean_recall_at_k"] >= float(th.get("mean_recall_at_k_min", 0.9))
        and agg["mean_mrr"] >= float(th.get("mean_mrr_min", 0.8))
        and agg["case_pass_rate"] >= float(th.get("case_pass_rate_min", 0.85))
    )

    print("\n=== 知识库 RAG 召回汇总 ===")
    print(json.dumps(agg, ensure_ascii=False, indent=2))
    if args.out:
        Path(args.out).write_text(
            json.dumps({"aggregate": agg, "results": results}, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
    return 0 if agg.get("batch_pass") else 1


if __name__ == "__main__":
    raise SystemExit(main())
