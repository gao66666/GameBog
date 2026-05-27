"""
从 shared/tools_registry.json 加载工具定义，统一执行 HTTP 调用与响应抽取。
供 LangChain Agent（mcp_public / mcp_user）与 MCP stdio Server（mcp_server）共用。
"""

from __future__ import annotations

import json
import re
from functools import lru_cache
from pathlib import Path
from typing import Annotated, Any, Callable

from langchain_core.tools import InjectedToolArg, StructuredTool
from langchain_core.runnables import RunnableConfig
from pydantic import BaseModel, Field, create_model

from tools.blog_client import get_client
from tools.general_client import get_current_time as _agent_get_current_time
from tools.general_client import calculate as _agent_calculate
from tools.general_client import text_stats as _agent_text_stats
from tools.param_contracts import contracts_for_schema, parse_positive_int, validate_contract

_REGISTRY_PATH = Path(__file__).resolve().parents[2] / "shared" / "tools_registry.json"
_PLACEHOLDER = re.compile(r"^\{\{(\w+)\}\}$")

_AGENT_METHODS: dict[str, Callable[..., dict]] = {
    "get_current_time": _agent_get_current_time,
    "calculate": _agent_calculate,
    "text_stats": _agent_text_stats,
}


def _registry_path() -> Path:
    return _REGISTRY_PATH


@lru_cache(maxsize=1)
def load_registry() -> dict[str, Any]:
    with open(_registry_path(), encoding="utf-8") as f:
        return json.load(f)


def get_tool_spec(name: str) -> dict[str, Any] | None:
    return load_registry().get("tools", {}).get(name)


def list_tool_names(*groups: str) -> list[str]:
    tools = load_registry().get("tools", {})
    if not groups:
        return list(tools.keys())
    out: list[str] = []
    for name, spec in tools.items():
        if spec.get("group") in groups:
            out.append(name)
    return out


def summarize_args(tool_name: str, arguments: dict[str, Any]) -> dict[str, Any]:
    """按 registry arg_keys 从工具参数构建 args_summary（键名与 MCP 参数一致）。"""
    spec = get_tool_spec(tool_name)
    if not spec:
        return {}
    keys = spec.get("arg_keys") or []
    out: dict[str, Any] = {}
    for k in keys:
        if k in arguments and arguments[k] is not None and str(arguments[k]).strip() != "":
            out[k] = arguments[k]
    return out


def _ok(data: Any) -> str:
    return json.dumps(data, ensure_ascii=False, default=str)


def _pick(obj: dict, *keys: str, **aliases: Any) -> dict:
    if not isinstance(obj, dict):
        return {}
    out: dict[str, Any] = {}
    for k in keys:
        if k in obj:
            out[k] = obj[k]
            continue
        camel = "".join(w if i == 0 else w.capitalize() for i, w in enumerate(k.split("_")))
        if camel in obj:
            out[k] = obj[camel]
    for out_key, src_keys in aliases.items():
        if out_key in out:
            continue
        if isinstance(src_keys, str):
            src_keys = [src_keys]
        for sk in src_keys:
            if sk in obj:
                out[out_key] = obj[sk]
                break
    return out


def _first_list(data: dict, *paths: str) -> list:
    for p in paths:
        items = data.get(p)
        if isinstance(items, list):
            return items
    return []


def _truncate_fields(obj: dict, limits: dict[str, int]) -> dict:
    out = dict(obj)
    for field, limit in limits.items():
        val = out.get(field)
        if isinstance(val, str) and len(val) > limit:
            out[field] = val[:limit]
    return out


def _format_price_cents(cents: Any) -> str:
    """与 models.Game.PriceCents 语义一致：<=0 或空为免费，>0 为分。"""
    if cents is None or cents == "":
        return "免费"
    try:
        c = int(cents)
    except (TypeError, ValueError):
        return "免费"
    if c <= 0:
        return "免费"
    yuan = c / 100.0
    if c % 100 == 0:
        return f"¥{int(yuan)}"
    return f"¥{yuan:.2f}"


def _shape_mall_product(product: dict[str, Any]) -> dict[str, Any]:
    row = _pick(
        product,
        "id",
        "name",
        "subtitle",
        "cover_url",
        status=["status"],
    )
    row["description"] = (product.get("description") or "")[:200]
    try:
        row["price_points"] = int(product.get("price_points", product.get("pricePoints")) or 0)
    except (TypeError, ValueError):
        row["price_points"] = 0
    try:
        row["stock"] = int(product.get("stock", 0))
    except (TypeError, ValueError):
        row["stock"] = 0
    row["on_sale"] = str(product.get("status") or "").strip() == "on_sale"
    return row


