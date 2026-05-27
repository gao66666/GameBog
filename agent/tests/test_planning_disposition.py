"""单轮 disposition：仅 proceed / answer / clarify / cannot。"""

from core.planning_round import (
    VALID_DISPOSITIONS,
    normalize_disposition,
)


def test_normalize_four_states():
    assert normalize_disposition({"disposition": "proceed"}) == "proceed"
    assert normalize_disposition({"disposition": "answer"}) == "answer"
    assert normalize_disposition({"disposition": "clarify"}) == "clarify"
    assert normalize_disposition({"disposition": "cannot"}) == "cannot"


def test_legacy_replan_close_map_to_proceed():
    assert normalize_disposition({"disposition": "replan"}) == "proceed"
    assert normalize_disposition({"disposition": "close"}) == "proceed"
    assert normalize_disposition({"disposition": "retry"}) == "proceed"
    assert normalize_disposition({"disposition": "done"}) == "proceed"


def test_valid_dispositions_set():
    assert VALID_DISPOSITIONS == frozenset({"proceed", "clarify", "cannot", "answer"})
