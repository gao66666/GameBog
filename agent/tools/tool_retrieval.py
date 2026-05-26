"""
组内工具检索：route 之后、analysis_pre 之前，用改写句 + 路由 tool_hints 从 registry 召回候选工具名。
Hybrid：embedding 余弦相似度 + 词面重合；索引进程内缓存，registry mtime 变化时重建。
"""

from __future__ import annotations

import asyncio
import math
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from langchain_openai import OpenAIEmbeddings

from infra.agent_log import get_logger
from infra.config import load_config
from tools.tool_runtime import get_tool_spec, load_registry

_log = get_logger("tool_retrieval")

_REGISTRY_PATH = Path(__file__).resolve().parents[2] / "shared" / "tools_registry.json"

_EMBEDDINGS: OpenAIEmbeddings | None = None
_INDEX_CACHE: dict[str, Any] | None = None


def _orch_cfg() -> dict[str, Any]:
    return load_config().get("orchestration", {})


def tool_retrieval_enabled() -> bool:
    return bool(_orch_cfg().get("tool_retrieval_enabled", True))


def _sparse_tokenize(text: str) -> list[str]:
    if not (text or "").strip():
        return []
    raw = text.strip()
    low = raw.lower()
    toks: list[str] = []
    toks.extend(re.findall(r"[a-z0-9]+", low))
    for ch in raw:
        if "\u4e00" <= ch <= "\u9fff":
            toks.append(ch)
    return toks


def _keyword_score(query: str, doc: str) -> float:
    toks = _sparse_tokenize(query)
    if not toks:
        return 0.0
    blob = doc.lower()
    raw = doc
    hits = 0
    for t in toks:
        if len(t) == 1 and "\u4e00" <= t <= "\u9fff":
            if t in raw:
                hits += 1
        elif t in blob:
            hits += 1
    return hits / len(toks)


def _cosine(a: list[float], b: list[float]) -> float:
    if not a or not b or len(a) != len(b):
        return 0.0
    dot = sum(x * y for x, y in zip(a, b))
    na = math.sqrt(sum(x * x for x in a))
    nb = math.sqrt(sum(y * y for y in b))
    if na <= 0 or nb <= 0:
        return 0.0
    return dot / (na * nb)


def _tool_index_text(name: str, spec: dict[str, Any]) -> str:
    parts = [
        str(spec.get("label") or name),
        str(spec.get("group") or ""),
        str(spec.get("description") or ""),
    ]
    tags = spec.get("tags")
    if isinstance(tags, list) and tags:
        parts.append(" ".join(str(t) for t in tags))
    return " | ".join(p for p in parts if p.strip())


def pool_tool_names(servers: list[str]) -> list[str]:
    """按 registry 键序返回组内工具名（稳定顺序）。"""
    groups = set(servers)
    tools = load_registry().get("tools", {})
    return [n for n, spec in tools.items() if spec.get("group") in groups]


@dataclass
class _ToolIndexEntry:
    name: str
    group: str
    text: str
    vector: list[float] | None


def _get_embeddings() -> OpenAIEmbeddings:
    global _EMBEDDINGS
    if _EMBEDDINGS is None:
        cfg = load_config().get("embedding", {})
        kwargs: dict[str, Any] = dict(
            model=cfg.get("model", "embedding-3"),
            base_url=cfg.get("base_url"),
            api_key=cfg.get("api_key"),
        )
        dim = cfg.get("dimensions")
        if dim and int(dim) > 0:
            kwargs["dimensions"] = int(dim)
        _EMBEDDINGS = OpenAIEmbeddings(**kwargs)
    return _EMBEDDINGS


def _build_index() -> dict[str, _ToolIndexEntry]:
    entries: dict[str, _ToolIndexEntry] = {}
    tools = load_registry().get("tools", {})
    texts: list[str] = []
    names: list[str] = []
    for name, spec in tools.items():
        if spec.get("group") == "builtin":
            continue
        text = _tool_index_text(name, spec)
        entries[name] = _ToolIndexEntry(name=name, group=str(spec.get("group", "")), text=text, vector=None)
        names.append(name)
        texts.append(text)

    vectors: list[list[float]] = []
    if texts:
        try:
            emb = _get_embeddings()
            vectors = emb.embed_documents(texts)
        except Exception as e:
            _log.warning("工具索引 embedding 失败，检索将降级为 keyword-only", extra={"error": str(e)[:200]})

    for i, name in enumerate(names):
        if i < len(vectors) and vectors[i]:
            entries[name].vector = vectors[i]
    return entries


