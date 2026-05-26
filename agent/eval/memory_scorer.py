"""长期记忆检索精度：Precision@K / Recall@K / MRR（离线直调 get_long_term_context）。"""

from __future__ import annotations

from typing import Any


def _memory_id(row: dict[str, Any]) -> str:
    mid = str(row.get("memory_id", "")).strip()
    if mid:
        return mid
    store = str(row.get("store", "")).strip()
    if store == "mongo":
        return str(row.get("fingerprint", "")).strip()
    if store == "qdrant":
        return str(row.get("point_id", "")).strip()
    return ""


def _normalize_expected(item: Any) -> tuple[str, str]:
    """返回 (store, id)。item 可为 str 或 {store, id}。"""
    if isinstance(item, dict):
        return str(item.get("store", "")).strip(), str(item.get("id", "")).strip()
    s = str(item).strip()
    if ":" in s:
        store, mid = s.split(":", 1)
        return store.strip(), mid.strip()
    return "", s


def retrieval_metrics(
    hits: list[dict[str, Any]],
    *,
    expected_ids: list[Any],
    forbidden_ids: list[Any] | None = None,
    k: int = 10,
) -> dict[str, Any]:
    """对召回列表计算检索指标。"""
    k = max(1, int(k))
    top = hits[:k]
    ranked_ids: list[str] = []
    ranked_pairs: list[tuple[str, str]] = []
    for row in top:
        if not isinstance(row, dict):
            continue
        store = str(row.get("store", "")).strip()
        mid = _memory_id(row)
        if mid:
            ranked_ids.append(mid)
            ranked_pairs.append((store, mid))

    expected: list[tuple[str, str]] = []
    for item in expected_ids or []:
        store, mid = _normalize_expected(item)
        if mid:
            expected.append((store, mid) if store else ("", mid))

    expected_mids = {mid for _, mid in expected}
    if not expected_mids:
        return {
            "precision_at_k": 1.0,
            "recall_at_k": 1.0,
            "mrr": 1.0,
            "hit_at_k": True,
            "forbidden_clean": True,
            "retrieved_ids": ranked_ids,
            "expected_ids": list(expected_mids),
        }

    hit_positions: list[int] = []
    for i, mid in enumerate(ranked_ids, start=1):
        if mid in expected_mids:
            hit_positions.append(i)

    true_pos = len(set(ranked_ids) & expected_mids)
    precision = true_pos / len(ranked_ids) if ranked_ids else 0.0
    recall = true_pos / len(expected_mids) if expected_mids else 1.0
    mrr = (1.0 / hit_positions[0]) if hit_positions else 0.0
    hit_at_k = bool(hit_positions)

    forbidden_mids: set[str] = set()
    for item in forbidden_ids or []:
        _, mid = _normalize_expected(item)
        if mid:
            forbidden_mids.add(mid)
    forbidden_clean = not any(mid in forbidden_mids for mid in ranked_ids)

    return {
        "precision_at_k": round(precision, 4),
        "recall_at_k": round(recall, 4),
        "mrr": round(mrr, 4),
        "hit_at_k": hit_at_k,
        "forbidden_clean": forbidden_clean,
        "retrieved_ids": ranked_ids,
        "expected_ids": list(expected_mids),
    }


def score_memory_case(
    hits: list[dict[str, Any]],
    expect: dict[str, Any],
) -> dict[str, Any]:
    k = int(expect.get("k", 10) or 10)
    metrics = retrieval_metrics(
        hits,
        expected_ids=expect.get("expected_ids") or [],
        forbidden_ids=expect.get("forbidden_ids") or [],
        k=k,
    )
    failures: list[str] = []

    min_recall = expect.get("min_recall_at_k")
    if min_recall is not None and metrics["recall_at_k"] < float(min_recall):
        failures.append(f"recall@{k}={metrics['recall_at_k']} < {min_recall}")

    min_precision = expect.get("min_precision_at_k")
    if min_precision is not None and metrics["precision_at_k"] < float(min_precision):
        failures.append(f"precision@{k}={metrics['precision_at_k']} < {min_precision}")

    min_mrr = expect.get("min_mrr")
    if min_mrr is not None and metrics["mrr"] < float(min_mrr):
        failures.append(f"mrr={metrics['mrr']} < {min_mrr}")

    if expect.get("require_hit_at_k") and not metrics["hit_at_k"]:
        failures.append("hit@k=false")

    if expect.get("require_forbidden_clean") and not metrics["forbidden_clean"]:
        failures.append("forbidden id appeared in top-k")

    passed = len(failures) == 0
    return {"passed": passed, "failures": failures, "metrics": metrics}
