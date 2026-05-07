"""
MCP Server: blog-user（用户授权型）
需要用户 Token（通过 RunnableConfig 注入，LLM 不可见）。
覆盖：我的资料、我的文章、收藏、关注、游戏记录、积分。
每个工具只抽取必要字段，敏感字段（email/tel/password）绝不返回。
"""
import json

from langchain.tools import tool
from langchain_core.runnables import RunnableConfig

from blog_client import get_client

_client = get_client()


def _ok(data) -> str:
    return json.dumps(data, ensure_ascii=False, default=str)


def _token(config: RunnableConfig) -> str:
    if config and "configurable" in config:
        return config["configurable"].get("token", "")
    return ""


# ============================================================
# me_get_data — 查询类
# ============================================================

@tool
def me_get_profile(config: RunnableConfig) -> str:
    """获取我的个人资料。返回 user_id/name/avatar/github，不含手机和邮箱。"""
    t = _token(config)
    if not t:
        return "未登录，请先登录。"
    data = _client.get_my_profile(t)
    if "_error" in data:
        return f"获取失败: {data['_error']}"
    return _ok({
        "user_id": data.get("user_id"),
        "user_name": data.get("user_name"),
        "avatar": data.get("avatar"),
        "github": data.get("github"),
    })


@tool
def me_get_articles(page: int = 1, size: int = 10, config: RunnableConfig = None) -> str:
    """获取我发布的文章列表。page=页码，size=每页条数。返回 id/title/summary/view_count。"""
    t = _token(config)
    if not t:
        return "未登录，请先登录。"
    data = _client.get_my_articles(t, page, size)
    if "_error" in data:
        return f"获取失败: {data['_error']}"
    articles = data.get("article_list") or []
    results = [{"id": a.get("id"), "title": a.get("title"), "summary": (a.get("summary") or "")[:80], "view_count": a.get("view_count")} for a in articles]
    return _ok({"total": data.get("total", len(results)), "articles": results})


@tool
def me_get_collections(config: RunnableConfig) -> str:
    """获取我收藏的文章列表。返回文章 id/title/summary。"""
    t = _token(config)
    if not t:
        return "未登录，请先登录。"
    data = _client.get_my_collections(t)
    if "_error" in data:
        return f"获取失败: {data['_error']}"
    items = data.get("collections") or data.get("article_list") or []
    results = [{"id": i.get("id") or i.get("article_id"), "title": i.get("title", ""), "summary": (i.get("summary") or "")[:80]} for i in items]
    return _ok({"collections": results})


@tool
def me_get_followed_topics(config: RunnableConfig) -> str:
    """获取我关注的话题列表。返回话题 id/name。"""
    t = _token(config)
    if not t:
        return "未登录，请先登录。"
    data = _client.get_my_followed_topics(t)
    if "_error" in data:
        return f"获取失败: {data['_error']}"
    topics = data.get("list") or data.get("topics") or data.get("topic_list") or []
    results = []
    for tp in topics:
        tid = tp.get("topicId") or tp.get("topic_id") or tp.get("id")
        nm = tp.get("topicName") or tp.get("topic_name") or tp.get("name") or ""
        results.append({"id": tid, "name": nm})
    return _ok({"topics": results})


@tool
def me_get_game_plays(config: RunnableConfig) -> str:
    """获取我的游戏游玩记录。返回游戏 id/name/游玩状态。"""
    t = _token(config)
    if not t:
        return "未登录，请先登录。"
    data = _client.get_my_game_plays(t)
    if "_error" in data:
        return f"获取失败: {data['_error']}"
    plays = data.get("plays") or data.get("game_list") or []
    results = [{"game_id": p.get("game_id") or p.get("gameId"), "game_name": p.get("game_name") or p.get("name", ""), "status": p.get("status", "")} for p in plays]
    return _ok({"game_plays": results})


@tool
def me_get_wallet(config: RunnableConfig) -> str:
    """获取我的积分余额。返回积分数量。"""
    t = _token(config)
    if not t:
        return "未登录，请先登录。"
    data = _client.get_my_wallet(t)
    if "_error" in data:
        return f"获取失败: {data['_error']}"
    return _ok({"points": data.get("points") or data.get("balance") or 0})


# ============================================================
# 工具注册
# ============================================================

USER_TOOLS = [
    me_get_profile,
    me_get_articles,
    me_get_collections,
    me_get_followed_topics,
    me_get_game_plays,
    me_get_wallet,
]