def _ensure_index() -> dict[str, _ToolIndexEntry]:
    global _INDEX_CACHE
    try:
        mtime = _REGISTRY_PATH.stat().st_mtime
    except OSError:
        mtime = 0.0

    if _INDEX_CACHE is not None and _INDEX_CACHE.get("mtime") == mtime:
        return _INDEX_CACHE["entries"]

    entries = _build_index()
    _INDEX_CACHE = {"mtime": mtime, "entries": entries}
    return entries


def _tool_match_score(
    ent: _ToolIndexEntry,
    query_text: str,
    query_vec: list[float] | None,
    *,
    dense_w: float,
    keyword_w: float,
) -> float:
    kw = _keyword_score(query_text, ent.text)
    dense = _cosine(query_vec, ent.vector) if query_vec and ent.vector else 0.0
    if query_vec and ent.vector:
        return dense_w * dense + keyword_w * kw
    return kw


def _rank_tools(
    query: str,
    pool: list[str],
    *,
    tool_hints: list[str] | None = None,
    top_k: int,
    dense_w: float,
    keyword_w: float,
) -> list[tuple[str, float]]:
    index = _ensure_index()
    queries: list[str] = []
    q = (query or "").strip()
    if q:
        queries.append(q)
    for hint in tool_hints or []:
        hs = str(hint).strip()
        if hs and hs not in queries:
            queries.append(hs)
    if not queries or not pool:
        return [(n, 0.0) for n in pool[:top_k]]

    query_vecs: list[list[float] | None] = [None] * len(queries)
    try:
        emb = _get_embeddings()
        if len(queries) == 1:
            query_vecs[0] = emb.embed_query(queries[0])
        else:
            embedded = emb.embed_documents(queries)
            for i, vec in enumerate(embedded):
                if i < len(query_vecs):
                    query_vecs[i] = vec
    except Exception as e:
        _log.warning("工具检索 query embed 失败，仅用 keyword", extra={"error": str(e)[:200]})

    ranked: list[tuple[str, float]] = []
    for name in pool:
        ent = index.get(name)
        if not ent:
            continue
        score = 0.0
        for i, qtext in enumerate(queries):
            s = _tool_match_score(
                ent,
                qtext,
                query_vecs[i] if i < len(query_vecs) else None,
                dense_w=dense_w,
                keyword_w=keyword_w,
            )
            if s > score:
                score = s
        ranked.append((name, score))

    ranked.sort(key=lambda x: (-x[1], x[0]))
    return ranked[:top_k]


async def retrieve_tools_for_turn(
    query: str,
    servers: list[str],
    *,
    tool_hints: list[str] | None = None,
) -> tuple[list[str], str]:
    """
    在 route 选定的 servers 组内召回工具名；query 为改写句，tool_hints 来自路由。
    返回 (tool_names, mode)：retrieved | skip_small_pool | fallback_all | disabled
    """
    if not tool_retrieval_enabled():
        pool = pool_tool_names(servers)
        return pool, "disabled"

    pool = pool_tool_names(servers)
    if not pool:
        return [], "fallback_all"

    orch = _orch_cfg()
    min_pool = int(orch.get("tool_retrieval_min_pool_for_skip", 4))
    top_k = int(orch.get("tool_retrieval_top_k", 10))
    dense_w = float(orch.get("tool_retrieval_dense_weight", 0.65))
    keyword_w = float(orch.get("tool_retrieval_keyword_weight", 0.35))
    fallback_all = bool(orch.get("tool_retrieval_fallback_all", True))

    if len(pool) <= min_pool:
        return list(pool), "skip_small_pool"

    try:
        ranked = await asyncio.to_thread(
            _rank_tools,
            query,
            pool,
            tool_hints=tool_hints,
            top_k=max(top_k, 1),
            dense_w=dense_w,
            keyword_w=keyword_w,
        )
        names = [n for n, _ in ranked]
        if names:
            return names, "retrieved"
    except Exception as e:
        _log.warning("工具检索异常", extra={"error": str(e)[:200]})

    if fallback_all:
        return list(pool), "fallback_all"
    return list(pool[:top_k]), "retrieved"
