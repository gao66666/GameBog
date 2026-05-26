#!/usr/bin/env python3

"""

校验 shared/tools_registry.json 内部一致性及与 BlogClient / Agent 端的对应关系。



用法（仓库根目录）:

  python agent/scripts/validate_tools_registry.py

"""



from __future__ import annotations



import inspect

import json

import re

import sys

from pathlib import Path

from typing import Any



ROOT = Path(__file__).resolve().parents[2]

REGISTRY_PATH = ROOT / "shared" / "tools_registry.json"

AGENT_DIR = ROOT / "agent"



# 与 agent/tool_runtime._apply_response_preset 保持同步

VALID_RESPONSE_PRESETS = frozenset(

    {

        "article_search",

        "article_detail",

        "article_leaderboard",

        "comments_list",

        "topics_list",

        "topic_detail",

        "topic_articles",

        "discussions_list",

        "games_list",
        "games_search",

        "game_detail",

        "points_mall_products",
        "points_mall_search",
        "points_mall_product",

        "game_reviews",

        "user_public",

        "me_profile",

        "me_collections",

        "me_followed_topics",

        "me_game_plays",

        "me_wallet",

        "passthrough",

    }

)



VALID_GROUPS = frozenset({"public", "user", "general"})

VALID_PROVIDERS = frozenset({"blog", "agent"})

VALID_VALIDATOR_RULES = frozenset(
    {
        "non_empty_str",
        "snowflake_id",
        "positive_int",
        "page",
        "page_size",
        "enum_view_like",
        "search_result_limit",
    }
)

TEMPLATE_VAR = re.compile(r"\{\{(\w+)\}\}")

PLACEHOLDER = re.compile(r"^\{\{(\w+)\}\}$")

# 模板允许的全局占位符（非 MCP 参数）

TEMPLATE_GLOBALS = frozenset({"label", "row_count"})





def _load_registry() -> dict[str, Any]:

    with open(REGISTRY_PATH, encoding="utf-8") as f:

        return json.load(f)





def _blog_client_methods() -> set[str]:

    sys.path.insert(0, str(AGENT_DIR))

    try:

        from tools.blog_client import BlogClient  # noqa: WPS433

    finally:

        if str(AGENT_DIR) in sys.path:

            sys.path.remove(str(AGENT_DIR))



    return {

        name

        for name, member in inspect.getmembers(BlogClient, predicate=inspect.isfunction)

        if not name.startswith("_")

    }





def _agent_client_methods() -> set[str]:

    sys.path.insert(0, str(AGENT_DIR))

    try:

        from tools import general_client  # noqa: WPS433

    finally:

        if str(AGENT_DIR) in sys.path:

            sys.path.remove(str(AGENT_DIR))



    return {

        name

        for name, member in inspect.getmembers(general_client, predicate=inspect.isfunction)

        if not name.startswith("_")

    }





def _schema_keys(spec: dict[str, Any]) -> set[str]:

    schema = spec.get("input_schema") or {}

    props = schema.get("properties") or {}

    return set(props.keys())





def _check_templates(tool_name: str, spec: dict[str, Any], arg_keys: set[str], errors: list[str]) -> None:

    allowed = TEMPLATE_GLOBALS | arg_keys

    for field in ("start_template", "end_ok_template", "end_fail_template"):

        tmpl = spec.get(field) or ""

        if not tmpl:

            continue

        for var in TEMPLATE_VAR.findall(tmpl):

            if var not in allowed:

                errors.append(

                    f"{tool_name}: {field} 使用了 {{{{{var}}}}}，不在允许集合 {sorted(allowed)} 中"

                )





def _check_call_kwargs(tool_name: str, spec: dict[str, Any], schema_keys: set[str], errors: list[str]) -> None:

    call = spec.get("call") or {}

    defaults = set((call.get("defaults") or {}).keys())

    allowed = schema_keys | defaults

    for param, tmpl in (call.get("kwargs") or {}).items():

        if not isinstance(tmpl, str):

            errors.append(f"{tool_name}: call.kwargs[{param!r}] 必须是字符串")

            continue

        m = PLACEHOLDER.match(tmpl.strip())

        if not m:

            continue

        var = m.group(1)

        if var not in allowed:

            errors.append(

                f"{tool_name}: call.kwargs[{param!r}] 引用 {{{{{var}}}}}，"

                f"不在 input_schema 或 defaults（允许: {sorted(allowed)}）"

            )





