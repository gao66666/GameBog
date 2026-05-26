"""turn_audit 规则与日志解析单测。"""

import json

from audit.turn_audit import extract_turn_from_log_line, run_rule_audit, sample_turns


def test_extract_zap_agent_turn_line():
    turn = {"schema_version": 1, "request_id": "r1", "status": "ok"}
    for line in (
        json.dumps({"level": "info", "msg": "agent_turn", "turn": turn}),
        json.dumps({"level": "info", "message": "agent_turn", "turn": turn}),
    ):
        got = extract_turn_from_log_line(line)
        assert got and got["request_id"] == "r1"


def test_extract_plain_jsonl():
    turn = {"schema_version": 1, "request_id": "r2", "status": "ok", "input": {"message": "hi"}}
    got = extract_turn_from_log_line(json.dumps(turn))
    assert got and got["request_id"] == "r2"


def test_rule_route_user_without_token():
    turn = {
        "schema_version": 1,
        "request_id": "x",
        "status": "ok",
        "route": {"servers": ["user", "public"], "has_token": False},
        "input": {"message": "test"},
    }
    issues = run_rule_audit(turn)
    assert any(i.get("layer") == "route" and i.get("severity") == "fail" for i in issues)


def test_sample_turns_reproducible():
    turns = [{"request_id": f"r{i}"} for i in range(20)]
    a = sample_turns(turns, 3, seed=7)
    b = sample_turns(turns, 3, seed=7)
    assert a == b
    assert len(a) == 3
