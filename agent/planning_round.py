"""
强制一轮：分析₁（不含长期记忆）→ 检索 → 计划（含长期）→ 分析₂（收口）→ 执行。
首轮分析可选短路：追问（clarify）或直接告知无法完成（cannot），此时不跑检索/计划/收口，也不走工具执行器。
显式阶段名见 chat_phases.PROCEED_FULL_CYCLE；主流程校验见 main.chat event_stream。
"""

from __future__ import annotations

import json
import re
import time
from typing import Any

from langchain_openai import ChatOpenAI
from langchain_core.messages import HumanMessage

from agent_log import get_logger
from agent_core import build_tool_short_catalog, format_tools_full_detail
from config import get_prompts, load_config

_log = get_logger("planning_round")


def _strip_json_fence(text: str) -> str:
    t = (text or "").strip()
    if t.startswith("```"):
        t = re.sub(r"^```(?:json)?\s*", "", t, flags=re.IGNORECASE)
        if t.endswith("```"):
            t = t[:-3].strip()
    return t


def _safe_json_loads(text: str) -> dict[str, Any] | None:
    try:
        obj = json.loads(_strip_json_fence(text))
        return obj if isinstance(obj, dict) else None
    except json.JSONDecodeError:
        return None


def analysis_pre_resolution(analysis_pre: dict[str, Any]) -> str:
    """归一化首轮分析的 dispositions：proceed | clarify | cannot。"""
    raw = str(analysis_pre.get("resolution") or "proceed").strip()
    key = raw.lower()
    if key in ("proceed", "continue", "go", "ok", ""):
        return "proceed"
    if key in ("clarify", "ask", "question") or raw in ("追问", "追问澄清"):
        return "clarify"
    if key in ("cannot", "reject", "deny", "unable", "impossible") or raw in ("无法完成", "不可行"):
        return "cannot"
    if "追问" in raw or "澄清" in raw:
        return "clarify"
    if "无法" in raw or "不能完成" in raw or "做不到" in raw:
        return "cannot"
    return "proceed"


def early_reply_from_analysis_pre(analysis_pre: dict[str, Any]) -> str | None:
    """若首轮分析要求追问或拒答，返回应对用户展示的全文；否则 None（走正常计划链路）。"""
    r = analysis_pre_resolution(analysis_pre)
    if r == "clarify":
        msg = (analysis_pre.get("clarify_message") or analysis_pre.get("clarify_questions") or "").strip()
        return msg if msg else None
    if r == "cannot":
        msg = (analysis_pre.get("cannot_reason") or analysis_pre.get("cannot_message") or "").strip()
        return msg if msg else None
    return None


def _truncate_json(data: Any, max_chars: int) -> str:
    s = json.dumps(data, ensure_ascii=False)
    if len(s) <= max_chars:
        return s
    return s[: max_chars - 20] + "\n…(truncated)…"


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


async def run_analysis_pre(
    *,
    user_message: str,
    rewritten: str,
    servers: list[str],
    recent_messages: list[dict],
    mid_summaries: list[dict],
    recent_json_max: int = 8000,
    mid_json_max: int = 6000,
) -> tuple[dict[str, Any], int]:
    t0 = time.time()
    recent_json = _truncate_json(recent_messages, recent_json_max)
    mid_json = _truncate_json(mid_summaries, mid_json_max)
    tools_short_catalog, code_to_name = build_tool_short_catalog(servers)
    prompt = get_prompts()["analysis_pre"].format(
        servers=json.dumps(servers, ensure_ascii=False),
        tools_short_catalog=tools_short_catalog,
        user_message=user_message.strip(),
        rewritten=(rewritten or user_message).strip(),
        recent_json=recent_json,
        mid_json=mid_json,
    )
    llm = _struct_llm()
    resp = await llm.ainvoke([HumanMessage(content=prompt)])
    raw = getattr(resp, "content", None) or ""
    parsed = _safe_json_loads(str(raw))
    if not parsed:
        _log.warning("analysis_pre JSON 解析失败，使用空对象降级")
        parsed = {
            "intent": "",
            "resolution": "proceed",
            "notes_for_planner": "",
            "clarify_message": "",
            "cannot_reason": "",
            "candidate_tool_codes": [],
        }
    parsed.setdefault("intent", "")
    parsed.setdefault("resolution", "proceed")
    parsed.setdefault("notes_for_planner", "")
    parsed.setdefault("clarify_message", "")
    parsed.setdefault("cannot_reason", "")
    raw_codes = parsed.get("candidate_tool_codes")
    if not isinstance(raw_codes, list):
        raw_codes = []
    norm_codes = [str(c).strip() for c in raw_codes if str(c).strip()][:24]
    parsed["candidate_tool_codes"] = [c for c in norm_codes if c in code_to_name]
    ms = int((time.time() - t0) * 1000)
    return parsed, ms


