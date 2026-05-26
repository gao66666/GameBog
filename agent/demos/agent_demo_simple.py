"""
LangChain Agent Demo —— 最简版（单次调用，无交互循环）
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from datetime import datetime

from langchain_openai import ChatOpenAI
from langchain.agents import create_agent
from langchain.tools import tool

from infra.config import load_config


@tool
def calculator(expression: str) -> str:
    """计算数学表达式，如 '2+3*4'"""
    try:
        return f"结果: {eval(expression)}"
    except Exception as e:
        return f"错误: {e}"


@tool
def get_current_time() -> str:
    """获取当前时间"""
    return datetime.now().strftime('%Y-%m-%d %H:%M:%S')


def main():
    cfg = load_config()

    llm = ChatOpenAI(
        model=cfg["model"],
        base_url=cfg["base_url"],
        api_key=cfg["api_key"],
        temperature=cfg["temperature"],
    )

    tools = [calculator, get_current_time]

    agent = create_agent(
        model=llm,
        tools=tools,
        system_prompt="你是一个有用的AI助手，用中文回答。",
    )

    questions = [
        "现在是几点？",
        "帮我算一下 123 * 456 + 789",
    ]

    for q in questions:
        print(f"\n{'='*50}")
        print(f"问: {q}")
        result = agent.invoke({"messages": [{"role": "user", "content": q}]})
        print(f"答: {result['emessages'][-1].content}")


if __name__ == "__main__":
    main()
