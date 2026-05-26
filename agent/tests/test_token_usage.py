"""token_usage 解析与聚合单测。"""

from types import SimpleNamespace

from core.token_usage import TurnUsageTracker, extract_usage_from_message, reset_turn_usage_tracker


def test_extract_usage_metadata():
    msg = SimpleNamespace(
        usage_metadata={"input_tokens": 100, "output_tokens": 50, "total_tokens": 150},
        response_metadata={},
    )
    u = extract_usage_from_message(msg)
    assert u["total_tokens"] == 150


def test_extract_openai_token_usage():
    msg = SimpleNamespace(
        usage_metadata=None,
        response_metadata={"token_usage": {"prompt_tokens": 80, "completion_tokens": 20}},
    )
    u = extract_usage_from_message(msg)
    assert u["input_tokens"] == 80 and u["output_tokens"] == 20


def test_tracker_by_phase():
    reset_turn_usage_tracker()
    t = TurnUsageTracker()
    t.add_usage({"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}, "rewrite")
    t.add_usage({"input_tokens": 100, "output_tokens": 40, "total_tokens": 140}, "output")
    d = t.to_dict()
    assert d["total_tokens"] == 155
    assert d["by_phase"]["rewrite"]["total_tokens"] == 15
