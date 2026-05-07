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

from config import load_config
from mcp_public import PUBLIC_TOOLS
from mcp_user import USER_TOOLS


# ============================================================
# MCP Server 摘要（路由阶段发给 LLM，不含工具细节）
# ============================================================

SERVER_SUMMARIES = {
    "public": {
        "name": "blog-public（公开只读）",
        "description": "浏览博客内容，无需登录。包含：搜索文章、文章详情/评论/排行榜/最新文章、话题浏览与讨论、游戏库与点评、用户公开资料。",
    },
    "user": {
        "name": "blog-user（用户授权）",
        "description": "个人中心操作，需要登录。包含：我的资料、我的文章列表、我的收藏、我关注的话题、我的游戏记录、我的积分余额。",
    },
}

SERVER_TOOLS = {
    "public": PUBLIC_TOOLS,
    "user": USER_TOOLS,
}


# ============================================================
# 路由阶段 —— LLM 选出需要的 Server 组
# ============================================================

ROUTING_PROMPT = """有两个 MCP Server，判断用户问题需要哪个。

public 能做什么：{public_desc}

user 能做什么：{user_desc}

只输出 JSON：
{{"servers": ["public"]}}
{{"servers": ["user"]}}
{{"servers": ["public", "user"]}}
{{"servers": []}}"""


async def _route(user_message: str, has_token: bool) -> list[str]:
    """返回需要的 Server 组名列表。"""
    available = ["public"]
    if has_token:
        available.append("user")

    if not available:
        return []

    routing_msg = ROUTING_PROMPT.format(
        public_desc=SERVER_SUMMARIES["public"]["description"],
        user_desc=SERVER_SUMMARIES["user"]["description"] if has_token else "（未登录，不可用）",
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
# 执行阶段 —— 加载选中组的工具，流式执行
# ============================================================

SYSTEM_PROMPT = """你是 GoBlog 的智能助手「小博」，热情、简洁。

规则：
1. 始终用中文回复。
2. 善用工具获取实时数据，不要编造。需要登录的操作系统已自动注入身份。调用工具时遵守各工具声明的参数名与类型（见工具 schema）。
3. 无法处理的事诚实告知。
4. 何时结束工具调用、给出**最终回复**：当**当前已取得的信息**已经足以完整、准确回答用户的问题，或**当前执行结果**已经满足用户需求时，即可不再调用工具，直接输出最终回复；不要为了「多调几次工具」而继续调用。
5. 用户意图模糊、缺关键参数、或信息有歧义时，**追问澄清**，不要自行假设。
   - 例如用户说"那个游戏"但你不知道是哪个 → 追问
   - 例如用户说"看一下那篇文章"但没有给标题/ID → 追问
6. 用户问题与工具能力无关时（如纯闲聊），友好回应即可。
7. **长期记忆**：系统会把召回的长期记忆以 JSON 注入到上下文中供你参考；**没有**提供供你调用的「删除/编辑长期记忆」工具，这不是「没权限」，而是当前对话侧未实现该能力。长期记忆的合并、覆盖或删除由 `memory` 服务在**写入新记忆时**在后台自动决策。若用户要求删除自己的长期记忆，如实说明目前无法通过聊天直接删，可说明未来若上线记忆管理入口再处理。
8. **复杂任务（多阶段、多依赖、或需多次检索/对比）**：
   - 先在回复中**用简短编号列出步骤（计划）**，再按顺序执行，避免跳步或颠倒依赖。
   - 执行中若某步受阻（工具报错、数据缺失、条件不足等），说明原因后**尝试对该问题进行解决或者尝试可行的替代方案，视该问题的解决方案实践难度来决定**；若多次(3次以上)尝试仍无法推进，应取消计划明确告知用户。不要强行编造。
   - 若判定在当前信息与工具能力下**无法完成用户需求**，应**直接说明**原因、缺失条件或局限，并可提示用户如何补充信息（若适用）。"""


# ============================================================
# 执行阶段
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
        system_prompt=SYSTEM_PROMPT,
    )

    return agent
