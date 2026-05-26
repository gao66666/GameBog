"""
与 Agent 多阶段编排对齐的显式阶段名（SSE / 日志用）。
analyse 判定 disposition；output 撰写最终回复；execute 仅收集工具结果。
"""

from __future__ import annotations

from enum import StrEnum


class ChatPhase(StrEnum):
    REWRITE = "rewrite"
    ROUTE = "route"
    TOOL_RETRIEVE = "tool_retrieve"
    ANALYSE = "analyse"
    RETRIEVE = "retrieve"
    PLAN = "plan"
    EXECUTE = "execute"
    OUTPUT = "output"
    DONE = "done"
