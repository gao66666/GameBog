#!/usr/bin/env python3
"""
从 cases/kb_articles/*.md 分片写入 Qdrant 公共知识库。

分片：eval/markdown_chunker.py
  1. 按 # / ## 标题切节，标题写入 section_path
  2. 节内超过 chunk_size 时，按 \\n\\n、\\n、句读递归切分

用法:
  python agent/scripts/seed_kb_eval.py --dry-run
  python agent/scripts/seed_kb_eval.py --chunk-size 600
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

from eval.kb_scorer import kb_chunk_point_id  # noqa: E402
from eval.markdown_chunker import assign_chunk_indices, chunk_markdown  # noqa: E402

CORPUS_PATH = AGENT_DIR / "cases" / "kb_rag_corpus.yaml"
ARTICLES_DIR = AGENT_DIR / "cases" / "kb_articles"


async def main_async(dry_run: bool, chunk_size: int, chunk_overlap: int) -> int:
    blob = yaml.safe_load(CORPUS_PATH.read_text(encoding="utf-8")) or {}
    source = str(blob.get("source") or "kb_eval")
    revision = int(blob.get("content_revision", 1) or 1)
    section_order = blob.get("section_order") or []
    default_chunk = int(blob.get("chunk_size", chunk_size))
    default_overlap = int(blob.get("chunk_overlap", chunk_overlap))
    articles = blob.get("articles") or []

    if not articles:
        print("articles empty in kb_rag_corpus.yaml", file=sys.stderr)
        return 2

    ingest_fn = None
    if not dry_run:
        from memory.memory_manager import ingest_document_chunk, init_memory_services

        await init_memory_services()
        ingest_fn = ingest_document_chunk

    manifest: list[dict] = []
    total = 0

    for art in articles:
        aid = str(art.get("article_id", "")).strip()
        game = str(art.get("game_name", "")).strip()
        fname = str(art.get("file", "")).strip()
        path = ARTICLES_DIR / fname
        if not aid or not path.is_file():
            print(f"[SKIP] {aid or fname} — 文件不存在: {path}", file=sys.stderr)
            continue

        md = path.read_text(encoding="utf-8")
        cs = int(art.get("chunk_size", default_chunk))
        co = int(art.get("chunk_overlap", default_overlap))
        chunks = chunk_markdown(md, chunk_size=cs, chunk_overlap=co, doc_title=game)
        indexed = assign_chunk_indices(chunks, section_order=section_order)

        print(f"\n{aid} ({game}) — {len(indexed)} chunks, chunk_size={cs}")
        for ch, ci in indexed:
            pid = kb_chunk_point_id(aid, ci, revision)
            manifest.append({
                "article_id": aid,
                "chunk_index": ci,
                "content_revision": revision,
                "point_id": pid,
                "game_name": game,
                "section_path": ch.section_path,
                "section_title": ch.section_title,
                "part_index": ch.part_index,
                "char_count": ch.char_count,
            })
            if dry_run:
                print(f"  #{ci} [{ch.section_title}] part={ch.part_index} len={ch.char_count}")
            else:
                assert ingest_fn is not None
                await ingest_fn(
                    aid,
                    ci,
                    ch.content,
                    content_revision=revision,
                    source=source,
                    ingest_kind="raw_chunk",
                    game_name=game or None,
                    section_path=ch.section_path,
                    preview=ch.content[:240],
                )
                print(f"  ok #{ci} [{ch.section_title}] len={ch.char_count}")
            total += 1

    out_path = AGENT_DIR / "cases" / "kb_rag_manifest.json"
    out_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"\n共 {total} 块 → {out_path}")
    return 0


def main() -> int:
    p = argparse.ArgumentParser(description="KB Markdown 分片入库")
    p.add_argument("--dry-run", action="store_true")
    p.add_argument("--chunk-size", type=int, default=900)
    p.add_argument("--chunk-overlap", type=int, default=80)
    args = p.parse_args()
    return asyncio.run(main_async(args.dry_run, args.chunk_size, args.chunk_overlap))


if __name__ == "__main__":
    raise SystemExit(main())
