"""通用工具组 — Agent 端本地执行，由 shared/tools_registry.json 加载。"""
from tools.tool_runtime import tools_for_group

GENERAL_TOOLS = tools_for_group("general")
