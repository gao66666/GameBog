"""公共知识库 RAG 召回精度（article_id + chunk_index，与用户库同检索路径）。"""

from __future__ import annotations

import uuid
from typing import Any


def kb_chunk_point_id(article_id: str, chunk_index: int, content_revision: int = 1) -> str:
    return str(
        uuid.uuid5(
            uuid.NAMESPACE_URL,
            f"goblog:kb:{article_id.strip()}:{int(chunk_index)}:{int(content_revision)}",
        )
    )


def _chunk_key(row: dict[str, Any]) -> tuple[str, int, int] | None:
    aid = str(row.get("article_id", "")).strip()
    if not aid:
        return None
    return (aid, int(row.get("chunk_index", 0) or 0), int(row.get("content_revision", 1) or 1))


def _normalize_chunk_ref(item: Any) -> tuple[str, int, int]:
    if isinstance(item, dict):
        aid = str(item.get("article_id", "")).strip()
        ci = int(item.get("chunk_index", 0) or 0)
        rev = int(item.get("content_revision", 1) or 1)
        return aid, ci, rev
    s = str(item).strip()
    parts = s.split(":")
    if len(parts) >= 3:
        return parts[0], int(parts[1]), int(parts[2])
    if len(parts) == 2:
        return parts[0], int(parts[1]), 1
    return s, 0, 1


def _chunk_to_point_id(aid: str, ci: int, rev: int) -> str:
    return kb_chunk_point_id(aid, ci, rev)


def _row_article_id(row: dict[str, Any]) -> str:
    return str(row.get("article_id", "")).strip()


def _row_section_title(row: dict[str, Any]) -> str:
    sp = row.get("section_path")
    if isinstance(sp, list) and sp:
        return str(sp[-1]).strip()
    return ""


def _expand_expectations(
    expect: dict[str, Any],
    *,
    section_order: list[str] | None = None,
) -> tuple[list[tuple[str, int, int]], list[tuple[str, str]]]:
    """返回 (chunk_keys, section_pairs)。"""
    chunk_keys: list[tuple[str, int, int]] = []
    for item in expect.get("expected_chunks") or []:
        key = _normalize_chunk_ref(item)
        if key[0]:
            chunk_keys.append(key)

    section_pairs: list[tuple[str, str]] = []
    order = section_order or []
    for item in expect.get("expected_sections") or []:
        if not isinstance(item, dict):
            continue
        aid = str(item.get("article_id", "")).strip()
        sec = str(item.get("section", "")).strip()
        if aid and sec:
            section_pairs.append((aid, sec))
            if order and sec in order:
                base = order.index(sec) * 100
                chunk_keys.append((aid, base, int(item.get("content_revision", 1) or 1)))

    return chunk_keys, section_pairs


def _is_relevant_row(
    row: dict[str, Any],
    *,
    expected_keys: list[tuple[str, int, int]],
    expected_pids: set[str],
    expected_sections: list[tuple[str, str]],
) -> bool:
    key = _chunk_key(row)
    if key and key in expected_keys:
        return True
    pid = str(row.get("memory_id", "")).strip()
    if pid and pid in expected_pids:
        return True
    aid = _row_article_id(row)
    sec = _row_section_title(row)
    if aid and sec:
        for ea, es in expected_sections:
            if ea == aid and es == sec:
                return True
    return False


