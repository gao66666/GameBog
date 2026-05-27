"""query 改写意图拆分（多路 Dense 检索）。"""

from core.rag import parse_rewrite_intents


def test_parse_single_intent():
    q = "艾尔登法环的电脑配置需求与推荐配置差异"
    assert parse_rewrite_intents(q) == [q]


def test_parse_multi_intent_semicolon():
    q = "战神2018的玩法特点；艾尔登法环的最低与推荐配置"
    assert len(parse_rewrite_intents(q)) == 2
    assert "战神" in parse_rewrite_intents(q)[0]
    assert "艾尔登" in parse_rewrite_intents(q)[1]


def test_parse_labeled_points():
    q = "要点一：只狼的核心玩法；要点二：只狼新手注意事项"
    parts = parse_rewrite_intents(q)
    assert len(parts) == 2
    assert "只狼" in parts[0] and "玩法" in parts[0]
    assert "注意事项" in parts[1]
