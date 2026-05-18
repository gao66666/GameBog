"""
与简历「Agent 多阶段编排」对齐的显式阶段名（SSE / 日志用）。
约束：resolution=proceed 时必须依次完成 ANALYSIS_PRE → RETRIEVE → PLAN → ANALYSIS_CLOSE → EXECUTE；
追问/拒绝（clarify/cannot）为有意短路，不跑后续阶段。
"""

from __future__ import annotations

from enum import StrEnum


class ChatPhase(StrEnum):
    REWRITE = "rewrite"
    ROUTE = "route"
    ANALYSIS_PRE = "analysis_pre"
    RETRIEVE = "retrieve"
    PLAN = "plan"
    ANALYSIS_CLOSE = "analysis_close"
    EXECUTE = "execute"
    DONE = "done"


# proceed 路径下必须全部打勾后，才允许进入 EXECUTE（由 main 校验）
PROCEED_FULL_CYCLE: tuple[ChatPhase, ...] = (
    ChatPhase.ANALYSIS_PRE,
    ChatPhase.RETRIEVE,
    ChatPhase.PLAN,
    ChatPhase.ANALYSIS_CLOSE,
    ChatPhase.EXECUTE,
)
