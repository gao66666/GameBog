"""
RAG 模块 —— query 改写。使用 OpenAI 兼容接口，thinking 由 common.yaml 控制。
"""

from langchain_openai import ChatOpenAI
from langchain_core.messages import HumanMessage

from infra.agent_log import get_logger
from infra.config import get_prompts, load_config
from infra.llm_util import chat_openai_kwargs
from core.token_usage import record_llm_usage

_log = get_logger("rag")


async def rewrite_query(query: str) -> str:
    """将用户原始问题改写为更适合检索的表述。"""
    cfg = load_config()
    llm = ChatOpenAI(**chat_openai_kwargs(cfg, temperature=0))
    prompt = get_prompts()["rewrite"].format(query=query)
    response = await llm.ainvoke([HumanMessage(content=prompt)])
    record_llm_usage(response, "rewrite")
    rewritten = response.content.strip()
    if not rewritten or rewritten == query:
        return query
    return rewritten
