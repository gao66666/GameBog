"""Markdown 分片单测。"""

from pathlib import Path

from eval.markdown_chunker import assign_chunk_indices, chunk_markdown

AGENT_DIR = Path(__file__).resolve().parents[1]

SAMPLE = """# 测试游戏

## 简介

这是简介段落。

## 配置需求

""" + ("最低配置需要 8GB 内存。" * 80) + """

## 玩法

探索与战斗循环。
"""


def test_heading_split():
    chunks = chunk_markdown(SAMPLE, chunk_size=400, chunk_overlap=40, doc_title="测试游戏")
    titles = [c.section_title for c in chunks]
    assert "简介" in titles
    assert "配置需求" in titles
    assert any(c.part_index > 0 for c in chunks if c.section_title == "配置需求")


def test_chunk_index_encoding():
    chunks = chunk_markdown(SAMPLE, chunk_size=400, doc_title="测试游戏")
    order = ["简介", "游戏背景", "配置需求", "玩法", "注意事项"]
    indexed = assign_chunk_indices(chunks, section_order=order)
    by_sec = {ch.section_title: ci for ch, ci in indexed}
    assert by_sec["简介"] == 0
    assert by_sec["玩法"] == 300


def test_real_article_dry_run():
    md = (AGENT_DIR / "cases" / "kb_articles" / "elden-ring.md").read_text(encoding="utf-8")
    chunks = chunk_markdown(md, chunk_size=500, chunk_overlap=50, doc_title="艾尔登法环")
    assert len(chunks) >= 5
    assert all("【" in c.content for c in chunks)
