"""memory_hints 排序、Sparse 归一化重排、Dense+Sparse 归一化融合。"""

from memory.memory_manager import (
    _fuse_dense_sparse_normalized,
    _hint_match_score,
    _hint_weighted_score_for_hit,
    _minmax_normalize_scores,
    _rerank_hits_by_memory_hints,
    sort_memory_hints_for_retrieval,
)


def test_sort_memory_hints_entity_first():
    assert sort_memory_hints_for_retrieval(["配置需求", "博德之门3"]) == ["博德之门3", "配置需求"]
    ordered = sort_memory_hints_for_retrieval(["玩法", "原神", "配置"])
    assert ordered[0] == "原神"
    assert set(ordered) == {"玩法", "原神", "配置"}


def test_hint_match_compact_space():
    blob = "【博德之门 3（Baldur's Gate 3） / 配置需求】\n\nWindows 10"
    assert _hint_match_score("博德之门3", blob) >= 0.95
    assert _hint_match_score("配置需求", blob) == 1.0


def test_minmax_normalize_flat():
    assert _minmax_normalize_scores({"a": 1.0, "b": 1.0}) == {"a": 1.0, "b": 1.0}


def test_rerank_sparse_prefers_game_over_generic_section():
    hits = [
        {
            "memory_id": "a",
            "game_name": "原神",
            "section_path": ["原神", "配置需求"],
            "content": "【原神 / 配置需求】 PC 最低",
            "sparse_score": 2.26,
            "score": 2.26,
        },
        {
            "memory_id": "b",
            "game_name": "博德之门3",
            "section_path": ["博德之门 3", "配置需求"],
            "content": "【博德之门 3 / 配置需求】 Windows 10",
            "sparse_score": 2.19,
            "score": 2.19,
        },
    ]
    ordered = _rerank_hits_by_memory_hints(hits, ["博德之门3", "配置需求"])
    assert ordered[0]["game_name"] == "博德之门3"
    assert ordered[0]["sparse_rerank_score"] >= ordered[1]["sparse_rerank_score"]


def test_fuse_dense_sparse_normalized():
    dense = [{"memory_id": "b", "dense_score": 0.9}, {"memory_id": "a", "dense_score": 0.5}]
    sparse = [{"memory_id": "a", "sparse_score": 10.0}, {"memory_id": "c", "sparse_score": 5.0}]
    fused = _fuse_dense_sparse_normalized(dense, sparse, dense_weight=0.5, sparse_weight=0.5)
    by_id = {pid: score for pid, score, _ in fused}
    assert by_id["a"] > by_id["c"]


def test_hint_weighted_score():
    hit = {
        "game_name": "博德之门3",
        "section_path": ["博德之门 3", "配置需求"],
        "content": "【博德之门 3 / 配置需求】",
    }
    s = _hint_weighted_score_for_hit(hit, ["博德之门3", "配置需求"])
    assert s > 1.0
