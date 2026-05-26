"""
长期记忆生命周期收口（在线主链路）。

| 时机 | 函数 |
|------|------|
| 生成前 | ``retrieve_long_term_for_turn`` |
| 中期摘要滚动后 | ``schedule_post_turn_memory`` → ``commit_long_term_after_short_mid`` |
| 会话结束 | ``commit_long_term_on_session_end`` |

离线 Turn 核查见 ``turn_audit`` + ``scripts/audit_turns.py``（读 agent_turn 日志，与在线解耦）。
"""

from __future__ import annotations

import asyncio
import traceback
from typing import Any

from infra.agent_log import get_logger
from infra.config import load_config
from memory.memory_manager import (
    get_long_term_context,
    maybe_commit_long_term,
    run_long_term_memory_write_agent,
)
from memory.memory_store import get_mid_summaries, get_short_messages

_log = get_logger("reflection")


async def commit_long_term_after_short_mid(
    *,
    user_id: int,
    chat_session_id: str,
    user_message: str,
    assistant_text: str,
) -> dict[str, int]:
    """短/中期已写入后：门控+抽取 → Mongo（画像/规则）+ 用户 Qdrant（事件/项目/经历）。"""
    text = (assistant_text or "").strip()
    if not text:
        return {"mongo": 0, "qdrant": 0}

    cfg = load_config().get("memory", {})
    short_limit = int(cfg.get("short_window_messages", 12))
    mid_limit = int(cfg.get("mid_max_items", 8))
    sid = (chat_session_id or "").strip() or "default"
    recent_messages = await get_short_messages(user_id, short_limit, sid)
    mid_summaries = await get_mid_summaries(user_id, mid_limit)

    return await run_long_term_memory_write_agent(
        user_id,
        reason="auto",
        user_message=user_message,
        assistant_text=text,
        recent_messages=recent_messages,
        mid_summaries=mid_summaries,
    )


async def retrieve_long_term_for_turn(
    user_id: int,
    query: str,
    *,
    mongo_limit: int | None = None,
    qdrant_limit: int | None = None,
    memory_hints: list[str] | None = None,
) -> list:
    """主链路在组装 messages 之前调用：Dense=检索改写句，Sparse=memory_hints。"""
    return await get_long_term_context(
        user_id,
        query,
        mongo_limit=mongo_limit,
        qdrant_limit=qdrant_limit,
        memory_hints=memory_hints,
    )


async def commit_long_term_on_session_end(
    user_id: int,
    reason: str = "session_end",
    *,
    chat_session_id: str | None = None,
) -> dict[str, int]:
    """``/memory/session-end``：剩余短期压中期 → 长期写入（``reason`` 强制抽取）；再清 session。"""
    from memory.memory_turn import flush_mid_summary_on_session_end

    _, last_user, last_asst = await flush_mid_summary_on_session_end(
        user_id, chat_session_id=chat_session_id
    )
    return await maybe_commit_long_term(
        user_id,
        reason=reason,
        user_message=last_user,
        assistant_text=last_asst,
    )


def schedule_post_turn_memory(
    *,
    user_id: int,
    request_id: str,
    user_message: str,
    assistant_text: str,
    chat_session_id: str,
    early_exit: str | None,
    planning_cycle_complete: bool,
    timings: dict[str, Any],
) -> None:
    """中期摘要滚动后异步长期写入；失败不影响 SSE。"""
    orch = load_config().get("orchestration", {})
    log_base = bool(orch.get("post_turn_async_log", True))
    text = (assistant_text or "").strip()
    if not text and not log_base:
        return

    async def _run() -> None:
        extra: dict[str, Any] = {
            "user_id": user_id,
            "phase": "post_turn_memory",
            "early_exit": early_exit or "",
            "planning_cycle_complete": planning_cycle_complete,
            "timings": timings,
            "assistant_chars": len(text),
            "user_chars": len((user_message or "").strip()),
        }
        if request_id:
            extra["request_id"] = request_id
        try:
            if text:
                try:
                    commit_out = await commit_long_term_after_short_mid(
                        user_id=user_id,
                        chat_session_id=chat_session_id,
                        user_message=user_message,
                        assistant_text=text,
                    )
                    extra["long_term_commit"] = commit_out
                except Exception:
                    _log.warning(
                        "long_term_commit_async_failed",
                        extra={**extra, "traceback": traceback.format_exc()[:1200]},
                    )
            if log_base:
                _log.info("post_turn_memory", extra=extra)
        except Exception:
            _log.warning(
                "post_turn_memory_failed",
                extra={"request_id": request_id, "traceback": traceback.format_exc()[:1200]},
            )

    try:
        asyncio.create_task(_run())
    except RuntimeError:
        _log.warning("post_turn_memory skipped no event loop")


# 兼容旧 import
schedule_async_self_reflection = schedule_post_turn_memory
