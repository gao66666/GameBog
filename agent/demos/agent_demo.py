"""
LangChain Agent Demo
功能：LLM + 工具 + 记忆 + 多轮对话
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from datetime import datetime

from langchain_openai import ChatOpenAI
from langchain.agents import AgentExecutor, create_openai_tools_agent
from langchain.tools import tool
from langchain.memory import ConversationBufferMemory
from langchain.prompts import ChatPromptTemplate, MessagesPlaceholder

from infra.config import load_config


# ============================================================
# 工具定义
# ============================================================

@tool
def calculator(expression: str) -> str:
    """计算数学表达式。输入数学表达式字符串，返回计算结果。例: '2+3*4'"""
    try:
        return f"计算结果: {eval(expression)}"
    except Exception as e:
        return f"计算失败: {e}"


@tool
def get_current_time() -> str:
    """获取当前日期和时间"""
    now = datetime.now()
    return f"当前时间: {now.strftime('%Y-%m-%d %H:%M:%S')}（{now.strftime('%A')}）"


@tool
def word_length(word: str) -> str:
    """获取一个英文单词的长度和基本信息"""
    return f"单词 '{word}' 长度 {len(word)}，大写: {word.upper()}"


# ============================================================
# 构建 Agent
# ============================================================

def create_agent():
    cfg = load_config()

    llm = ChatOpenAI(
        model=cfg["model"],
        base_url=cfg["base_url"],
        api_key=cfg["api_key"],
        temperature=cfg["temperature"],
    )

    tools = [calculator, get_current_time, word_length]

    prompt = ChatPromptTemplate.from_messages([
        ("system", "你是一个有用的AI助手，可以使用工具回答问题。请用中文回答。"),
        MessagesPlaceholder(variable_name="chat_history", optional=True),
        ("human", "{input}"),
        MessagesPlaceholder(variable_name="agent_scratchpad"),
    ])

    memory = ConversationBufferMemory(
        memory_key="chat_history",
        return_messages=True,
    )

    agent = create_openai_tools_agent(llm, tools, prompt)

    agent_executor = AgentExecutor(
        agent=agent,
        tools=tools,
        memory=memory,
        verbose=True,
        handle_parsing_errors=True,
    )

    return agent_executor


# ============================================================
# 交互式对话
# ============================================================

def main():
    cfg = load_config()

    print("=" * 55)
    print("  🤖 LangChain Agent Demo")
    print(f"  模型: {cfg['model']}  |  Provider: {cfg['base_url']}")
    print("  工具: 计算器 | 当前时间 | 单词长度")
    print("  输入 quit / exit 退出")
    print("=" * 55)

    agent = create_agent()

    while True:
        try:
            user_input = input("\n🧑 你: ").strip()
            if not user_input:
                continue
            if user_input.lower() in ("quit", "exit", "q"):
                print("👋 再见！")
                break

            response = agent.invoke({"input": user_input})
            print(f"\n🤖 Agent: {response['output']}")

        except KeyboardInterrupt:
            print("\n👋 再见！")
            break
        except Exception as e:
            print(f"❌ 错误: {e}")


if __name__ == "__main__":
    main()
