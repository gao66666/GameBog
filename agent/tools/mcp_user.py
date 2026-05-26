"""用户授权 MCP 工具 — 由 shared/tools_registry.json 加载。"""
from tools.tool_runtime import tools_for_group

USER_TOOLS = tools_for_group("user")
