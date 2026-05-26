"""
Agent 核心 —— 路由选组 → 检索候选 → 分析选工具 → 执行阶段按需加载工具。
"""
import json
from dataclasses import dataclass
from typing import Any, AsyncIterator

from langchain_openai import ChatOpenAI
from langchain.agents import create_agent
from infra.config import get_prompts, load_config
from infra.llm_util import ainvoke_with_system, chat_openai_kwargs
from tools.mcp_general import GENERAL_TOOLS
from tools.mcp_public import PUBLIC_TOOLS
from tools.mcp_user import USER_TOOLS
from tools.tool_runtime import get_tool_spec, load_registry
from tools.param_contracts import CONTRACT_RULES
from core.stream_emitter import coerce_tool_output_text
from core.token_usage import record_llm_usage
from memory.memory_manager import sort_memory_hints_for_retrieval


SERVER_TOOLS = {
    "public": PUBLIC_TOOLS,
    "user": USER_TOOLS,
    "general": GENERAL_TOOLS,
}


def _tools_by_name(servers: list[str]) -> dict[str, object]:
    by_name: dict[str, object] = {}
    for group in servers:
        for t in SERVER_TOOLS.get(group, []):
            nm = getattr(t, "name", None)
            if nm and nm not in by_name:
                by_name[nm] = t
    return by_name


def build_tool_short_catalog(
    servers: list[str],
    *,
    tool_names: list[str] | None = None,
    max_total_chars: int = 12000,
) -> tuple[str, dict[str, str]]:
    """为已选组内工具（或 tool_names 子集）生成「识别码 → 工具名 + 短说明」表。"""
    allowed: set[str] | None = set(tool_names) if tool_names is not None else None
    lines: list[str] = []
    code_to_name: dict[str, str] = {}
    n = 0
    for group in servers:
        tools = SERVER_TOOLS.get(group, [])
        if not tools:
            continue
        group_has = False
        group_lines: list[str] = []
        for t in tools:
            name = getattr(t, "name", None) or "unknown"
            if allowed is not None and name not in allowed:
                continue
            n += 1
            code = f"T{n:02d}"
            code_to_name[code] = name
            spec = get_tool_spec(name) or {}
            label = (spec.get("label") or name).strip()
            desc = (getattr(t, "description", None) or spec.get("description") or "").strip().replace("\n", " ")
            if len(desc) > 120:
                desc = desc[:117] + "..."
            group_lines.append(f"- **{code}** → `{name}`（{label}）— {desc}")
            group_has = True
        if group_has:
            lines.append(f"**组 `{group}`**")
            lines.extend(group_lines)
            lines.append("")
    text = "\n".join(lines).strip()
    if len(text) > max_total_chars:
        text = text[: max_total_chars - 40] + "\n…（工具表已截断，仍以识别码为准）"
    return text or "（当前无可用工具）", code_to_name


def _format_schema_summary(schema: dict[str, Any]) -> str:
    props = schema.get("properties") or {}
    if not isinstance(props, dict) or not props:
        return "  （无参数）"
    required = set(schema.get("required") or [])
    lines: list[str] = []
    for key, meta in props.items():
        if not isinstance(meta, dict):
            continue
        req = "必填" if key in required else "可选"
        typ = meta.get("type", "any")
        desc = (meta.get("description") or "").strip()
        contract = meta.get("x-contract")
        contract_hint = ""
        if isinstance(contract, str) and contract in CONTRACT_RULES:
            contract_hint = f" [契约: {CONTRACT_RULES[contract]}]"
        lines.append(
            f"  - `{key}` ({typ}, {req}){(': ' + desc) if desc else ''}{contract_hint}"
        )
    return "\n".join(lines) if lines else "  （无参数）"


def format_global_parameter_rules() -> str:
    rules = load_registry().get("global_parameter_rules") or []
    if not rules:
        return ""
    body = "\n".join(f"- {r}" for r in rules if str(r).strip())
    return f"## 全局参数规范（必须遵守）\n{body}\n"


