"""Agent 进程内 Prometheus 指标（简历「可观测性」）。"""

from __future__ import annotations

from prometheus_client import Counter

CHAT_REQUESTS = Counter(
    "agent_chat_requests_total",
    "Chat POST /chat 计数",
    labelnames=("outcome",),
)
