"""OpenAI 兼容 Chat 客户端参数（DeepSeek / 智谱等）。"""
from __future__ import annotations

import os
from typing import Any

from langchain_core.language_models import BaseChatModel
from langchain_core.messages import BaseMessage, HumanMessage, SystemMessage

from infra.config import get_prompts


def resolve_api_key(cfg: dict[str, Any], *env_names: str) -> str:
    for name in env_names:
        val = (os.getenv(name) or "").strip()
        if val:
            return val
    return str(cfg.get("api_key") or "").strip()


def chat_openai_kwargs(cfg: dict[str, Any], *, temperature: float | None = None, **extra: Any) -> dict[str, Any]:
    """构建 ChatOpenAI 参数；thinking 仅对支持的厂商注入 extra_body。"""
    base = str(cfg.get("base_url") or "").lower()
    kw: dict[str, Any] = {
        "model": cfg["model"],
        "base_url": cfg["base_url"],
        "api_key": resolve_api_key(cfg, "LLM_API_KEY", "DEEPSEEK_API_KEY", "OPENAI_API_KEY"),
        "temperature": cfg.get("temperature", 0.7) if temperature is None else temperature,
    }
    kw.update(extra)
    thinking = bool(cfg.get("thinking"))
    zhipu = "bigmodel.cn" in base or "zhipu" in base
    deepseek = "deepseek" in base
    if thinking and (zhipu or deepseek):
        kw["extra_body"] = {"thinking": {"type": "enabled"}}
    elif zhipu and not thinking:
        kw["extra_body"] = {"thinking": {"type": "disabled"}}
    return kw


async def ainvoke_with_system(
    llm: BaseChatModel,
    *,
    human_content: str,
    system_content: str | None = None,
    system_key: str | None = "system",
) -> BaseMessage:
    """SystemMessage + HumanMessage 调用，与 memory_store 注入方式一致。"""
    system = (system_content or "").strip()
    if not system and system_key:
        raw = get_prompts().get(system_key)
        system = str(raw or "").strip()
    messages: list[BaseMessage] = []
    if system:
        messages.append(SystemMessage(content=system))
    messages.append(HumanMessage(content=human_content))
    return await llm.ainvoke(messages)