def format_tools_full_detail(
    tool_names: list[str],
    servers: list[str],
    *,
    max_chars: int = 28000,
) -> str:
    """按计划阶段给定工具名列表，拼接完整说明（description + 参数 schema + 响应预设）。"""
    by_name = _tools_by_name(servers)
    chunks: list[str] = []
    for nm in tool_names:
        t = by_name.get(nm)
        spec = get_tool_spec(nm) or {}
        if not t and not spec:
            continue
        label = (spec.get("label") or nm).strip()
        desc = (getattr(t, "description", None) or spec.get("description") or "").strip()
        schema = spec.get("input_schema") or {"type": "object", "properties": {}}
        preset = spec.get("response_preset") or "passthrough"
        wrap = spec.get("response_wrap")
        io_hint = f"响应预设: `{preset}`"
        if wrap:
            io_hint += f"，结果包装键 `{wrap}`"
        guide_lines = spec.get("parameter_guide") or []
        guide_block = ""
        if isinstance(guide_lines, list) and guide_lines:
            guide_block = "**调用约束**：\n" + "\n".join(
                f"- {g}" for g in guide_lines if str(g).strip()
            ) + "\n\n"
        chunks.append(
            f"### `{nm}`（{label}）\n"
            f"**功能**：{desc}\n\n"
            f"{guide_block}"
            f"**参数**：\n{_format_schema_summary(schema)}\n\n"
            f"**输出**：{io_hint}\n"
        )
    header = format_global_parameter_rules()
    out = "\n".join(chunks).strip()
    if header:
        out = header + "\n" + out
    if len(out) > max_chars:
        return out[: max_chars - 24] + "\n…（工具完整说明已截断）"
    return out or "（无候选工具详情；请按第一轮分析在能力边界内做计划）"


def resolve_tools_for_execution(
    servers: list[str],
    *,
    analyse: dict[str, Any] | None = None,
    analysis_pre: dict[str, Any] | None = None,
    plan: dict[str, Any] | None = None,
    retrieved_tool_names: list[str] | None = None,
    code_to_name: dict[str, str] | None = None,
) -> list[str] | None:
    """
    解析执行阶段应绑定的工具名。
    返回 None 表示加载 servers 整组；返回 [] 表示不加载组内 MCP 工具（若 servers 含 general 仍加载通用组）。
    """
    orch = load_config().get("orchestration", {})
    if not orch.get("dynamic_tool_injection", True):
        return None

    mapping = code_to_name or {}
    if not mapping and retrieved_tool_names:
        _, mapping = build_tool_short_catalog(servers, tool_names=retrieved_tool_names)

    src = analyse or analysis_pre or {}
    codes = src.get("candidate_tool_codes") or []
    names: list[str] = []
    seen: set[str] = set()
    for c in codes:
        nm = mapping.get(str(c).strip())
        if nm and nm not in seen:
            seen.add(nm)
            names.append(nm)

    requires_tools = (plan or {}).get("requires_tools", True) if plan is not None else True
    if names:
        return names
    if plan is not None and not requires_tools:
        return []
    if retrieved_tool_names:
        return list(retrieved_tool_names)
    return None


@dataclass
class RouteResult:
    servers: list[str]
    tool_hints: list[str]
    memory_hints: list[str]


def _normalize_route_hints(
    raw: Any,
    *,
    max_items: int = 8,
    max_chars: int = 64,
) -> list[str]:
    if not isinstance(raw, list):
        return []
    out: list[str] = []
    seen: set[str] = set()
    max_items = max(1, int(max_items))
    max_chars = max(8, int(max_chars))
    for item in raw:
        if not isinstance(item, str):
            continue
        s = item.strip()
        if not s or len(s) > max_chars:
            continue
        key = s.lower()
        if key in seen:
            continue
        seen.add(key)
        out.append(s)
        if len(out) >= max_items:
            break
    return out


