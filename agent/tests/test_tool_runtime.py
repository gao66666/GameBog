"""tool_runtime 响应 preset 与 Go API 字段对齐。"""

from tools.tool_runtime import _apply_response_preset


def test_games_list_reads_go_list_field():
    data = {
        "list": [
            {"id": 1, "name": "博德之门3", "publisher": "Larian", "developer": "Larian"},
            {"id": 2, "name": "艾尔登法环", "publisher": "Bandai", "developer": "FromSoftware"},
        ],
        "total": 2,
    }
    out = _apply_response_preset("games_list", data, {})
    assert len(out["games"]) == 2
    assert out["games"][0]["name"] == "博德之门3"
    assert out["total"] == 2


def test_games_list_empty_when_wrong_keys_only():
    out = _apply_response_preset("games_list", {"games": [], "total": 0}, {})
    assert out["games"] == []