async def run_plan(
    *,
    analysis_pre: dict[str, Any],
    user_message: str,
    rewritten: str,
    servers: list[str],
    long_term_context: list[dict],
    long_term_json_max: int = 12000,
) -> tuple[dict[str, Any], int]:
    t0 = time.time()
    long_term_json = _truncate_json(long_term_context, long_term_json_max)
    analysis_pre_json = json.dumps(analysis_pre, ensure_ascii=False)[:8000]
    _, code_to_name = build_tool_short_catalog(servers)
    codes = analysis_pre.get("candidate_tool_codes") or []
    if not isinstance(codes, list):
        codes = []
    names_ordered: list[str] = []
    seen: set[str] = set()
    for c in codes:
        c = str(c).strip()
        nm = code_to_name.get(c)
        if nm and nm not in seen:
            seen.add(nm)
            names_ordered.append(nm)
    if not names_ordered:
        names_ordered = list(code_to_name.values())
    tools_detail = format_tools_full_detail(names_ordered, servers)
    prompt = get_prompts()["plan"].format(
        analysis_pre_json=analysis_pre_json,
        user_message=user_message.strip(),
        rewritten=(rewritten or user_message).strip(),
        servers=json.dumps(servers, ensure_ascii=False),
        long_term_json=long_term_json,
        tools_detail=tools_detail,
    )
    llm = _struct_llm()
    resp = await llm.ainvoke([HumanMessage(content=prompt)])
    raw = getattr(resp, "content", None) or ""
    parsed = _safe_json_loads(str(raw))
    if not parsed:
        _log.warning("plan JSON 解析失败，使用空计划降级")
        parsed = {"plan_steps": [], "requires_tools": True, "memory_digest": ""}
    if not isinstance(parsed.get("plan_steps"), list):
        parsed["plan_steps"] = []
    parsed["plan_steps"] = [str(x).strip() for x in parsed["plan_steps"] if str(x).strip()][:12]
    parsed.setdefault("requires_tools", True)
    parsed.setdefault("memory_digest", "")
    ms = int((time.time() - t0) * 1000)
    return parsed, ms


async def run_analysis_close(
    *,
    plan: dict[str, Any],
    user_message: str,
    long_term_context: list[dict],
    long_term_json_max: int = 12000,
) -> tuple[dict[str, Any], int]:
    t0 = time.time()
    plan_json = json.dumps(plan, ensure_ascii=False)[:12000]
    long_term_json = _truncate_json(long_term_context, long_term_json_max)
    prompt = get_prompts()["analysis_close"].format(
        plan_json=plan_json,
        long_term_json=long_term_json,
        user_message=user_message.strip(),
    )
    llm = _struct_llm()
    resp = await llm.ainvoke([HumanMessage(content=prompt)])
    raw = getattr(resp, "content", None) or ""
    parsed = _safe_json_loads(str(raw))
    if not parsed:
        _log.warning("analysis_close JSON 解析失败，使用空收口降级")
        parsed = {"final_briefing": ""}
    parsed.setdefault("final_briefing", "")
    ms = int((time.time() - t0) * 1000)
    return parsed, ms


def build_round_system_message(
    *,
    analysis_pre: dict[str, Any],
    plan: dict[str, Any],
    close: dict[str, Any],
) -> str:
    """拼成一条 system 消息，插入执行器 messages（在长期记忆 JSON 之前或之后均可，此处由调用方决定顺序）。"""
    intent = str(analysis_pre.get("intent", "") or "").strip()
    notes = str(analysis_pre.get("notes_for_planner", "") or "").strip()
    steps = plan.get("plan_steps") or []
    req_tools = plan.get("requires_tools", True)
    digest = str(plan.get("memory_digest", "") or "").strip()
    briefing = str(close.get("final_briefing", "") or "").strip()

    lines = [
        "【分析→计划→分析（收口）】以下内容由上游模块生成，**你必须**在最终答复中落实；与工具事实冲突时以工具为准，但长期记忆中与问题相关的要点不得无故忽略。",
    ]
    if intent:
        lines.append(f"意图：{intent}")
    if notes:
        lines.append(f"首轮分析备注：{notes}")
    lines.append(f"计划阶段判定需要工具：{'是' if req_tools else '否（可无工具直接答复）'}")
    if steps:
        lines.append("计划步骤：")
        for i, s in enumerate(steps, 1):
            lines.append(f"  {i}. {s}")
    if digest:
        lines.append(f"长期记忆 digest：{digest}")
    if briefing:
        lines.append(f"收口口径：{briefing}")
    return "\n".join(lines)


def planning_round_enabled() -> bool:
    return bool(load_config().get("orchestration", {}).get("planning_round_enabled", True))