async def _route(
    user_message: str,
    has_token: bool,
    *,
    rewritten: str | None = None,
) -> RouteResult:
    """返回需要的 Server 组名与工具检索关键词。优先参考检索改写句。"""
    available = ["public", "general"]
    if has_token:
        available.append("user")

    if not available:
        return RouteResult([], [], [])

    prompts = get_prompts()
    summaries = prompts["server_summaries"]
    routing_msg = prompts["routing"].format(
        public_desc=summaries["public"]["description"],
        user_desc=summaries["user"]["description"] if has_token else "（未登录，不可用）",
        general_desc=summaries["general"]["description"],
    )

    user_parts = [f"用户原话：{user_message.strip()}"]
    rw = (rewritten or "").strip()
    if rw and rw != user_message.strip():
        user_parts.append(f"检索改写：{rw}")

    cfg = load_config()
    llm = ChatOpenAI(**chat_openai_kwargs(cfg, temperature=0))

    human = routing_msg.strip() + "\n\n" + "\n".join(user_parts)
    response = await ainvoke_with_system(llm, human_content=human)
    record_llm_usage(response, "route")

    try:
        text = response.content.strip()
        if text.startswith("```"):
            text = text.split("\n", 1)[1]
            if text.endswith("```"):
                text = text[:-3]
        result = json.loads(text)
        servers = [s for s in result.get("servers", []) if s in available]
        mem_cfg = cfg.get("memory", {})
        tool_hints = _normalize_route_hints(result.get("tool_hints"))
        memory_hints = _normalize_route_hints(
            result.get("memory_hints"),
            max_items=int(mem_cfg.get("memory_hints_max_items", 8)),
            max_chars=int(mem_cfg.get("memory_hints_max_chars", 64)),
        )
        memory_hints = sort_memory_hints_for_retrieval(memory_hints)
        if not has_token:
            user_only = ("收藏", "积分", "我的", "个人", "关注", "钱包", "资料")
            tool_hints = [h for h in tool_hints if not any(k in h for k in user_only)]
            memory_hints = [h for h in memory_hints if not any(k in h for k in user_only)]
        return RouteResult(servers, tool_hints, memory_hints)
    except json.JSONDecodeError:
        return RouteResult(["public"], [], [])


def build_agent(
    servers: list[str],
    tool_names: list[str] | None = None,
    *,
    execute_mode: bool = False,
):
    """构建 Agent。execute_mode 时使用 execute_system，仅用于工具收集。"""
    tools: list[Any] = []
    by_name = _tools_by_name(servers)

    if tool_names is None:
        for s in servers:
            tools.extend(SERVER_TOOLS.get(s, []))
    elif tool_names:
        for nm in tool_names:
            t = by_name.get(nm)
            if t is not None:
                tools.append(t)
    elif "general" in servers:
        tools.extend(SERVER_TOOLS.get("general", []))

    cfg = load_config()
    llm = ChatOpenAI(**chat_openai_kwargs(cfg, streaming=True))

    system = get_prompts()["execute_system"] if execute_mode else get_prompts()["system"]
    agent = create_agent(
        model=llm,
        tools=tools,
        system_prompt=system,
    )

    return agent


async def stream_tool_execution(
    agent: Any,
    messages: list[dict],
    agent_config: dict[str, Any],
    em: Any,
) -> AsyncIterator[tuple[str, Any]]:
    """yield ('line', sse_str) 或 ('result', tool_results)。"""
    tool_results: list[dict[str, Any]] = []
    pending_tool: dict[str, Any] = {"name": "unknown", "args": {}}

    async for event in agent.astream_events(
        {"messages": messages},
        config=agent_config,
        version="v2",
    ):
        kind = event.get("event", "")
        if kind == "on_chat_model_stream":
            continue
        if kind == "on_chat_model_end":
            out = (event.get("data") or {}).get("output")
            if out is not None:
                record_llm_usage(out, "execute")
            continue
        if kind == "on_tool_start":
            tool_name = event.get("name", "unknown")
            raw_input = event.get("data", {}).get("input") or {}
            if not isinstance(raw_input, dict):
                raw_input = {}
            pending_tool = {"name": tool_name, "args": raw_input}
            for line in em.emit_tool_start(tool_name, raw_input):
                yield ("line", line)
        elif kind == "on_tool_end":
            tool_name = event.get("name") or pending_tool.get("name", "unknown")
            raw_input = event.get("data", {}).get("input") or pending_tool.get("args") or {}
            if not isinstance(raw_input, dict):
                raw_input = pending_tool.get("args") or {}
            raw_out = event.get("data", {}).get("output", "")
            output = coerce_tool_output_text(raw_out)
            tool_results.append(
                {
                    "tool": tool_name,
                    "args": raw_input,
                    "output": output[:8000],
                    "ok": not any(x in output for x in ("失败", "未登录", "错误", "未知工具")),
                }
            )
            for line in em.emit_tool_end(tool_name, raw_input, output):
                yield ("line", line)

    yield ("result", tool_results)
