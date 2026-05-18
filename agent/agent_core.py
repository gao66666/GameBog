"""
Agent 核心 —— 两阶段 Agent：
  1. 路由阶段：只给 LLM 两组 Server 的摘要，选出需要的组
  2. 执行阶段：加载选中组的详细工具，流式执行
"""
import json
from datetime import datetime

from langchain_openai import ChatOpenAI
from langchain.agents import create_agent
from langchain.tools import tool
from langchain_core.messages import HumanMessage

from config import get_prompts, load_config
from mcp_public import PUBLIC_TOOLS
from mcp_user import USER_TOOLS


# ============================================================
# MCP Server 摘要：见 prompts.yaml（热更新）
# ============================================================

SERVER_TOOLS = {
    "public": PUBLIC_TOOLS,
    "user": USER_TOOLS,
}


def build_tool_short_catalog(
    servers: list[str],
    *,
    max_total_chars: int = 12000,
) -> tuple[str, dict[str, str]]:
    """按稳定顺序为已选组内工具生成「识别码 → 工具名 + 短说明」表。返回 (catalog 文本, 识别码→工具名)。"""
    lines: list[str] = []
    code_to_name: dict[str, str] = {}
    n = 0
    for group in servers:
        tools = SERVER_TOOLS.get(group, [])
        if not tools:
            continue
        lines.append(f"**组 `{group}`**")
        for t in tools:
            n += 1
            code = f"T{n:02d}"
            name = getattr(t, "name", None) or "unknown"
            code_to_name[code] = name
            desc = (getattr(t, "description", None) or "").strip().replace("\n", " ")
            if len(desc) > 120:
                desc = desc[:117] + "..."
            lines.append(f"- **{code}** → 工具名 `{name}` — {desc}")
        lines.append("")
    text = "\n".join(lines).strip()
    if len(text) > max_total_chars:
        text = text[: max_total_chars - 40] + "\n…（工具表已截断，仍以识别码为准）"
    return text or "（当前无可用工具）", code_to_name


def format_tools_full_detail(
    tool_names: list[str],
    servers: list[str],
    *,
    max_chars: int = 28000,
) -> str:
    """按计划阶段给定工具名列表，拼接各工具的完整 description（供计划 LLM）。"""
    by_name: dict[str, object] = {}
    for group in servers:
        for t in SERVER_TOOLS.get(group, []):
            nm = getattr(t, "name", None)
            if nm and nm not in by_name:
                by_name[nm] = t
    chunks: list[str] = []
    for nm in tool_names:
        t = by_name.get(nm)
        if not t:
            continue
        desc = (getattr(t, "description", None) or "").strip()
        chunks.append(f"### `{nm}`\n{desc}\n")
    out = "\n".join(chunks).strip()
    if len(out) > max_chars:
        return out[: max_chars - 24] + "\n…（工具完整说明已截断）"
    return out or "（无候选工具详情；请按第一轮分析在能力边界内做计划）"


# ============================================================
# 路由阶段 —— LLM 选出需要的 Server 组
# ============================================================


async def _route(user_message: str, has_token: bool) -> list[str]:
    """返回需要的 Server 组名列表。"""
    available = ["public"]
    if has_token:
        available.append("user")

    if not available:
        return []

    prompts = get_prompts()
    summaries = prompts["server_summaries"]
    routing_msg = prompts["routing"].format(
        public_desc=summaries["public"]["description"],
        user_desc=summaries["user"]["description"] if has_token else "（未登录，不可用）",
    )

    cfg = load_config()
    llm_kwargs = dict(
        model=cfg["model"],
        base_url=cfg["base_url"],
        api_key=cfg["api_key"],
        temperature=0,
    )
    if cfg.get("thinking"):
        llm_kwargs["extra_body"] = {"thinking": {"type": "enabled"}}
    else:
        llm_kwargs["extra_body"] = {"thinking": {"type": "disabled"}}
    llm = ChatOpenAI(**llm_kwargs)

    response = await llm.ainvoke([
        HumanMessage(content=routing_msg),
        HumanMessage(content=user_message),
    ])

    try:
        text = response.content.strip()
        if text.startswith("```"):
            text = text.split("\n", 1)[1]
            if text.endswith("```"):
                text = text[:-3]
        result = json.loads(text)
        servers = [s for s in result.get("servers", []) if s in available]
        return servers
    except json.JSONDecodeError:
        return ["public"]


# ============================================================
# 执行阶段 —— 加载选中组的工具，流式执行（system 提示见 prompts.yaml）
# ============================================================


@tool
def get_current_time() -> str:
    """获取当前日期和时间。"""
    return datetime.now().strftime("%Y-%m-%d %H:%M:%S")


def build_agent(servers: list[str]):
    """按 server 组名加载对应工具，构建 Agent。servers 如 ['public'] 或 ['public', 'user']。"""
    tools = [get_current_time]
    for s in servers:
        tools.extend(SERVER_TOOLS.get(s, []))

    cfg = load_config()
    llm_kwargs = dict(
        model=cfg["model"],
        base_url=cfg["base_url"],
        api_key=cfg["api_key"],
        temperature=cfg["temperature"],
        streaming=True,
    )
    if cfg.get("thinking"):
        llm_kwargs["extra_body"] = {"thinking": {"type": "enabled"}}
    else:
        llm_kwargs["extra_body"] = {"thinking": {"type": "disabled"}}
    llm = ChatOpenAI(**llm_kwargs)
    
    agent = create_agent(
        model=llm,
        tools=tools,
        system_prompt=get_prompts()["system"],
    )

    return agent
