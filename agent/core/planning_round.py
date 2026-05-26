"""
编排：analyse（disposition + 综合分析）→ plan → execute(仅工具) → [analyse 复检] → output 成稿。
"""

from __future__ import annotations

import json
import re
import time
from typing import Any, Literal

from langchain_openai import ChatOpenAI
from langchain_core.messages import HumanMessage

from infra.agent_log import get_logger
from core.agent_core import build_tool_short_catalog, format_tools_full_detail
from infra.config import get_prompts, load_config
from infra.llm_util import ainvoke_with_system, chat_openai_kwargs
from core.token_usage import record_llm_usage

_log = get_logger("planning_round")

TERMINAL_DISPOSITIONS = frozenset({"close", "clarify", "cannot", "answer"})
EXECUTE_DISPOSITIONS = frozenset({"proceed", "replan"})


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


def normalize_disposition(analyse: dict[str, Any]) -> str:
    """归一化 disposition：proceed | replan | close | clarify | cannot | answer。"""
    raw = str(analyse.get("disposition") or analyse.get("resolution") or "proceed").strip()
    key = raw.lower()
    mapping = {
        "proceed": "proceed",
        "continue": "proceed",
        "go": "proceed",
        "ok": "proceed",
        "replan": "replan",
        "retry": "replan",
        "again": "replan",
        "close": "close",
        "done": "close",
        "end": "close",
        "finish": "close",
        "answer": "answer",
        "direct": "answer",
        "clarify": "clarify",
        "ask": "clarify",
        "question": "clarify",
        "cannot": "cannot",
        "reject": "cannot",
        "unable": "cannot",
        "impossible": "cannot",
    }
    if key in mapping:
        return mapping[key]
    if raw in ("追问", "追问澄清") or "追问" in raw or "澄清" in raw:
        return "clarify"
    if raw in ("无法完成", "不可行") or "无法" in raw or "不能完成" in raw:
        return "cannot"
    return "proceed"


# 兼容旧 import
analysis_pre_resolution = normalize_disposition


def _truncate_json(data: Any, max_chars: int) -> str:
    s = json.dumps(data, ensure_ascii=False)
    if len(s) <= max_chars:
        return s
    return s[: max_chars - 20] + "\n…(truncated)…"


def _struct_llm() -> ChatOpenAI:
    cfg = load_config()
    sc = cfg.get("summary_llm") or {}
    merged = {**cfg, **sc}
    if sc.get("thinking") or cfg.get("thinking"):
        merged["thinking"] = True
    return ChatOpenAI(**chat_openai_kwargs(merged, temperature=0))


def _default_analyse() -> dict[str, Any]:
    return {
        "disposition": "proceed",
        "analysis": "",
        "intent": "",
        "notes_for_planner": "",
        "replan_reason": "",
        "clarify_message": "",
        "cannot_reason": "",
        "candidate_tool_codes": [],
    }


def _normalize_analyse(parsed: dict[str, Any], code_to_name: dict[str, str]) -> dict[str, Any]:
    parsed.setdefault("disposition", parsed.get("resolution", "proceed"))
    parsed.setdefault("analysis", parsed.get("intent", ""))
    parsed.setdefault("intent", "")
    parsed.setdefault("notes_for_planner", "")
    parsed.setdefault("replan_reason", "")
    parsed.setdefault("clarify_message", "")
    parsed.setdefault("cannot_reason", "")
    raw_codes = parsed.get("candidate_tool_codes")
    if not isinstance(raw_codes, list):
        raw_codes = []
    norm_codes = [str(c).strip() for c in raw_codes if str(c).strip()][:24]
    parsed["candidate_tool_codes"] = [c for c in norm_codes if c in code_to_name]
    return parsed


async def run_analyse(
    *,
    phase: Literal["pre", "post"],
    user_message: str,
    rewritten: str,
    servers: list[str],
    retrieved_tool_names: list[str],
    recent_messages: list[dict],
    mid_summaries: list[dict],
    tool_results: list[dict] | None = None,
    prior_analyse: dict[str, Any] | None = None,
    plan: dict[str, Any] | None = None,
    cycle: int = 0,
    recent_json_max: int = 8000,
    mid_json_max: int = 6000,
) -> tuple[dict[str, Any], int, dict[str, str]]:
    t0 = time.time()
    recent_json = _truncate_json(recent_messages, recent_json_max)
    mid_json = _truncate_json(mid_summaries, mid_json_max)
    tools_short_catalog, code_to_name = build_tool_short_catalog(
        servers, tool_names=retrieved_tool_names
    )

    if phase == "post":
        phase_label = f"execute 后复检（第 {cycle} 轮）"
        extra_context = "、本轮计划与工具调用结果"
        post_section = f"""
  本轮计划 JSON：
  {_truncate_json(plan or {}, 6000)}

  已有工具调用结果（JSON 数组）：
  {_truncate_json(tool_results or [], 12000)}

  上一轮分析 JSON：
  {_truncate_json(prior_analyse or {}, 4000)}
"""
    else:
        phase_label = "execute 前（首轮）"
        extra_context = "（此时尚无工具结果）"
        post_section = ""

    prompt = get_prompts()["analyse"].format(
        phase_label=phase_label,
        extra_context=extra_context,
        post_section=post_section,
        servers=json.dumps(servers, ensure_ascii=False),
        tools_short_catalog=tools_short_catalog,
        user_message=user_message.strip(),
        rewritten=(rewritten or user_message).strip(),
        recent_json=recent_json,
        mid_json=mid_json,
    )
    llm = _struct_llm()
    resp = await ainvoke_with_system(llm, human_content=prompt)
    record_llm_usage(resp, f"analyse_{phase}")
    raw = getattr(resp, "content", None) or ""
    parsed = _safe_json_loads(str(raw))
    if not parsed:
        _log.warning("analyse JSON 解析失败，降级为 proceed")
        parsed = _default_analyse()
    parsed = _normalize_analyse(parsed, code_to_name)

    disp = normalize_disposition(parsed)
    if phase == "pre" and disp in ("replan", "close"):
        _log.warning("analyse pre 阶段出现 %s，降级为 proceed", disp)
        parsed["disposition"] = "proceed"
    if phase == "post" and disp == "proceed":
        parsed["disposition"] = "replan"

    ms = int((time.time() - t0) * 1000)
    return parsed, ms, code_to_name