def kb_retrieval_metrics(
    hits: list[dict[str, Any]],
    *,
    expected_chunks: list[Any],
    expected_sections: list[Any] | None = None,
    forbidden_chunks: list[Any] | None = None,
    forbidden_sections: list[Any] | None = None,
    k: int = 10,
    section_order: list[str] | None = None,
) -> dict[str, Any]:
    """Recall@K / Precision@K / MRR，按文档块标注。"""
    k = max(1, int(k))
    top = [h for h in hits[:k] if isinstance(h, dict)]

    ranked_keys: list[tuple[str, int, int]] = []
    ranked_pids: list[str] = []
    for row in top:
        key = _chunk_key(row)
        if key:
            ranked_keys.append(key)
        pid = str(row.get("memory_id", "")).strip()
        if pid:
            ranked_pids.append(pid)

    expected_keys: list[tuple[str, int, int]] = []
    expected_pids: set[str] = set()
    for item in expected_chunks or []:
        key = _normalize_chunk_ref(item)
        if key[0]:
            expected_keys.append(key)
            expected_pids.add(_chunk_to_point_id(*key))

    section_pairs: list[tuple[str, str]] = []
    for item in expected_sections or []:
        if isinstance(item, dict):
            aid = str(item.get("article_id", "")).strip()
            sec = str(item.get("section", "")).strip()
            if aid and sec:
                section_pairs.append((aid, sec))

    n_expected = len(set(expected_keys)) + len(
        [s for s in section_pairs if not any(k[0] == s[0] for k in expected_keys)]
    )
    if not expected_keys and not section_pairs:
        n_expected = 0
    elif section_pairs:
        n_expected = max(n_expected, len(section_pairs))

    if n_expected == 0:
        return {
            "precision_at_k": 1.0,
            "recall_at_k": 1.0,
            "mrr": 1.0,
            "hit_at_k": True,
            "forbidden_clean": True,
            "retrieved_chunk_keys": [f"{a}:{c}:{r}" for a, c, r in ranked_keys],
            "expected_chunk_keys": [],
        }

    def _is_relevant(row: dict[str, Any], pos: int) -> bool:
        return _is_relevant_row(
            row,
            expected_keys=expected_keys,
            expected_pids=expected_pids,
            expected_sections=section_pairs,
        )

    hit_positions: list[int] = []
    matched_sections: set[tuple[str, str]] = set()
    matched_keys: set[tuple[str, int, int]] = set()
    for i, row in enumerate(top, start=1):
        if _is_relevant(row, i):
            hit_positions.append(i)
            key = _chunk_key(row)
            if key:
                matched_keys.add(key)
            aid = _row_article_id(row)
            sec = _row_section_title(row)
            if aid and sec:
                matched_sections.add((aid, sec))

    relevant_in_top = len(hit_positions)
    precision = relevant_in_top / len(top) if top else 0.0

    hit_sections = len(matched_sections)
    if section_pairs:
        recall = len({s for s in section_pairs if s in matched_sections}) / len(section_pairs)
    elif expected_keys:
        recall = len(matched_keys) / len(set(expected_keys))
    else:
        recall = 1.0
    mrr = (1.0 / hit_positions[0]) if hit_positions else 0.0

    forbidden_keys: set[tuple[str, int, int]] = set()
    for item in forbidden_chunks or []:
        forbidden_keys.add(_normalize_chunk_ref(item))
    forbidden_sec_pairs: set[tuple[str, str]] = set()
    for item in forbidden_sections or []:
        if isinstance(item, dict):
            aid = str(item.get("article_id", "")).strip()
            sec = str(item.get("section", "")).strip()
            if aid and sec:
                forbidden_sec_pairs.add((aid, sec))
    forbidden_clean = not any(k in forbidden_keys for k in ranked_keys)
    if forbidden_sec_pairs:
        for row in top:
            aid = _row_article_id(row)
            sec = _row_section_title(row)
            if (aid, sec) in forbidden_sec_pairs:
                forbidden_clean = False
                break

    return {
        "precision_at_k": round(precision, 4),
        "recall_at_k": round(recall, 4),
        "mrr": round(mrr, 4),
        "hit_at_k": bool(hit_positions),
        "forbidden_clean": forbidden_clean,
        "retrieved_chunk_keys": [f"{a}:{c}:{r}" for a, c, r in ranked_keys],
        "expected_chunk_keys": [f"{a}:{c}:{r}" for a, c, r in expected_keys],
        "retrieved_ids": ranked_pids,
    }


def score_kb_rag_case(
    hits: list[dict[str, Any]],
    expect: dict[str, Any],
    *,
    section_order: list[str] | None = None,
) -> dict[str, Any]:
    k = int(expect.get("k", 10) or 10)
    metrics = kb_retrieval_metrics(
        hits,
        expected_chunks=expect.get("expected_chunks") or [],
        expected_sections=expect.get("expected_sections") or [],
        forbidden_chunks=expect.get("forbidden_chunks") or [],
        forbidden_sections=expect.get("forbidden_sections") or [],
        k=k,
        section_order=section_order,
    )
    failures: list[str] = []

    if expect.get("min_recall_at_k") is not None and metrics["recall_at_k"] < float(expect["min_recall_at_k"]):
        failures.append(f"recall@{k}={metrics['recall_at_k']} < {expect['min_recall_at_k']}")
    if expect.get("min_precision_at_k") is not None and metrics["precision_at_k"] < float(expect["min_precision_at_k"]):
        failures.append(f"precision@{k}={metrics['precision_at_k']} < {expect['min_precision_at_k']}")
    if expect.get("min_mrr") is not None and metrics["mrr"] < float(expect["min_mrr"]):
        failures.append(f"mrr={metrics['mrr']} < {expect['min_mrr']}")
    if expect.get("require_hit_at_k") and not metrics["hit_at_k"]:
        failures.append("hit@k=false")
    if expect.get("require_forbidden_clean") and not metrics["forbidden_clean"]:
        failures.append("forbidden chunk in top-k")

    return {"passed": len(failures) == 0, "failures": failures, "metrics": metrics}
