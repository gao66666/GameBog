#!/usr/bin/env python3
"""检查 Qdrant 公共知识库是否已入库（只读）。"""
from __future__ import annotations

import asyncio
import json
import sys
from pathlib import Path

AGENT_DIR = Path(__file__).resolve().parents[1]
if str(AGENT_DIR) not in sys.path:
    sys.path.insert(0, str(AGENT_DIR))

from memory.memory_manager import (  # noqa: E402
    _kb_collection_name,
    _qdrant_collection_names,
    init_memory_services,
    retrieve_kb_documents,
)
from qdrant_client import QdrantClient  # noqa: E402
from infra.config import load_config  # noqa: E402


def main() -> int:
    cfg = load_config()
    url = cfg.get("qdrant", {}).get("url", "http://127.0.0.1:6333")
    user_col, kb_col = _qdrant_collection_names()

    print(f"Qdrant URL: {url}")
    print(f"KB collection: {kb_col}")
    print(f"User memory collection: {user_col}")

    try:
        client = QdrantClient(url=url, timeout=10)
    except Exception as e:
        print(f"ERROR: 无法连接 Qdrant: {e}")
        return 1

    collections = [c.name for c in client.get_collections().collections]
    print(f"Collections: {collections}")

    if kb_col not in collections:
        print(f"\n[KB] 集合 {kb_col!r} 不存在 — 公共知识库未建库/未 seed")
        kb_count = 0
    else:
        info = client.get_collection(kb_col)
        kb_count = int(getattr(info, "points_count", 0) or 0)
        print(f"\n[KB] points_count = {kb_count}")

        # 按 article_id 统计
        counts: dict[str, int] = {}
        offset = None
        while True:
            batch, offset = client.scroll(
                collection_name=kb_col,
                limit=64,
                offset=offset,
                with_payload=["article_id", "game_name", "memory_scope"],
                with_vectors=False,
            )
            for pt in batch:
                aid = str((pt.payload or {}).get("article_id") or "")
                counts[aid] = counts.get(aid, 0) + 1
            if offset is None:
                break
        print("[KB] article_id 分布:")
        for aid, n in sorted(counts.items()):
            print(f"  {aid}: {n} chunks")
        if "game:bg3" in counts:
            print("  => game:bg3（博德之门3）已入库")
        else:
            print("  => game:bg3 未找到 — cases/kb_articles/bg3.md 可能未执行 seed_kb_eval.py")

        # 抽样 bg3
        flt = {
            "must": [{"key": "article_id", "match": {"value": "game:bg3"}}]
        }
        sample, _ = client.scroll(
            collection_name=kb_col,
            scroll_filter=flt,
            limit=2,
            with_payload=True,
            with_vectors=False,
        )
        if sample:
            print("\n[KB] game:bg3 样例 payload:")
            for pt in sample:
                p = pt.payload or {}
                print(
                    json.dumps(
                        {
                            "game_name": p.get("game_name"),
                            "section_path": p.get("section_path"),
                            "preview": (p.get("preview") or "")[:120],
                        },
                        ensure_ascii=False,
                    )
                )

    if user_col in collections:
        uinfo = client.get_collection(user_col)
        print(f"\n[User memory] points_count = {int(getattr(uinfo, 'points_count', 0) or 0)}")

    # 在线检索探测（需 embedding）
    async def probe_retrieve() -> None:
        await init_memory_services(ensure_kb=False)
        q = "博德之门3 是什么游戏"
        hits = await retrieve_kb_documents(q, limit=6, memory_hints=["博德之门3"])
        print(f"\n[Retrieve probe] query={q!r} hits={len(hits)}")
        for i, h in enumerate(hits[:5], 1):
            print(
                f"  {i}. kind={h.get('kind')} game_name={h.get('game_name')} "
                f"title={h.get('title')} score={h.get('score', 0):.4f}"
            )

    try:
        asyncio.run(probe_retrieve())
    except Exception as e:
        print(f"\n[Retrieve probe] 失败（多为 embedding/Qdrant）: {e}")

    return 0 if kb_count > 0 else 2


if __name__ == "__main__":
    raise SystemExit(main())
