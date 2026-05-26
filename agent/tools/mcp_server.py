"""
GoBlog MCP Server —— 工具定义与调用由 shared/tools_registry.json 驱动。

启动: python -m tools.mcp_server   （在 agent 目录下）
"""
import asyncio
import json
import sys
from pathlib import Path

if __name__ == "__main__" and __package__ is None:
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from mcp.server import Server
from mcp.server.stdio import stdio_server
from mcp.types import TextContent, Tool

from tools.tool_runtime import get_tool_spec, load_registry, mcp_call_tool

server = Server("goblog-mcp")


@server.list_tools()
async def handle_list_tools() -> list[Tool]:
    tools: list[Tool] = []
    for name, spec in load_registry().get("tools", {}).items():
        tools.append(
            Tool(
                name=name,
                description=(spec.get("description") or spec.get("label") or name).strip(),
                inputSchema=spec.get("input_schema") or {"type": "object", "properties": {}},
            )
        )
    return tools


def _token_from(arguments: dict) -> str:
    return str(arguments.get("token") or "")


@server.call_tool()
async def handle_call_tool(name: str, arguments: dict) -> list[TextContent]:
    if not get_tool_spec(name):
        payload = json.dumps({"_error": f"未知工具: {name}"}, ensure_ascii=False)
        return [TextContent(type="text", text=payload)]
    try:
        token = _token_from(arguments)
        args = {k: v for k, v in arguments.items() if k != "token"}
        text = mcp_call_tool(name, args, token=token)
        return [TextContent(type="text", text=text)]
    except Exception as e:
        payload = json.dumps({"_error": f"工具执行失败: {e}"}, ensure_ascii=False)
        return [TextContent(type="text", text=payload)]


async def main():
    async with stdio_server() as (read_stream, write_stream):
        await server.run(read_stream, write_stream, server.create_initialization_options())


if __name__ == "__main__":
    asyncio.run(main())
