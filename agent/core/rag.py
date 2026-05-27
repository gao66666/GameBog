"""
RAG 模块 —— query 改写。使用 OpenAI 兼容接口，thinking 由 common.yaml 控制。
"""

import re

from langchain_openai import ChatOpenAI
from langchain_core.messages import HumanMessage

from infra.agent_log import get_logger
from infra.config import get_prompts, load_config
from infra.llm_util import chat_openai_kwargs
from core.token_usage import record_llm_usage

_log = get_logger("rag")

_INTENT_SPLIT_RE = re.compile(r"[；;]")


def parse_rewrite_intents(rewritten: str, *, max_intents: int = 3) -> list[str]:
    """
    从改写结果拆出独立 Dense 检索要点（与 prompts 中分号分隔约定一致）。
    单意图时返回整句一条；多意图时每段单独做向量检索后再合并。
    """
    text = (rewritten or "").strip()
    if not text:
        return []
    parts = [p.strip() for p in _INTENT_SPLIT_RE.split(text) if p.strip()]
    intents: list[str] = []
    for p in parts:
        p = re.sub(r"^要点\s*[一二三四五六七八九十\d]+[：:]\s*", "", p).strip()
        if p:
            intents.append(p)
    if not intents:
        return [text]
    if len(intents) == 1:
        return [intents[0]]
    cap = max(1, int(max_intents))
    return intents[:cap]


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
