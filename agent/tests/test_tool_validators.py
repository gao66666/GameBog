"""工具参数契约校验。"""

from tools.tool_runtime import execute_tool, load_registry

load_registry.cache_clear()


def test_game_id_rejects_small_int():
    out = execute_tool("get_game_detail", {"game_id": 1})
    assert "无效" in out or "雪花" in out or "禁止" in out
    assert "_error" not in out or "HTTP" not in out


def test_game_id_accepts_snowflake_string():
    out = execute_tool("get_game_detail", {"game_id": "2052398853495197696"})
    assert "无效" not in out[:80]
    assert "禁止" not in out[:80]


def test_list_games_no_required_args():
    out = execute_tool("list_all_games", {})
    assert "未知工具" not in out


def test_search_requires_query():
    out = execute_tool("search_articles", {"query": "  "})
    assert "非空" in out


def test_sort_by_enum():
    out = execute_tool("get_article_leaderboard", {"sort_by": "views"})
    assert "view" in out.lower() or "仅允许" in out


def test_game_detail_includes_retail_price():
    import json

    out = execute_tool("get_game_detail", {"game_id": "2052398839796600832"})
    data = json.loads(out)
    assert "retail_price_cents" in data
    assert "retail_price_display" in data
    assert "points_mall" not in data
    assert data["retail_price_cents"] == 29800
    assert "298" in data["retail_price_display"]


def test_list_points_mall_products():
    import json

    out = execute_tool("list_points_mall_products", {})
    data = json.loads(out)
    assert "products" in data
    assert len(data["products"]) >= 1
    p = data["products"][0]
    assert "price_points" in p
    assert "retail_price_cents" not in p


def test_get_points_mall_product():
    import json

    lst = json.loads(execute_tool("list_points_mall_products", {}))
    pid = lst["products"][0]["id"]
    out = execute_tool("get_points_mall_product", {"product_id": pid})
    data = json.loads(out)
    assert data["id"] == pid
    assert "price_points" in data
