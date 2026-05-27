"""Markdown 文档分片：先按标题切分，超长块再递归细分。"""

from __future__ import annotations

import re
from dataclasses import dataclass, field

_HEADING_RE = re.compile(r"^(#{1,6})\s+(.+?)\s*$")
_SENT_END_RE = re.compile(r"(?<=[。！？；.!?;])\s*")

# 递归细分分隔符优先级（由粗到细）
_RECURSIVE_SEPARATORS: tuple[str | re.Pattern[str], ...] = (
    "\n\n",
    "\n",
    "。",
    "！",
    "？",
    "；",
    ". ",
    "! ",
    "? ",
)


@dataclass
class MarkdownChunk:
    content: str
    section_path: list[str] = field(default_factory=list)
    heading_level: int = 0
    section_title: str = ""
    part_index: int = 0
    char_count: int = 0

    def __post_init__(self) -> None:
        self.char_count = len(self.content)


def _strip_heading_prefix(text: str) -> str:
    return text.strip()


def _split_oversized(text: str, chunk_size: int, overlap: int) -> list[str]:
    """对单段正文递归按分隔符切分，直至每片 <= chunk_size。"""
    text = text.strip()
    if not text:
        return []
    if len(text) <= chunk_size:
        return [text]

    for sep in _RECURSIVE_SEPARATORS:
        if isinstance(sep, str):
            if sep not in text:
                continue
            parts = text.split(sep)
            joiner = sep
        else:
            parts = sep.split(text)
            joiner = ""

        if len(parts) <= 1:
            continue

        merged: list[str] = []
        buf = ""
        for i, part in enumerate(parts):
            piece = part
            if isinstance(sep, str) and i < len(parts) - 1:
                piece = part + joiner if part else joiner
            if not piece.strip():
                continue
            candidate = (buf + piece) if buf else piece
            if len(candidate) <= chunk_size:
                buf = candidate
            else:
                if buf.strip():
                    merged.append(buf.strip())
                if len(piece) > chunk_size:
                    merged.extend(_split_oversized(piece, chunk_size, overlap))
                    buf = ""
                else:
                    buf = piece
        if buf.strip():
            merged.append(buf.strip())
        if len(merged) > 1 or (merged and len(merged[0]) <= chunk_size):
            return merged

    # 硬切（无合适分隔符）
    out: list[str] = []
    step = max(1, chunk_size - overlap)
    for i in range(0, len(text), step):
        out.append(text[i : i + chunk_size].strip())
    return [p for p in out if p]


def _parse_heading_sections(md: str) -> list[tuple[int, str, str]]:
    """
    按 # / ## / … 切分。
    返回 (level, title, body)；level=0 表示文首无标题前言。
    """
    lines = md.replace("\r\n", "\n").split("\n")
    sections: list[tuple[int, str, str]] = []
    doc_title = ""
    cur_level = 0
    cur_title = ""
    body_lines: list[str] = []

    def flush() -> None:
        nonlocal body_lines, cur_title, cur_level
        body = "\n".join(body_lines).strip()
        if cur_title or body:
            sections.append((cur_level, cur_title or doc_title, body))
        body_lines = []

    for line in lines:
        m = _HEADING_RE.match(line)
        if m:
            flush()
            cur_level = len(m.group(1))
            cur_title = m.group(2).strip()
            if cur_level == 1 and not doc_title:
                doc_title = cur_title
            continue
        body_lines.append(line)

    flush()
    return sections


def chunk_markdown(
    md: str,
    *,
    chunk_size: int = 900,
    chunk_overlap: int = 80,
    doc_title: str = "",
    include_heading_in_content: bool = True,
) -> list[MarkdownChunk]:
    """
    1. 按 Markdown 标题初步分节，标题写入 section_path 元数据
    2. 节内正文若超过 chunk_size，按 \\n\\n / \\n / 句读递归切分
    """
    chunk_size = max(200, int(chunk_size))
    chunk_overlap = max(0, min(chunk_overlap, chunk_size // 4))
    raw_sections = _parse_heading_sections(md)

    if not raw_sections:
        return []

    path_stack: list[str] = []
    if doc_title:
        path_stack = [doc_title]

    out: list[MarkdownChunk] = []
    for level, title, body in raw_sections:
        if level == 0:
            sec_path = list(path_stack)
            sec_title = title or (path_stack[-1] if path_stack else "")
        elif level == 1:
            path_stack = [title]
            sec_path = [title]
            sec_title = title
        else:
            while len(path_stack) >= level:
                path_stack.pop()
            if level - 1 < len(path_stack):
                path_stack = path_stack[: level - 1]
            path_stack.append(title)
            sec_path = list(path_stack)
            sec_title = title

        if not body.strip():
            continue

        parts = _split_oversized(body, chunk_size, chunk_overlap)
        game_label = (doc_title or "").strip() or (
            sec_path[0].strip() if sec_path else ""
        )
        for pi, part in enumerate(parts):
            text = part
            if include_heading_in_content and sec_title:
                prefix = " / ".join(sec_path)
                head = f"【{prefix}】"
                if game_label:
                    head = f"检索: {game_label} | {sec_title.strip()}\n\n{head}"
                text = f"{head}\n\n{part}"
            out.append(
                MarkdownChunk(
                    content=text,
                    section_path=sec_path,
                    heading_level=level,
                    section_title=sec_title,
                    part_index=pi,
                )
            )
    return out


def assign_chunk_indices(
    chunks: list[MarkdownChunk],
    *,
    section_order: list[str] | None = None,
) -> list[tuple[MarkdownChunk, int]]:
    """
    为每块分配稳定 chunk_index。
    有 section_order 时：index = section_order_idx * 100 + part_index；否则顺序 0..n-1。
    """
    order_map = {name: i for i, name in enumerate(section_order or [])}
    result: list[tuple[MarkdownChunk, int]] = []

    if not order_map:
        for i, ch in enumerate(chunks):
            result.append((ch, i))
        return result

    for ch in chunks:
        title = ch.section_title.strip()
        if title not in order_map:
            # 尝试 section_path 最后一级
            for seg in reversed(ch.section_path):
                if seg in order_map:
                    title = seg
                    break
        sec_idx = order_map.get(title, 99)
        ci = sec_idx * 100 + ch.part_index
        result.append((ch, ci))
    return result
