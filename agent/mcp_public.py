"""
MCP Server: blog-public（公开只读型）
无需鉴权，可安全缓存。覆盖：文章浏览、搜索、话题、游戏、用户公开资料。
每个工具只抽取必要字段，不 dump 原始 JSON。
"""
import json
from datetime import datetime

from langchain.tools import tool

from blog_client import get_client

_client = get_client()


def _ok(data) -> str:
    return json.dumps(data, ensure_ascii=False, default=str)


# ============================================================
# 文章
# ============================================================


@tool
def search_articles(query: str, page: int = 1, size: int = 10) -> str:
    """全文搜索文章。

    参数：query 为搜索关键词（str）；page 为页码从 1 起；size 为每页条数。
    返回 id、title、summary、author 等。
    """
    data = _client.search(query, page, size)
    if "_error" in data:
        return f"搜索失败: {data['_error']}"
    articles = data.get("articles") or data.get("article_list") or []
    results = []
    for a in articles:
        results.append({
            "id": a.get("id"),
            "title": a.get("title"),
            "summary": a.get("summary", "")[:100],
            "author": a.get("author_name") or a.get("user_name", "未知"),
            "view_count": a.get("view_count"),
        })
    total = data.get("total", len(results))
    return _ok({"total": total, "articles": results})


@tool
def get_article_detail(article_id: int) -> str:
    """查看文章详情。返回标题、正文（截断2000字）、作者、浏览量、点赞数、标签。"""
    data = _client.get_article(article_id)
    if "_error" in data:
        return f"文章不存在: {data['_error']}"
    return _ok({
        "id": data.get("id"),
        "title": data.get("title"),
        "content": (data.get("content", "") or "")[:2000],
        "author_id": data.get("author_id") or data.get("authorId"),
        "author_name": data.get("author_name"),
        "view_count": data.get("view_count") or data.get("viewCount"),
        "like_count": data.get("like_count") or data.get("likeCount"),
        "summary": data.get("summary"),
        "tags": [t.get("name") for t in (data.get("tags") or [])],
        "created_at": data.get("created_at") or data.get("createdAt"),
    })


@tool
def list_latest_articles(page: int = 1, size: int = 6) -> str:
    """最新文章列表。page=页码，size=每页条数(默认6)。返回 id/title/summary/author/view_count。"""
    data = _client.list_latest_articles(page=page, size=size)
    if "_error" in data:
        return f"获取失败: {data['_error']}"
    articles = data.get("article_list") or []
    results = []
    for a in articles:
        results.append({
            "id": a.get("id"),
            "title": a.get("title"),
            "summary": (a.get("summary") or "")[:80],
            "author": a.get("author_name") or a.get("user_name", "未知"),
            "view_count": a.get("view_count") or a.get("viewCount"),
        })
    return _ok({"total": data.get("total", len(results)), "articles": results})


@tool
def get_article_leaderboard(sort_by: str = "view") -> str:
    """文章排行榜。sort_by='view' 按阅读量，'like' 按点赞数。返回 top10。"""
    data = _client.get_article_leaderboard(tp=sort_by)
    if "_error" in data:
        return f"获取失败: {data['_error']}"
    articles = data.get("article_list") or []
    results = []
    for a in articles:
        results.append({
            "id": a.get("id"),
            "title": a.get("title"),
            "view_count": a.get("view_count") or a.get("viewCount"),
            "like_count": a.get("like_count") or a.get("likeCount"),
        })
    return _ok({"articles": results})


@tool
def get_article_comments(article_id: int) -> str:
    """查看某篇文章的评论。返回评论内容、作者、时间。"""
    data = _client.get_article_comments(article_id)
    if "_error" in data:
        return f"获取评论失败: {data['_error']}"
    comments = data.get("comments") or data.get("comment_list") or []
    results = []
    for c in comments:
        results.append({
            "id": c.get("id"),
            "content": (c.get("content") or "")[:200],
            "author": c.get("user_name") or c.get("author_name", "未知"),
            "created_at": c.get("created_at") or c.get("createdAt"),
        })
    return _ok({"comments": results})


# ============================================================
# 话题
# ============================================================

