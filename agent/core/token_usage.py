"""单轮 /chat 的 LLM token 用量聚合（SSE done + agent_turn）。"""

from __future__ import annotations

from contextvars import ContextVar
from typing import Any

_turn_usage_ctx: ContextVar["TurnUsageTracker | None"] = ContextVar("turn_usage", default=None)


def extract_usage_from_message(msg: Any) -> dict[str, int]:
    """从 LangChain AIMessage 解析 input/output/total tokens。"""
    inp = out = total = 0

    um = getattr(msg, "usage_metadata", None)
    if um is not None:
        if isinstance(um, dict):
            inp = int(um.get("input_tokens") or um.get("prompt_tokens") or 0)
            out = int(um.get("output_tokens") or um.get("completion_tokens") or 0)
            total = int(um.get("total_tokens") or 0)
        else:
            inp = int(getattr(um, "input_tokens", 0) or getattr(um, "prompt_tokens", 0) or 0)
            out = int(getattr(um, "output_tokens", 0) or getattr(um, "completion_tokens", 0) or 0)
            total = int(getattr(um, "total_tokens", 0) or 0)

    meta = getattr(msg, "response_metadata", None)
    if isinstance(meta, dict):
        tu = meta.get("token_usage") or meta.get("usage")
        if isinstance(tu, dict):
            if not inp:
                inp = int(tu.get("prompt_tokens") or tu.get("input_tokens") or 0)
            if not out:
                out = int(tu.get("completion_tokens") or tu.get("output_tokens") or 0)
            if not total:
                total = int(tu.get("total_tokens") or 0)

    if not total and (inp or out):
        total = inp + out
    return {"input_tokens": inp, "output_tokens": out, "total_tokens": total}


class TurnUsageTracker:
    def __init__(self) -> None:
        self.input_tokens = 0
        self.output_tokens = 0
        self.total_tokens = 0
        self.by_phase: dict[str, dict[str, int]] = {}

    def add_usage(self, usage: dict[str, int], phase: str) -> None:
        inp = int(usage.get("input_tokens") or 0)
        out = int(usage.get("output_tokens") or 0)
        tot = int(usage.get("total_tokens") or 0) or (inp + out)
        if not (inp or out or tot):
            return
        self.input_tokens += inp
        self.output_tokens += out
        self.total_tokens += tot
        bucket = self.by_phase.setdefault(phase, {"input_tokens": 0, "output_tokens": 0, "total_tokens": 0})
        bucket["input_tokens"] += inp
        bucket["output_tokens"] += out
        bucket["total_tokens"] += tot

    def add_from_message(self, msg: Any, phase: str) -> None:
        self.add_usage(extract_usage_from_message(msg), phase)

    def to_dict(self) -> dict[str, Any]:
        return {
            "input_tokens": self.input_tokens,
            "output_tokens": self.output_tokens,
            "total_tokens": self.total_tokens,
            "by_phase": dict(self.by_phase),
        }


def reset_turn_usage_tracker() -> TurnUsageTracker:
    tracker = TurnUsageTracker()
    _turn_usage_ctx.set(tracker)
    return tracker


def get_turn_usage_tracker() -> TurnUsageTracker | None:
    return _turn_usage_ctx.get()


def record_llm_usage(msg: Any, phase: str) -> None:
    tracker = _turn_usage_ctx.get()
    if tracker is not None:
        tracker.add_from_message(msg, phase)
