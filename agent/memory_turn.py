"""单轮对话后主链路落盘：短期 Redis → 中期滚动摘要。

长期记忆：检索 ``reflection.retrieve_long_term_for_turn``、轮末写入 ``reflection.commit_long_term_after_short_mid``（异步任务内）。
"""

from agent_log import get_logger
from memory_store import append_short_messages, get_short_messages, pop_short_messages, update_mid_summary

_log = get_logger("memory_turn")


async def persist_chat_turn(
    *,
    user_id: int,
    user_message: str,
    assistant_text: str,
    short_ttl_minutes: int,
    short_limit: int,
    recent_limit: int,
    chat_session_id: str,
    mid_summaries: list,
    recent_messages: list,
    request_id: str = "",
) -> None:
    text = (assistant_text or "").strip()
    if not text:
        return
    extra = {"user_id": user_id, "phase": "memory_persist"}
    if request_id:
        extra["request_id"] = request_id
    _log.info("memory_turn persist start", extra=extra)

    prev_summary = mid_summaries[-1] if mid_summaries else None
    await append_short_messages(
        user_id,
        [
            {"role": "user", "content": user_message},
            {"role": "assistant", "content": text},
        ],
        short_ttl_minutes,
        chat_session_id,
    )
    current_short_messages = await get_short_messages(user_id, short_limit, chat_session_id)
    if len(current_short_messages) >= short_limit:
        summary_chunk = current_short_messages[:recent_limit]
        new_summary = await update_mid_summary(user_id, prev_summary, summary_chunk)
        if new_summary is not None:
            await pop_short_messages(user_id, recent_limit, chat_session_id)
            _log.debug("memory_turn mid_summary_rolled", extra=extra)

    _log.info("memory_turn persist done", extra=extra)