@tool
def list_all_topics() -> str:
    """获取所有话题列表。返回 id/name/description。"""
    data = _client.list_topics()
    if "_error" in data:
        return f"获取失败: {data['_error']}"
    topics = data.get("topics") or data.get("topic_list") or []
    results = [{"id": t.get("id"), "name": t.get("name"), "description": t.get("description", "")[:100]} for t in topics]
    return _ok({"topics": results})


@tool
def get_topic_detail(topic_id: int) -> str:
    """查看话题详情。返回话题名称、描述、文章数。"""
    data = _client.get_topic(topic_id)
    if "_error" in data:
        return f"话题不存在: {data['_error']}"
    return _ok({
        "id": data.get("id"),
        "name": data.get("name"),
        "description": data.get("description", "")[:200],
        "article_count": data.get("article_count", 0),
    })


@tool
def get_topic_articles(topic_id: int) -> str:
    """查看话题下的文章列表。返回文章 id/title/summary。"""
    data = _client.get_topic_articles(topic_id)
    if "_error" in data:
        return f"获取失败: {data['_error']}"
    articles = data.get("articles") or data.get("article_list") or []
    results = [{"id": a.get("id"), "title": a.get("title"), "summary": (a.get("summary") or "")[:80]} for a in articles]
    return _ok({"articles": results})


@tool
def get_topic_discussions(topic_id: int) -> str:
    """查看话题下的讨论。返回讨论内容、作者、时间。"""
    data = _client.get_topic_discussions(topic_id)
    if "_error" in data:
        return f"获取失败: {data['_error']}"
    items = data.get("discussions") or data.get("discussion_list") or []
    results = []
    for d in items:
        results.append({
            "id": d.get("id"),
            "content": (d.get("content") or "")[:200],
            "author": d.get("user_name") or d.get("author_name", "未知"),
            "created_at": d.get("created_at") or d.get("createdAt"),
        })
    return _ok({"discussions": results})


# ============================================================
# 游戏
# ============================================================

@tool
def list_all_games() -> str:
    """游戏库列表。返回 id/name/publisher/developer。"""
    data = _client.list_games()
    if "_error" in data:
        return f"获取失败: {data['_error']}"
    games = data.get("games") or data.get("game_list") or []
    results = [{"id": g.get("id"), "name": g.get("name"), "publisher": g.get("publisher", ""), "developer": g.get("developer", "")} for g in games]
    return _ok({"games": results})


@tool
def get_game_detail(game_id: int) -> str:
    """查看游戏详情。返回名称、描述、发行商、开发商、发行日期。"""
    data = _client.get_game(game_id)
    if "_error" in data:
        return f"游戏不存在: {data['_error']}"
    return _ok({
        "id": data.get("id"),
        "name": data.get("name"),
        "description": (data.get("description") or "")[:300],
        "publisher": data.get("publisher"),
        "developer": data.get("developer"),
        "release_at": data.get("release_at") or data.get("releaseAt"),
    })


@tool
def get_game_reviews(game_id: int) -> str:
    """查看游戏的玩家点评。返回评分、内容摘要、作者。"""
    data = _client.get_game_reviews(game_id)
    if "_error" in data:
        return f"获取失败: {data['_error']}"
    reviews = data.get("reviews") or data.get("review_list") or []
    results = []
    for r in reviews:
        results.append({
            "id": r.get("id"),
            "rating": r.get("rating"),
            "content": (r.get("content") or "")[:150],
            "author": r.get("user_name") or str(r.get("user_id") or r.get("userId", "未知")),
        })
    return _ok({"reviews": results})


# ============================================================
# 用户（公开）
# ============================================================

@tool
def get_user_public_profile(user_id: int) -> str:
    """查看用户公开资料。只返回 name/avatar/github/follower_count，不含手机/邮箱。"""
    data = _client.get_user_public(user_id)
    if "_error" in data:
        return f"用户不存在: {data['_error']}"
    return _ok({
        "user_id": data.get("user_id"),
        "user_name": data.get("user_name"),
        "avatar": data.get("avatar"),
        "github": data.get("github"),
        "follower_count": data.get("follower_count"),
    })


# ============================================================
# 工具注册
# ============================================================

PUBLIC_TOOLS = [
    search_articles,
    get_article_detail,
    list_latest_articles,
    get_article_leaderboard,
    get_article_comments,
    list_all_topics,
    get_topic_detail,
    get_topic_articles,
    get_topic_discussions,
    list_all_games,
    get_game_detail,
    get_game_reviews,
    get_user_public_profile,
]