def validate_registry(

    reg: dict[str, Any],

    client_methods: set[str],

    agent_methods: set[str],

) -> list[str]:

    errors: list[str] = []

    tools = reg.get("tools")

    if not isinstance(tools, dict) or not tools:

        errors.append("tools 必须为非空对象")

        return errors



    for name, spec in tools.items():

        if not isinstance(spec, dict):

            errors.append(f"{name}: 工具定义必须是对象")

            continue



        group = spec.get("group")

        if group not in VALID_GROUPS:

            errors.append(f"{name}: group 必须是 {sorted(VALID_GROUPS)} 之一，当前为 {group!r}")



        if not str(spec.get("label") or "").strip():

            errors.append(f"{name}: 缺少 label")

        if not str(spec.get("description") or "").strip():

            errors.append(f"{name}: 缺少 description")



        schema_keys = _schema_keys(spec)

        arg_keys_list = spec.get("arg_keys") or []

        if not isinstance(arg_keys_list, list):

            errors.append(f"{name}: arg_keys 必须是数组")

            continue

        arg_keys = set(arg_keys_list)

        extra = arg_keys - schema_keys

        if extra:

            errors.append(f"{name}: arg_keys {sorted(extra)} 不在 input_schema.properties {sorted(schema_keys)} 中")



        preset = spec.get("response_preset")

        if not preset:

            errors.append(f"{name}: 缺少 response_preset")

        elif preset not in VALID_RESPONSE_PRESETS:

            errors.append(f"{name}: 未知 response_preset {preset!r}")



        call = spec.get("call") or {}

        provider = call.get("provider", "blog")

        if provider not in VALID_PROVIDERS:

            errors.append(f"{name}: call.provider 必须是 {sorted(VALID_PROVIDERS)} 之一")

        method = call.get("method")
        if not method:

            errors.append(f"{name}: call.method 不能为空")

        elif provider == "agent":

            if method not in agent_methods:

                errors.append(f"{name}: general_client 不存在方法 {method!r}")

            if spec.get("auth"):

                errors.append(f"{name}: agent 端工具不应 auth=true")

        elif method not in client_methods:

            errors.append(f"{name}: BlogClient 不存在方法 {method!r}")



        if spec.get("auth") and not call.get("pass_token"):

            errors.append(f"{name}: auth=true 时 call.pass_token 应为 true")



        _check_templates(name, spec, arg_keys, errors)

        _check_call_kwargs(name, spec, schema_keys, errors)

        _check_validators(name, spec, schema_keys, errors)

    return errors


def _check_validators(
    tool_name: str, spec: dict[str, Any], schema_keys: set[str], errors: list[str]
) -> None:
    for item in spec.get("validators") or []:
        if not isinstance(item, dict):
            errors.append(f"{tool_name}: validators 项必须是对象")
            continue
        field = item.get("field")
        rule = item.get("rule")
        if not field or not rule:
            errors.append(f"{tool_name}: validators 项缺少 field 或 rule")
            continue
        if field not in schema_keys:
            errors.append(f"{tool_name}: validators.field {field!r} 不在 input_schema 中")
        if rule not in VALID_VALIDATOR_RULES:
            errors.append(
                f"{tool_name}: validators.rule {rule!r} 未知（允许: {sorted(VALID_VALIDATOR_RULES)}）"
            )
    props = (spec.get("input_schema") or {}).get("properties") or {}
    if isinstance(props, dict):
        for key, meta in props.items():
            if not isinstance(meta, dict):
                continue
            contract = meta.get("x-contract")
            if contract and contract not in VALID_VALIDATOR_RULES:
                errors.append(f"{tool_name}: x-contract {contract!r} 未知")





def main() -> int:

    if not REGISTRY_PATH.is_file():

        print(f"找不到 {REGISTRY_PATH}", file=sys.stderr)

        return 1



    reg = _load_registry()

    client_methods = _blog_client_methods()

    agent_methods = _agent_client_methods()

    errors = validate_registry(reg, client_methods, agent_methods)



    if errors:

        print(f"tools_registry 校验失败（{len(errors)} 项）:", file=sys.stderr)

        for e in errors:

            print(f"  - {e}", file=sys.stderr)

        return 1



    print(f"tools_registry OK（{len(reg.get('tools', {}))} 个工具）")

    return 0





if __name__ == "__main__":

    sys.exit(main())

