#!/usr/bin/env python3
"""探测一轮长期记忆召回结果（Mongo + 用户/公共 Qdrant）。"""
from __future__ import annotations

import asyncio
import sys
from pathlib import Path

AGENT_DIR = Path(__file__).resolve().parents[1]
if str(AGENT_DIR) not in sys.path:
    sys.path.insert(0, str(AGENT_DIR))

from memory.memory_manager import (  # noqa: E402
    _sparse_query_text,
    get_long_term_context,
    init_memory_services,
)

UID = 2058127879710969856

CASES = [
    (
        "博得之门3 配置需求",
        "博得之门3 的配置需求是什么？",
        ["博德之门3", "配置需求"],
    ),
    (
        "你知道博得之门3吗",
        "博得之门3是什么游戏？",
        ["博德之门3", "游戏介绍"],
    ),
]


async def show(label: str, rewritten: str, hints: list[str]) -> None:
    await init_memory_services(ensure_kb=False)
    sparse = _sparse_query_text(rewritten, hints)
    print("=" * 60)
    print("场景:", label)
    print("改写句:", rewritten)
    print("memory_hints:", hints)
    print("BM25 合并文本:", sparse[:140])
    hits = await get_long_term_context(UID, rewritten, memory_hints=hints)
    print("合计召回:", len(hits), "条")
    mongo = [h for h in hits if h.get("store") == "mongo"]
    wiki = [
        h
        for h in hits
        if h.get("memory_scope") == "document" or h.get("kind") == "wiki"
    ]
    user_q = [
        h
        for h in hits
        if h.get("store") == "qdrant"
        and h.get("memory_scope") != "document"
        and h.get("kind") != "wiki"
    ]
    print("  Mongo:", len(mongo), "| Wiki/KB:", len(wiki), "| 用户Qdrant:", len(user_q))
    for i, h in enumerate(hits, 1):
        sp = h.get("section_path") or []
        sec = sp[-1] if isinstance(sp, list) and sp else ""
        preview = (h.get("content") or h.get("preview") or "")[:80].replace("\n", " ")
        print(
            f"{i:2}. store={h.get('store')} kind={h.get('kind')} scope={h.get('memory_scope')} "
            f"score={float(h.get('score', 0)):.3f} game={h.get('game_name') or '-'} sec={sec or '-'}"
        )
        print(f"    {preview}")


async def main() -> None:
    for label, rw, hints in CASES:
        await show(label, rw, hints)


if __name__ == "__main__":
    asyncio.run(main())