def _shape_game_row(game: dict, *, include_description: bool = True) -> dict:
    row = _pick(
        game,
        "id",
        "name",
        "publisher",
        "developer",
        "release_at",
        "cover_url",
        tags=["tags"],
    )
    if include_description:
        row["description"] = (game.get("description") or "")[:300]
    raw_cents = game.get("price_cents", game.get("priceCents"))
    try:
        retail_cents = int(raw_cents) if raw_cents is not None else -1
    except (TypeError, ValueError):
        retail_cents = -1
    row["retail_price_cents"] = retail_cents
    row["retail_price_display"] = _format_price_cents(retail_cents)
    return row


def _resolve_call_kwargs(spec: dict[str, Any], arguments: dict[str, Any]) -> dict[str, Any]:
    call = spec.get("call") or {}
    defaults = call.get("defaults") or {}
    merged = {**defaults, **{k: v for k, v in arguments.items() if v is not None}}
    kwargs: dict[str, Any] = {}
    for param, tmpl in (call.get("kwargs") or {}).items():
        m = _PLACEHOLDER.match(str(tmpl).strip())
        if m:
            key = m.group(1)
            if key in merged:
                kwargs[param] = merged[key]
        else:
            kwargs[param] = tmpl
    if call.get("pass_token"):
        kwargs["token"] = arguments.get("_token", "")
    return kwargs


def _coerce_arguments_from_schema(spec: dict[str, Any], arguments: dict[str, Any]) -> dict[str, Any]:
    """将 schema 声明为 integer 的字符串数字转为 int，便于校验与 HTTP 调用。"""
    schema = spec.get("input_schema") or {}
    props = schema.get("properties") or {}
    if not isinstance(props, dict):
        return arguments
    out = dict(arguments)
    for key, meta in props.items():
        if not isinstance(meta, dict) or meta.get("type") != "integer":
            continue
        if key not in out or out[key] is None:
            continue
        n = parse_positive_int(out[key])
        if n is not None:
            out[key] = n
    return out


def _run_validators(spec: dict[str, Any], arguments: dict[str, Any]) -> str | None:
    schema = spec.get("input_schema") or {}
    seen: set[tuple[str, str]] = set()

    def check(field: str, rule: str, val: Any) -> str | None:
        key = (field, rule)
        if key in seen:
            return None
        seen.add(key)
        if val is None and field in set(schema.get("required") or []):
            return f"缺少必填参数 {field}"
        if val is None:
            return None
        custom = None
        for item in spec.get("validators") or []:
            if item.get("field") == field and item.get("rule") == rule:
                custom = item.get("message")
                break
        err = validate_contract(rule, val, field=field)
        if err and custom:
            return str(custom)
        return err

    for item in spec.get("validators") or []:
        field = str(item.get("field") or "")
        rule = str(item.get("rule") or "")
        if not field or not rule:
            continue
        err = check(field, rule, arguments.get(field))
        if err:
            return err

    for field, rule in contracts_for_schema(schema):
        err = check(field, rule, arguments.get(field))
        if err:
            return err

    return None


def _token_from_config(config: RunnableConfig | None) -> str:
    if config and "configurable" in config:
        return config["configurable"].get("token", "") or ""
    return ""


# ---------------------------------------------------------------------------
# 响应 preset：与原先 mcp_public / mcp_user 行为对齐
# ---------------------------------------------------------------------------

def _shape_article_list_item(a: dict) -> dict:
    row = _pick(a, "id", "title", "summary", "view_count", "like_count")
    row["author"] = a.get("author_name") or a.get("user_name") or "未知"
    if isinstance(row.get("summary"), str) and len(row["summary"]) > 100:
        row["summary"] = row["summary"][:100]
    return row


