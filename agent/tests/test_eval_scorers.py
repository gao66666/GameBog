"""eval 打分器单元测试（无需 Agent 进程）。"""

from eval.memory_scorer import retrieval_metrics, score_memory_case
from eval.scorer import aggregate_batch, compute_task_completed, score_turn
from eval.tool_scorer import score_tool_accuracy
from eval.ab_compare import compare_ab


def test_tool_args_contains():
    turn = {
        "tools": [
            {
                "tool": "search_articles",
                "ok": True,
                "args_summary": {"query": "Go 语言"},
            }
        ],
        "tool_retrieve": {"tools": ["search_articles", "list_latest_articles"]},
    }
    r = score_tool_accuracy(
        turn,
        {
            "tool_must_first": "search_articles",
            "tool_args": {"search_articles": {"query_contains": "Go", "keys_present": ["query"]}},
            "tool_retrieve_must_include": ["search_articles"],
        },
    )
    assert r["passed"], r


def test_tool_args_id_present():
    turn = {
        "tools": [
            {"tool": "get_article_detail", "ok": True, "args_summary": {"article_id": 1}},
        ],
    }
    r = score_tool_accuracy(
        turn,
        {"tool_args": {"get_article_detail": {"article_id_present": True}}},
    )
    assert r["passed"], r


def test_memory_precision_recall():
    hits = [
        {"store": "mongo", "memory_id": "a"},
        {"store": "mongo", "memory_id": "b"},
        {"store": "qdrant", "memory_id": "noise"},
    ]
    m = retrieval_metrics(hits, expected_ids=["a", "c"], forbidden_ids=["noise"], k=3)
    assert m["recall_at_k"] == 0.5
    assert m["hit_at_k"] is True
    assert m["forbidden_clean"] is False

    scored = score_memory_case(
        hits,
        {
            "k": 3,
            "expected_ids": ["a"],
            "min_recall_at_k": 1.0,
            "require_forbidden_clean": False,
        },
    )
    assert scored["passed"]


def test_task_completed_and_batch_timing():
    turn_ok = {
        "status": "ok",
        "output": {"text": "现在是 12:00"},
        "tools": [{"tool": "get_current_time", "ok": True}],
        "analyses": [{"disposition": "answer"}],
        "timings": {"total_ms": 3000, "execute_ms": 800},
    }
    scored = score_turn(turn_ok, {"status": "ok"}, has_token=False)
    assert scored["metrics"]["task_completed"]
    assert scored["metrics"]["timing_execute_ms"] == 800

    turn_clarify = {
        "status": "ok",
        "early_exit": "clarify",
        "output": {"text": ""},
        "analyses": [{"disposition": "clarify"}],
    }
    assert not compute_task_completed(turn_clarify, {"stream_ok": True, "tools_all_ok": True})

    results = [
        {"passed": True, "metrics": {"task_completed": True, "timing_total_ms": 1000, "stream_ok": True, "route_match": True, "tool_call_count": 0, "tools_all_ok": True}},
        {"passed": False, "metrics": {"task_completed": False, "timing_total_ms": 5000, "stream_ok": True, "route_match": True, "tool_call_count": 0, "tools_all_ok": True}},
    ]
    agg = aggregate_batch(results, {})
    assert agg["task_completion_rate"] == 0.5
    assert agg["avg_total_ms"] == 3000


def test_ab_compare_regression():
    control = {
        "aggregate": {"case_pass_rate": 1.0, "total_ms_p95": 10000},
        "results": [
            {"case_id": "a", "passed": True},
            {"case_id": "b", "passed": True},
        ],
    }
    treatment = {
        "aggregate": {"case_pass_rate": 0.5, "total_ms_p95": 12000},
        "results": [
            {"case_id": "a", "passed": True},
            {"case_id": "b", "passed": False, "failures": ["x"]},
        ],
    }
    c = compare_ab(control, treatment, min_pass_rate_delta=-0.05, max_regression_cases=0)
    assert not c["ab_pass"]
    assert len(c["regressions"]) == 1
