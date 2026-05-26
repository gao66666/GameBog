"""单轮对话后主链路落盘：短期 Redis → 中期滚动摘要。

长期记忆：检索 ``reflection.retrieve_long_term_for_turn``；轮末写入与中期摘要滚动同频；
会话结束见 ``flush_mid_summary_on_session_end`` + ``reflection.commit_long_term_on_session_end``。
"""

from infra.agent_log import get_logger
from infra.config import load_config
from memory.memory_store import (
    append_short_messages,
    delete_short_session,
    get_all_short_messages,
    get_mid_summaries,
    get_short_messages,
    list_short_session_ids,
    pop_short_messages,
    update_mid_summary,
)
from memory.memory_store import _norm_chat_session_id

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
) -> bool:
    """落盘短期消息；若触发中期摘要滚动则返回 True（与异步反思/长期写入同频）。"""
    text = (assistant_text or "").strip()
    if not text:
        return False
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
    mid_summary_rolled = False
    if len(current_short_messages) >= short_limit:
        summary_chunk = current_short_messages[:recent_limit]
        new_summary = await update_mid_summary(user_id, prev_summary, summary_chunk)
        if new_summary is not None:
            await pop_short_messages(user_id, recent_limit, chat_session_id)
            mid_summary_rolled = True
            _log.debug("memory_turn mid_summary_rolled", extra=extra)

    _log.info("memory_turn persist done", extra={**extra, "mid_summary_rolled": mid_summary_rolled})
    return mid_summary_rolled


def _last_user_assistant(messages: list[dict]) -> tuple[str, str]:
    last_user, last_asst = "", ""
    for msg in reversed(messages):
        role = str(msg.get("role", ""))
        content = str(msg.get("content", "")).strip()
        if not content:
            continue
        if role == "assistant" and not last_asst:
            last_asst = content
        elif role == "user" and not last_user:
            last_user = content
        if last_user and last_asst:
            break
    return last_user, last_asst


async def flush_mid_summary_on_session_end(
    user_id: int,
    *,
    chat_session_id: str | None = None,
) -> tuple[bool, str, str]:
    """会话结束：将剩余短期（不必满 short_limit）压入中期摘要；返回 (是否写入中期, 末轮 user, 末轮 assistant)。"""
    if user_id <= 0:
        return False, "", ""

    mem_cfg = load_config().get("memory", {})
    mid_limit = int(mem_cfg.get("mid_max_items", 8))
    mid_summaries = await get_mid_summaries(user_id, mid_limit)
    prev_summary = mid_summaries[-1] if mid_summaries else None

    if chat_session_id and chat_session_id.strip():
        session_ids = [_norm_chat_session_id(chat_session_id)]
    else:
        session_ids = await list_short_session_ids(user_id)
        if not session_ids:
            session_ids = ["default"]

    rolled = False
    last_user, last_asst = "", ""
    extra = {"user_id": user_id, "phase": "memory_session_flush"}

    for sid in session_ids:
        chunk = await get_all_short_messages(user_id, sid)
        if not chunk:
            await delete_short_session(user_id, sid)
            continue
        u, a = _last_user_assistant(chunk)
        if u:
            last_user = u
        if a:
            last_asst = a
        new_summary = await update_mid_summary(user_id, prev_summary, chunk)
        if new_summary is not None:
            prev_summary = new_summary
            rolled = True
            _log.info("memory_turn session_end mid_summary", extra={**extra, "chat_session_id": sid})
        await delete_short_session(user_id, sid)

    return rolled, last_user, last_asst
