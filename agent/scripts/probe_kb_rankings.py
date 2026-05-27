#!/usr/bin/env python3
"""rewrite → route → KB 检索：打印 memory_hints 与 Dense / Sparse / 融合排名。"""
from __future__ import annotations

import asyncio
import sys
from pathlib import Path

AGENT_DIR = Path(__file__).resolve().parents[1]
if str(AGENT_DIR) not in sys.path:
    sys.path.insert(0, str(AGENT_DIR))

from core.agent_core import _route  # noqa: E402
from core.rag import parse_rewrite_intents, rewrite_query  # noqa: E402
from infra.config import load_config  # noqa: E402
from memory.memory_manager import (  # noqa: E402
    _kb_collection_name,
    _rerank_hits_by_memory_hints,
    _search_qdrant_dense_multi,
    _search_qdrant_hybrid,
    _search_qdrant_sparse_only,
    _sparse_query_text,
    init_memory_services,
    sort_memory_hints_for_retrieval,
)


def _sec(hit: dict) -> str:
    sp = hit.get("section_path") or []
    if isinstance(sp, list) and sp:
        return str(sp[-1])
    return "-"


def _print_rank(title: str, hits: list[dict], *, score_key: str = "score", top: int = 8) -> None:
    print(f"\n--- {title} (top {top}) ---")
    if not hits:
        print("  (无)")
        return
    for i, h in enumerate(hits[:top], 1):
        sc = float(h.get(score_key, h.get("score", 0)) or 0)
        print(
            f"  {i:2}. {h.get('article_id', '?')} [{_sec(h)}] "
            f"{score_key}={sc:.4f}  chunk={h.get('chunk_index')}"
        )


async def main_async(msg: str) -> int:
    await init_memory_services()
    kb_col = _kb_collection_name()
    limit = 10
    mem = load_config().get("memory", {})
    prefetch = max(limit, int(mem.get("hybrid_prefetch_limit", 48)))

    print("=" * 64)
    print("用户原话:", msg)

    rewritten = (await rewrite_query(msg)).strip()
    print("检索改写:", rewritten)
    intents = parse_rewrite_intents(rewritten)
    if len(intents) > 1:
        print("Dense 分意图:", intents)

    route = await _route(msg, has_token=False, rewritten=rewritten)
    hints = sort_memory_hints_for_retrieval(route.memory_hints)
    sparse_text = _sparse_query_text(rewritten, hints)
    print("memory_hints:", hints)
    print("BM25 文本:", sparse_text)
    print("route servers:", route.servers)

    dense = _search_qdrant_dense_multi(kb_col, rewritten, limit, None, prefetch=prefetch)
    sparse_raw = _search_qdrant_sparse_only(kb_col, sparse_text, prefetch, None)
    sparse = (
        _rerank_hits_by_memory_hints(sparse_raw, hints)
        if mem.get("hint_rerank_enabled", True) and hints
        else sparse_raw
    )
    hybrid = _search_qdrant_hybrid(kb_col, rewritten, limit, None, memory_hints=hints)

    _print_rank("Dense（仅改写分意图）", dense, score_key="dense_score")
    _print_rank("Sparse (BM25 + hint 重排)", sparse, score_key="sparse_score")
    _print_rank("Hybrid 融合", hybrid, score_key="score")
    return 0


def main() -> int:
    msg = " ".join(sys.argv[1:]).strip() or "艾尔登法环是什么类型的游戏，谁开发的"
    return asyncio.run(main_async(msg))


if __name__ == "__main__":
    raise SystemExit(main())
