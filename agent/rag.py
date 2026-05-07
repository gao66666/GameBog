"""
RAG 模块 —— query 改写。使用 OpenAI 兼容接口，thinking 由 common.yaml 控制。
"""

from langchain_openai import ChatOpenAI
from langchain_core.messages import HumanMessage

from agent_log import get_logger
from config import load_config

_log = get_logger("rag")

REWRITE_PROMPT = """将用户问题改写为更适合检索的形式。重写规则：
- 补全省略和指代（如"那个"→具体名称）
- 使用标准术语
- 拆分复合问题为多个检索要点

原始问题：{query}

只输出改写后的问题，不要解释。"""


async def rewrite_query(query: str) -> str:
    """将用户原始问题改写为更适合检索的表述。"""
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
    prompt = REWRITE_PROMPT.format(query=query)
    response = await llm.ainvoke([HumanMessage(content=prompt)])
    rewritten = response.content.strip()
    if not rewritten or rewritten == query:
        return query
    return rewritten
