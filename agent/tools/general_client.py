"""
通用工具组（general）— Agent 端本地执行，不走 Blog API。
"""

from __future__ import annotations

import ast
import operator
import re
from datetime import datetime
from typing import Any
from zoneinfo import ZoneInfo

_WEEKDAY_ZH = ("周一", "周二", "周三", "周四", "周五", "周六", "周日")

_BIN_OPS: dict[type, Any] = {
    ast.Add: operator.add,
    ast.Sub: operator.sub,
    ast.Mult: operator.mul,
    ast.Div: operator.truediv,
    ast.FloorDiv: operator.floordiv,
    ast.Mod: operator.mod,
    ast.Pow: operator.pow,
}
_UNARY_OPS: dict[type, Any] = {
    ast.UAdd: operator.pos,
    ast.USub: operator.neg,
}


def _safe_eval_node(node: ast.AST) -> float:
    if isinstance(node, ast.Expression):
        return _safe_eval_node(node.body)
    if isinstance(node, ast.Constant):
        if isinstance(node.value, (int, float)):
            return float(node.value)
        raise ValueError("表达式只能包含数字与运算符")
    if isinstance(node, ast.UnaryOp):
        op = _UNARY_OPS.get(type(node.op))
        if op is None:
            raise ValueError("不支持的 unary 运算符")
        return float(op(_safe_eval_node(node.operand)))
    if isinstance(node, ast.BinOp):
        op = _BIN_OPS.get(type(node.op))
        if op is None:
            raise ValueError("不支持的 binary 运算符")
        left = _safe_eval_node(node.left)
        right = _safe_eval_node(node.right)
        return float(op(left, right))
    raise ValueError("表达式格式无效")


def get_current_time(timezone: str = "Asia/Shanghai") -> dict[str, Any]:
    """返回指定时区的当前时间。"""
    tz_name = (timezone or "Asia/Shanghai").strip() or "Asia/Shanghai"
    try:
        tz = ZoneInfo(tz_name)
    except Exception:
        tz = ZoneInfo("Asia/Shanghai")
        tz_name = "Asia/Shanghai"
    now = datetime.now(tz)
    return {
        "datetime": now.strftime("%Y-%m-%d %H:%M:%S"),
        "iso": now.isoformat(),
        "timezone": tz_name,
        "weekday": _WEEKDAY_ZH[now.weekday()],
        "timestamp": int(now.timestamp()),
    }


def calculate(expression: str) -> dict[str, Any]:
    """安全计算四则运算表达式（仅数字与 + - * / // % ** 及括号）。"""
    expr = (expression or "").strip()
    if not expr:
        return {"_error": "表达式不能为空"}
    if len(expr) > 200:
        return {"_error": "表达式过长（最多 200 字符）"}
    try:
        tree = ast.parse(expr, mode="eval")
        value = _safe_eval_node(tree)
    except (SyntaxError, ValueError, TypeError, ZeroDivisionError, OverflowError) as e:
        return {"_error": f"无法计算: {e}"}
    if value == int(value):
        display = str(int(value))
    else:
        display = str(round(value, 10)).rstrip("0").rstrip(".")
    return {"expression": expr, "result": value, "result_text": display}


def text_stats(text: str) -> dict[str, Any]:
    """统计文本字数、行数、段落数等（Agent 本地）。"""
    if text is None or not str(text).strip():
        return {"_error": "文本不能为空"}
    if len(text) > 50_000:
        return {"_error": "文本过长（最多 50000 字符）"}

    stripped = text.strip()
    lines = text.splitlines()
    non_empty_lines = sum(1 for ln in lines if ln.strip())
    paragraphs = len([p for p in re.split(r"\n\s*\n", stripped) if p.strip()]) or 1
    cjk_chars = len(re.findall(r"[\u4e00-\u9fff]", text))
    latin_words = len(re.findall(r"[a-zA-Z]+(?:'[a-zA-Z]+)?", text))
    digits = len(re.findall(r"\d", text))

    return {
        "chars": len(text),
        "chars_no_whitespace": sum(1 for c in text if not c.isspace()),
        "lines": len(lines) if lines else 1,
        "non_empty_lines": non_empty_lines,
        "paragraphs": paragraphs,
        "cjk_chars": cjk_chars,
        "latin_words": latin_words,
        "digits": digits,
    }
