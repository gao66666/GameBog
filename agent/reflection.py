"""
自我反思与纠正（规划异步）——简历 Agent 第 4 条。

在用户可见输出完成后，由 ``main`` 通过 ``asyncio.create_task`` 触发；**不阻塞 SSE**。

**「反思」异步任务**（``schedule_async_self_reflection`` 内）在对话结束后执行，且 **包含长期记忆落库**：先 ``commit_long_term_after_short_mid``（重载短/中期 → **单次 LLM 门控+抽取** → **决策 LLM + 落库**），再结构化日志，最后可选反思质检 LLM。

长期相关 API 在本模块收口（``main`` 不直接调 ``memory_manager`` 的长期函数）：

| 时机 | 函数 | 说明 |
|------|------|------|
| **生成前**（同步，仍在主链路） | ``retrieve_long_term_for_turn`` | 只读召回，注入 system |
| **SSE 结束后**（异步任务内） | ``commit_long_term_after_short_mid`` | 长期写入（与上表「反思」一体） |
| **会话结束**（HTTP） | ``commit_long_term_on_session_end`` | ``maybe_commit_long_term``；session 由调用方清理 |
"""

from __future__ import annotations

import asyncio
import json
import re
import time
import traceback
from typing import Any

from langchain_openai import ChatOpenAI
from langchain_core.messages import HumanMessage

from agent_log import get_logger
from config import load_config
from memory_manager import (
    get_long_term_context,
    maybe_commit_long_term,
    run_long_term_memory_write_agent,
)
from memory_store import get_mid_summaries, get_short_messages

_log = get_logger("reflection")


async def commit_long_term_after_short_mid(
    *,
    user_id: int,
    chat_session_id: str,
    user_message: str,
    assistant_text: str,
) -> dict[str, int]:
    """短/中期已写入后：重载 Redis → 触发判定 → Mongo/Qdrant 写入 Agent（与 memory_manager 原管线一致）。"""
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
) -> list:
    """主链路在组装 messages 之前调用：按当前改写/问题召回长期记忆（只读）。"""
    return await get_long_term_context(
        user_id,
        query,
        mongo_limit=mongo_limit,
        qdrant_limit=qdrant_limit,
    )


async def commit_long_term_on_session_end(user_id: int, reason: str = "session_end") -> dict[str, int]:
    """``/memory/session-end``：整块触发长期写入；``reason`` 参与触发判定（可无 user/assistant 正文）。"""
    return await maybe_commit_long_term(user_id, reason=reason)


REFLECTION_PROMPT = """你是「自我反思」模块（用户**永远看不到**本输出）。在单轮对话已结束后，只做质检与内省，**不得**编造工具未返回的事实。

用户原话（可截断）：
{user_message}

助手最终可见答复（可截断）：
{assistant_text}

是否经追问/拒答短路：{early_exit}
是否完成编排闭环（检索→计划→收口）：{planning_cycle_complete}

只输出一个 JSON（不要 markdown），字段：
{{
  "severity": "ok 或 warn",
  "alignment": "ok 或 partial 或 poor",
  "notes": "一两句中文：是否答非所问、遗漏关键点、无证据断言、语气风险等",
  "correction_hints": "给工程/迭代用的改进建议（中文短句）；无则空字符串"
}}"""


def _strip_json_fence(text: str) -> str:
    t = (text or "").strip()
    if t.startswith("```"):
        t = re.sub(r"^```(?:json)?\s*", "", t, flags=re.IGNORECASE)
        if t.endswith("```"):
            t = t[:-3].strip()
    return t


def _struct_llm() -> ChatOpenAI:
    cfg = load_config()
    sc = cfg.get("summary_llm") or {}
    llm_kwargs = dict(
        model=sc.get("model", cfg.get("model", "gpt-4o-mini")),
        base_url=sc.get("base_url", cfg.get("base_url")),
        api_key=sc.get("api_key", cfg.get("api_key")),
        temperature=0,
    )
    if sc.get("thinking") or cfg.get("thinking"):
        llm_kwargs["extra_body"] = {"thinking": {"type": "enabled"}}
    else:
        llm_kwargs["extra_body"] = {"thinking": {"type": "disabled"}}
    return ChatOpenAI(**llm_kwargs)


async def _run_reflection_llm(
    *,
    user_message: str,
    assistant_text: str,
    early_exit: str | None,
    planning_cycle_complete: bool,
    max_user: int,
    max_asst: int,
) -> dict[str, Any] | None:
    u = (user_message or "").strip()[:max_user]
    a = (assistant_text or "").strip()[:max_asst]
    if not a:
        return None
    prompt = REFLECTION_PROMPT.format(
        user_message=u or "（空）",
        assistant_text=a,
        early_exit=early_exit or "无",
        planning_cycle_complete=str(planning_cycle_complete),
    )
    llm = _struct_llm()
    resp = await llm.ainvoke([HumanMessage(content=prompt)])
    raw = getattr(resp, "content", None) or ""
    try:
        obj = json.loads(_strip_json_fence(str(raw)))
        return obj if isinstance(obj, dict) else None
    except json.JSONDecodeError:
        _log.warning("reflection LLM 返回非 JSON", extra={"preview": str(raw)[:400]})
        return None


def schedule_async_self_reflection(
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
    """在事件循环上挂起异步任务；失败不影响主响应。"""
    orch = load_config().get("orchestration", {})
    log_base = bool(orch.get("post_turn_async_log", True))
    llm_on = bool(orch.get("reflection_llm_enabled", False))
    max_u = int(orch.get("reflection_max_user_chars", 4000))
    max_a = int(orch.get("reflection_max_assistant_chars", 8000))

    text = (assistant_text or "").strip()
    if not text and not log_base and not llm_on:
        return

    async def _run() -> None:
        extra: dict[str, Any] = {
            "user_id": user_id,
            "phase": "self_reflection",
            "early_exit": early_exit or "",
            "planning_cycle_complete": planning_cycle_complete,
            "timings": timings,
            "assistant_chars": len((assistant_text or "").strip()),
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
                _log.info("self_reflection_turn", extra=extra)
            if llm_on and text:
                t0 = time.perf_counter()
                critique = await _run_reflection_llm(
                    user_message=user_message,
                    assistant_text=text,
                    early_exit=early_exit,
                    planning_cycle_complete=planning_cycle_complete,
                    max_user=max_u,
                    max_asst=max_a,
                )
                elapsed_ms = int((time.perf_counter() - t0) * 1000)
                if critique:
                    extra["reflection_ms"] = elapsed_ms
                    extra["reflection_severity"] = critique.get("severity", "")
                    extra["reflection_alignment"] = critique.get("alignment", "")
                    extra["reflection_notes"] = (critique.get("notes") or "")[:800]
                    extra["reflection_correction_hints"] = (critique.get("correction_hints") or "")[:800]
                    _log.info("self_reflection_llm", extra=extra)
                else:
                    extra["reflection_ms"] = elapsed_ms
                    _log.info("self_reflection_llm_empty", extra=extra)

        except Exception:
            _log.warning(
                "self_reflection_failed",
                extra={"request_id": request_id, "traceback": traceback.format_exc()[:1200]},
            )

    try:
        asyncio.create_task(_run())
    except RuntimeError:
        _log.warning("self_reflection skipped no event loop")
