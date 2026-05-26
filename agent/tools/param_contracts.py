"""工具参数契约：与 shared/tools_registry.json 中 validators / x-contract 对齐。"""

from __future__ import annotations

from typing import Any

# 博客侧 game/article/user 等为雪花 ID（约 19 位）；topic 等仍可能为小整数
SNOWFLAKE_ID_MIN = 1_000_000_000_000_000

CONTRACT_RULES: dict[str, str] = {
    "non_empty_str": "非空字符串",
    "snowflake_id": f"雪花 ID（≥{SNOWFLAKE_ID_MIN}），须从列表/搜索工具返回的 id 原样复制，禁止 1/2/3 等猜测值",
    "positive_int": "正整数（≥1）",
    "page": "页码，整数 ≥1，默认 1",
    "page_size": "每页条数，整数 1～100",
    "enum_view_like": "仅允许 view 或 like",
    "search_result_limit": "搜索结果条数，整数 3～5",
}


def parse_positive_int(val: Any) -> int | None:
    if val is None or isinstance(val, bool):
        return None
    if isinstance(val, int):
        return val if val > 0 else None
    if isinstance(val, float) and val.is_integer() and val > 0:
        return int(val)
    if isinstance(val, str):
        s = val.strip()
        if s.isdigit():
            try:
                return int(s)
            except ValueError:
                return None
    return None


def validate_contract(rule: str, val: Any, *, field: str = "") -> str | None:
    """通过返回 None，失败返回给模型看的错误文案。"""
    label = field or "参数"

    if rule == "non_empty_str":
        if not isinstance(val, str) or not val.strip():
            return f"{label} 必须为非空字符串"
        return None

    if rule in ("snowflake_id", "positive_int", "page", "page_size"):
        n = parse_positive_int(val)
        if n is None:
            return f"{label} 必须是正整数"
        if rule == "snowflake_id" and n < SNOWFLAKE_ID_MIN:
            return (
                f"{label} 无效：须使用列表/搜索工具返回的 id 原样传入（雪花 ID），"
                f"禁止猜测 1、2、3 等小数字"
            )
        if rule == "page" and n < 1:
            return f"{label} 页码须 ≥1"
        if rule == "page_size" and not (1 <= n <= 100):
            return f"{label} 每页条数须在 1～100"
        return None

    if rule == "enum_view_like":
        if not isinstance(val, str):
            return f"{label} 必须是字符串 view 或 like"
        v = val.strip().lower()
        if v not in ("view", "like"):
            return f"{label} 仅允许 view（阅读量）或 like（点赞数）"
        return None

    if rule == "search_result_limit":
        n = parse_positive_int(val)
        if n is None or not (3 <= n <= 5):
            return f"{label} 须在 3～5 之间"
        return None

    return None


def contracts_for_schema(schema: dict[str, Any]) -> list[tuple[str, str]]:
    """从 input_schema.properties 的 x-contract 提取 (field, rule)。"""
    props = schema.get("properties") or {}
    if not isinstance(props, dict):
        return []
    out: list[tuple[str, str]] = []
    for key, meta in props.items():
        if not isinstance(meta, dict):
            continue
        rule = meta.get("x-contract")
        if isinstance(rule, str) and rule.strip():
            out.append((key, rule.strip()))
    return out