def _apply_response_preset(preset: str, data: dict, arguments: dict[str, Any]) -> Any:
    if preset == "article_search":
        articles = _first_list(data, "articles", "article_list")
        return {
            "total": data.get("total", len(articles)),
            "articles": [_shape_article_list_item(a) for a in articles],
        }
    if preset == "article_detail":
        article = data.get("article", data)
        row = _pick(
            article,
            "id",
            "title",
            "content",
            "summary",
            "author_id",
            "author_name",
            "view_count",
            "like_count",
            "created_at",
            tags=["tags"],
        )
        if isinstance(row.get("content"), str) and len(row["content"]) > 2000:
            row["content"] = row["content"][:2000]
        if "tags" in row and isinstance(row["tags"], list):
            row["tags"] = [t.get("name") if isinstance(t, dict) else t for t in row["tags"]]
        return row
    if preset == "article_leaderboard":
        articles = _first_list(data, "articles", "article_list")
        return {
            "articles": [
                _pick(a, "id", "title", "view_count", "like_count") for a in articles
            ]
        }
    if preset == "comments_list":
        comments = _first_list(data, "comments", "comment_list")
        return {
            "comments": [
                _truncate_fields(
                    _pick(c, "id", "content", "created_at", author=["user_name", "author_name"]),
                    {"content": 200},
                )
                for c in comments
            ]
        }
    if preset == "topics_list":
        topics = _first_list(data, "topics", "topic_list")
        return {
            "topics": [
                _truncate_fields(_pick(t, "id", "name", "description"), {"description": 100})
                for t in topics
            ]
        }
    if preset == "topic_detail":
        topic = data.get("topic", data)
        return _truncate_fields(
            _pick(topic, "id", "name", "description", "article_count"),
            {"description": 200},
        )
    if preset == "topic_articles":
        articles = _first_list(data, "articles", "article_list")
        return {
            "articles": [
                {
                    "id": a.get("id"),
                    "title": a.get("title"),
                    "summary": (a.get("summary") or "")[:80],
                }
                for a in articles
            ]
        }
    if preset == "discussions_list":
        items = _first_list(data, "discussions", "discussion_list")
        return {
            "discussions": [
                _truncate_fields(
                    _pick(d, "id", "content", "created_at", author=["user_name", "author_name"]),
                    {"content": 200},
                )
                for d in items
            ]
        }
    if preset == "games_list":
        games = _first_list(data, "list", "games", "game_list")
        return {
            "games": [_shape_game_row(g, include_description=False) for g in games],
            "total": data.get("total", len(games)),
        }
    if preset == "games_search":
        games = _first_list(data, "games", "list", "game_list")
        rows = []
        for i, g in enumerate(games[:5], 1):
            if not isinstance(g, dict):
                continue
            rows.append(
                {
                    "rank": i,
                    "game_id": g.get("id"),
                    "name": g.get("name"),
                    "publisher": g.get("publisher"),
                    "developer": g.get("developer"),
                }
            )
        return {
            "games": rows,
            "total": len(rows),
            "hint": "name 可为模糊关键词；多条时选与用户意图最匹配的一条 game_id 再调 get_game_detail",
        }
    if preset == "game_detail":
        game = data.get("game", data)
        return _shape_game_row(game if isinstance(game, dict) else {}, include_description=True)
    if preset == "points_mall_products":
        products = _first_list(data, "products", "product_list")
        return {
            "products": [_shape_mall_product(p) for p in products if isinstance(p, dict)],
            "total": len(products),
        }
    if preset == "points_mall_search":
        products = _first_list(data, "products", "product_list")
        rows = []
        for i, p in enumerate(products[:5], 1):
            if not isinstance(p, dict):
                continue
            row = _shape_mall_product(p)
            rows.append(
                {
                    "rank": i,
                    "product_id": row.get("id"),
                    "name": row.get("name"),
                    "subtitle": row.get("subtitle"),
                    "price_points": row.get("price_points"),
                    "stock": row.get("stock"),
                }
            )
        return {
            "products": rows,
            "total": len(rows),
            "hint": "多条时选最匹配的一条 product_id 再调 get_points_mall_product",
        }
    if preset == "points_mall_product":
        raw = data.get("product", data)
        if isinstance(raw, dict):
            return _shape_mall_product(raw)
        return {}
    if preset == "game_reviews":
        reviews = _first_list(data, "reviews", "review_list")
        return {
            "reviews": [
                _truncate_fields(
                    {
                        "id": r.get("id"),
                        "rating": r.get("rating"),
                        "content": (r.get("content") or "")[:150],
                        "author": r.get("user_name") or str(r.get("user_id") or r.get("userId", "未知")),
                    },
                    {"content": 150},
                )
                for r in reviews
            ]
        }
    if preset == "user_public":
        return _pick(data, "user_id", "user_name", "avatar", "github", "follower_count")
    if preset == "me_profile":
        return _pick(data, "user_id", "user_name", "avatar", "github")
    if preset == "me_collections":
        items = _first_list(data, "collections", "article_list")
        return {
            "collections": [
                {
                    "id": i.get("id") or i.get("article_id"),
                    "title": i.get("title", ""),
                    "summary": (i.get("summary") or "")[:80],
                }
                for i in items
            ]
        }
    if preset == "me_followed_topics":
        topics = _first_list(data, "list", "topics", "topic_list")
        return {
            "topics": [
                {
                    "id": t.get("topicId") or t.get("topic_id") or t.get("id"),
                    "name": t.get("topicName") or t.get("topic_name") or t.get("name") or "",
                }
                for t in topics
            ]
        }
    if preset == "me_game_plays":
        plays = _first_list(data, "plays", "game_list", "play_list")
        return {
            "game_plays": [
                {
                    "game_id": p.get("game_id") or p.get("gameId"),
                    "game_name": p.get("game_name") or p.get("name", ""),
                    "status": p.get("status", ""),
                }
                for p in plays
            ]
        }
    if preset == "me_wallet":
        return {"points": data.get("points") or data.get("balance") or 0}
    if preset == "me_mall_orders":
        orders = _first_list(data, "orders", "order_list")
        return {
            "orders": [
                {
                    "order_id": o.get("orderId") or o.get("order_id"),
                    "product_id": o.get("productId") or o.get("product_id"),
                    "product_name": o.get("productName") or o.get("product_name", ""),
                    "points_spent": o.get("pointsSpent") or o.get("points_spent", 0),
                    "code": o.get("code", ""),
                    "created_at": str(o.get("createdAt") or o.get("created_at", "")),
                }
                for o in orders
            ],
            "total": len(orders),
        }
    if preset == "me_game_orders":
        orders = _first_list(data, "orders", "order_list")
        rows = []
        for o in orders:
            pc = o.get("priceCents") if o.get("priceCents") is not None else o.get("price_cents", 0)
            try:
                cents = int(pc)
            except (TypeError, ValueError):
                cents = 0
            rows.append(
                {
                    "order_id": o.get("orderId") or o.get("order_id"),
                    "game_id": o.get("gameId") or o.get("game_id"),
                    "game_name": o.get("gameName") or o.get("game_name", ""),
                    "price_cents": cents,
                    "price_display": _format_price_cents(cents),
                    "code": o.get("code", ""),
                    "created_at": str(o.get("createdAt") or o.get("created_at", "")),
                }
            )
        return {"orders": rows, "total": len(rows)}
    if preset == "me_points_transactions":
        items = _first_list(data, "list", "transactions", "transaction_list")
        page = data.get("page", arguments.get("page", 1))
        size = data.get("size", arguments.get("size", 20))
        total = data.get("total")
        if total is None:
            total = len(items)
        return {
            "transactions": [
                {
                    "txn_id": t.get("txnId") or t.get("txn_id"),
                    "amount": t.get("amount", 0),
                    "type": t.get("type", ""),
                    "description": t.get("description", ""),
                    "balance_after": t.get("balanceAfter") or t.get("balance_after", 0),
                    "ref_type": t.get("refType") or t.get("ref_type", ""),
                    "created_at": str(t.get("createdAt") or t.get("created_at", "")),
                }
                for t in items
            ],
            "total": total,
            "page": page,
            "size": size,
        }
    if preset == "passthrough":
        return data
    raise ValueError(f"未知 response_preset: {preset}")