# 兼容旧名
run_analysis_pre = run_analyse


async def run_plan(
    *,
    analyse: dict[str, Any],
    user_message: str,
    rewritten: str,
    servers: list[str],
    retrieved_tool_names: list[str],
    code_to_name: dict[str, str],
    long_term_context: list[dict],
    long_term_json_max: int = 12000,
) -> tuple[dict[str, Any], int]:
    t0 = time.time()
    long_term_json = _truncate_json(long_term_context, long_term_json_max)
    analyse_json = json.dumps(analyse, ensure_ascii=False)[:8000]
    codes = analyse.get("candidate_tool_codes") or []
    names_ordered: list[str] = []
    seen: set[str] = set()
    for c in codes:
        c = str(c).strip()
        nm = code_to_name.get(c)
        if nm and nm not in seen:
            seen.add(nm)
            names_ordered.append(nm)
    if not names_ordered:
        names_ordered = list(retrieved_tool_names) if retrieved_tool_names else list(code_to_name.values())
    tools_detail = format_tools_full_detail(names_ordered, servers)
    prompt = get_prompts()["plan"].format(
        analyse_json=analyse_json,
        user_message=user_message.strip(),
        rewritten=(rewritten or user_message).strip(),
        servers=json.dumps(servers, ensure_ascii=False),
        long_term_json=long_term_json,
        tools_detail=tools_detail,
    )
    llm = _struct_llm()
    resp = await llm.ainvoke([HumanMessage(content=prompt)])
    record_llm_usage(resp, "plan")
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


async def run_output(
    *,
    disposition: str,
    analyse: dict[str, Any],
    plan: dict[str, Any] | None,
    tool_results: list[dict],
    user_message: str,
    long_term_context: list[dict],
    long_term_json_max: int = 12000,
    force_note: str = "",
) -> tuple[str, int]:
    """生成对用户可见的最终回复正文。"""
    t0 = time.time()
    plan_json = _truncate_json(plan or {}, 8000)
    long_term_json = _truncate_json(long_term_context, long_term_json_max)
    analyse_payload = dict(analyse)
    if force_note:
        analyse_payload["force_note"] = force_note
    prompt = get_prompts()["output"].format(
        disposition=disposition,
        analyse_json=_truncate_json(analyse_payload, 8000),
        plan_json=plan_json,
        tool_results_json=_truncate_json(tool_results, 14000),
        long_term_json=long_term_json,
        user_message=user_message.strip(),
    )
    llm = _struct_llm()
    resp = await ainvoke_with_system(llm, human_content=prompt)
    record_llm_usage(resp, "output")
    raw = str(getattr(resp, "content", None) or "")
    parsed = _safe_json_loads(raw)
    reply = ""
    if parsed:
        reply = str(parsed.get("final_reply") or "").strip()
    if not reply:
        reply = _strip_json_fence(raw).strip()
    if not reply:
        reply = (
            analyse.get("clarify_message")
            or analyse.get("cannot_reason")
            or analyse.get("analysis")
            or "抱歉，我暂时无法完成这个请求，请稍后再试。"
        )
    ms = int((time.time() - t0) * 1000)
    return reply, ms


def build_execute_messages(
    *,
    plan: dict[str, Any],
    analyse: dict[str, Any],
    long_term_context: list[dict],
    recent_messages: list[dict],
    user_msg: dict[str, str],
) -> list[dict]:
    """工具执行器上下文：不含对用户成稿指令。"""
    blocks = [get_prompts()["execute_system"]]
    analysis = str(analyse.get("analysis") or "").strip()
    if analysis:
        blocks.append(f"【分析】\n{analysis}")
    notes = str(analyse.get("notes_for_planner") or "").strip()
    if notes:
        blocks.append(f"【计划要点】\n{notes}")
    steps = plan.get("plan_steps") or []
    if steps:
        blocks.append("【计划步骤】")
        for i, s in enumerate(steps, 1):
            blocks.append(f"{i}. {s}")
    messages: list[dict] = []
    if long_term_context:
        messages.append(
            {
                "role": "system",
                "content": f"长期记忆(JSON): {_truncate_json(long_term_context, 8000)}",
            }
        )
    messages.append({"role": "system", "content": "\n".join(blocks)})
    messages.extend(recent_messages)
    messages.append(user_msg)
    return messages


def max_orchestration_cycles() -> int:
    n = int(load_config().get("orchestration", {}).get("max_orchestration_cycles", 3))
    return max(1, min(n, 6))
