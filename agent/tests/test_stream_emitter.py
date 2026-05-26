"""stream_emitter 工具输出解析单测。"""

from core.stream_emitter import coerce_tool_output_text, parse_tool_output


def test_parse_article_search_json():
    raw = '{"total": 3, "articles": [{"id": 1}, {"id": 2}, {"id": 3}]}'
    parsed = parse_tool_output(raw)
    assert parsed["ok"] is True
    assert parsed["row_count"] == 3


def test_parse_article_list_key():
    raw = '{"total": 2, "article_list": [{"id": 1}, {"id": 2}]}'
    parsed = parse_tool_output(raw)
    assert parsed["row_count"] == 2


def test_parse_dict_output():
    parsed = parse_tool_output({"collections": [{"id": 1}]})
    assert parsed["row_count"] == 1

    parsed = parse_tool_output({"topics": [{}, {}]})
    assert parsed["row_count"] == 2

    parsed = parse_tool_output({"game_plays": [{}]})
    assert parsed["row_count"] == 1

    parsed = parse_tool_output({"reviews": [{}], "total": 99})
    assert parsed["row_count"] == 1


def test_coerce_python_repr():
    text = coerce_tool_output_text({"articles": [{"id": 1}]})
    parsed = parse_tool_output(text)
    assert parsed["row_count"] == 1