def _invoke_agent_call(method_name: str, kwargs: dict[str, Any]) -> dict[str, Any]:
    fn = _AGENT_METHODS.get(method_name)
    if fn is None:
        return {"_error": f"Agent 端未实现方法: {method_name}"}
    try:
        out = fn(**kwargs)
        if isinstance(out, dict):
            return out
        return {"result": out}
    except TypeError as e:
        return {"_error": f"参数错误: {e}"}
    except Exception as e:
        return {"_error": str(e)}


def execute_tool(name: str, arguments: dict[str, Any], *, token: str = "") -> str:
    """执行注册表工具，返回 JSON 字符串或 plain 错误文案（与 LangChain 工具一致）。"""
    spec = get_tool_spec(name)
    if not spec:
        return f"未知工具: {name}"

    if spec.get("auth") and not token:
        return "未登录，请先登录。"

    arguments = _coerce_arguments_from_schema(spec, arguments)
    err = _run_validators(spec, arguments)
    if err:
        return err

    call_args = dict(arguments)
    call_args["_token"] = token
    kwargs = _resolve_call_kwargs(spec, call_args)

    call = spec.get("call") or {}
    provider = call.get("provider", "blog")
    method_name = call.get("method")
    if not method_name:
        return "工具配置错误: 缺少 call.method"

    if provider == "agent":
        data = _invoke_agent_call(method_name, kwargs)
    else:
        client = get_client()
        if not hasattr(client, method_name):
            return f"工具配置错误: 缺少 client 方法 {method_name}"
        method: Callable[..., dict] = getattr(client, method_name)
        data = method(**kwargs)

    if isinstance(data, dict) and "_error" in data:
        prefix = spec.get("error_prefix") or "获取失败"
        return f"{prefix}: {data['_error']}"

    preset = spec.get("response_preset", "passthrough")
    shaped = _apply_response_preset(preset, data if isinstance(data, dict) else {}, arguments)
    wrap_key = spec.get("response_wrap")
    if wrap_key:
        return _ok({wrap_key: shaped})
    return _ok(shaped)


def _make_args_model(name: str, spec: dict[str, Any]) -> type[BaseModel]:
    schema = spec.get("input_schema") or {"type": "object", "properties": {}}
    props = schema.get("properties") or {}
    required = set(schema.get("required") or [])
    defaults_map = (spec.get("call") or {}).get("defaults") or {}
    fields: dict[str, Any] = {}
    for pname, pdef in props.items():
        ptype = pdef.get("type", "string")
        if ptype == "integer":
            py_type = int
        elif ptype == "number":
            py_type = float
        elif ptype == "boolean":
            py_type = bool
        else:
            py_type = str
        if pname in required:
            fields[pname] = (py_type, Field(description=pdef.get("description") or ""))
        else:
            default = defaults_map.get(pname, pdef.get("default"))
            fields[pname] = (
                py_type,
                Field(default=default, description=pdef.get("description") or ""),
            )
    safe = re.sub(r"[^a-zA-Z0-9_]", "_", name)
    if not fields:
        return create_model(f"Tool_{safe}_Args")  # type: ignore[call-overload]
    return create_model(f"Tool_{safe}_Args", **fields)  # type: ignore[call-overload]


def _build_langchain_tool(name: str, spec: dict[str, Any]) -> StructuredTool:
    description = (spec.get("description") or spec.get("label") or name).strip()
    args_model = _make_args_model(name, spec)

    if spec.get("auth"):

        def _run(config: Annotated[RunnableConfig, InjectedToolArg()], **kwargs: Any) -> str:
            tok = _token_from_config(config)
            return execute_tool(name, kwargs, token=tok)

        return StructuredTool.from_function(
            func=_run,
            name=name,
            description=description,
            args_schema=args_model,
        )

    def _run(**kwargs: Any) -> str:
        return execute_tool(name, kwargs, token="")

    return StructuredTool.from_function(
        func=_run,
        name=name,
        description=description,
        args_schema=args_model,
    )


def tools_for_group(group: str) -> list[StructuredTool]:
    """按 group（public / user / general）构建 LangChain 工具列表。"""
    tools: list[StructuredTool] = []
    for name, spec in load_registry().get("tools", {}).items():
        if spec.get("group") != group:
            continue
        tools.append(_build_langchain_tool(name, spec))
    return tools


def mcp_list_tools() -> list[dict[str, Any]]:
    """供 MCP list_tools 使用的 Tool 定义（name / description / inputSchema）。"""
    out: list[dict[str, Any]] = []
    for name, spec in load_registry().get("tools", {}).items():
        out.append(
            {
                "name": name,
                "description": (spec.get("description") or spec.get("label") or name).strip(),
                "inputSchema": spec.get("input_schema") or {"type": "object", "properties": {}},
            }
        )
    return out


def mcp_call_tool(name: str, arguments: dict[str, Any], *, token: str = "") -> str:
    """MCP call_tool：成功返回 _ok JSON；失败返回带 _error 的 JSON。"""
    result = execute_tool(name, arguments, token=token)
    stripped = result.strip()
    if stripped.startswith("{"):
        try:
            json.loads(stripped)
            return stripped
        except json.JSONDecodeError:
            pass
    return json.dumps({"_error": result}, ensure_ascii=False)
