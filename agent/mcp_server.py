"""
GoBlog MCP Server —— 将 Go 后端 API 封装为 MCP 工具，只抽取必要字段。

启动方式（stdio）: python mcp_server.py
Agent 通过 langchain_mcp_adapters 连接本项目。
"""

import asyncio
import json

from mcp.server import Server
from mcp.server.stdio import stdio_server
from mcp.types import Tool, TextContent

from blog_client import get_client
from config import load_config

server = Server("goblog-mcp")

client = get_client()


# ============================================================
# 工具格式
# ============================================================

def _ok(data) -> str:
    """将 dict/list 转为紧凑 JSON 字符串。"""
    return json.dumps(data, ensure_ascii=False, default=str)


def _err(msg: str) -> str:
    return json.dumps({"_error": msg}, ensure_ascii=False)


def _pick(obj: dict, *keys: str) -> dict:
    """从 dict 中只取指定 key。"""
    return {k: obj[k] for k in keys if k in obj}


# ============================================================
# 🔵 MCP 工具列表定义
# ============================================================

@server.list_tools()
async def handle_list_tools() -> list[Tool]:
    return [
        # ---- 🟢 文章 ----
        Tool(
            name="search_articles",
            description="在 GoBlog 中按关键词搜索文章，返回列表（标题、摘要、作者、阅读量等）。",
            inputSchema={
                "type": "object",
                "properties": {
                    "query": {
                        "type": "string",
                        "description": "搜索关键词，必填，字符串类型。",
                    },
                    "page": {
                        "type": "integer",
                        "description": "页码，从 1 开始，可选，默认 1。",
                    },
                    "size": {
                        "type": "integer",
                        "description": "每页条数，可选，默认 10。",
                    },
                },
                "required": ["query"],
            },
        ),
        Tool(
            name="get_article",
            description="获取文章完整内容。输入文章 ID，返回标题、正文、作者、阅读量、点赞数、评论数、标签、发布时间。",
            inputSchema={
                "type": "object",
                "properties": {
                    "article_id": {"type": "integer", "description": "文章 ID"}
                },
                "required": ["article_id"]
            }
        ),
        Tool(
            name="list_latest_articles",
            description="获取最新文章列表。按创建时间倒序，返回 ID、标题、摘要、作者、阅读量、点赞数。",
            inputSchema={
                "type": "object",
                "properties": {
                    "page": {"type": "integer", "description": "页码，默认 1"},
                    "size": {"type": "integer", "description": "每页数量，默认 6"}
                }
            }
        ),
        Tool(
            name="get_article_leaderboard",
            description="获取文章排行榜。type='view' 为阅读榜，type='like' 为点赞榜。",
            inputSchema={
                "type": "object",
                "properties": {
                    "type": {"type": "string", "description": "排行榜类型: view 或 like"}
                }
            }
        ),
        Tool(
            name="get_article_comments",
            description="获取某篇文章的评论列表。返回评论内容、评论者昵称、评论时间。",
            inputSchema={
                "type": "object",
                "properties": {
                    "article_id": {"type": "integer", "description": "文章 ID"}
                },
                "required": ["article_id"]
            }
        ),
        # ---- 🟢 用户 ----
        Tool(
            name="get_user_public",
            description="获取用户公开资料：昵称、头像、GitHub 主页、关注者数量。不含手机号、邮箱等隐私信息。",
            inputSchema={
                "type": "object",
                "properties": {
                    "user_id": {"type": "integer", "description": "用户 ID"}
                },
                "required": ["user_id"]
            }
        ),
        # ---- 🟢 话题 ----
        Tool(
            name="list_topics",
            description="获取全站话题列表，返回话题 ID 和名称。",
            inputSchema={
                "type": "object",
                "properties": {}
            }
        ),
        Tool(
            name="get_topic",
            description="获取话题详情，返回话题名称、描述等信息。",
            inputSchema={
                "type": "object",
                "properties": {
                    "topic_id": {"type": "integer", "description": "话题 ID"}
                },
                "required": ["topic_id"]
            }
        ),
        Tool(
            name="get_topic_articles",
            description="获取某个话题下的文章列表。",
            inputSchema={
                "type": "object",
                "properties": {
                    "topic_id": {"type": "integer", "description": "话题 ID"}
                },
                "required": ["topic_id"]
            }
        ),
        Tool(
            name="get_topic_discussions",
            description="获取话题下的讨论/回复内容。",
            inputSchema={
                "type": "object",
                "properties": {
                    "topic_id": {"type": "integer", "description": "话题 ID"}
                },
                "required": ["topic_id"]
            }
        ),
        # ---- 🟢 游戏 ----
        Tool(
            name="list_games",
            description="获取游戏库列表，返回游戏名称、发行商、发行日期。",
            inputSchema={
                "type": "object",
                "properties": {}
            }
        ),
        Tool(
            name="get_game",
            description="获取游戏详情：名称、描述、发行商、开发商、发行日期、成就列表。",
            inputSchema={
                "type": "object",
                "properties": {
                    "game_id": {"type": "integer", "description": "游戏 ID"}
                },
                "required": ["game_id"]
            }
        ),
        Tool(
            name="get_game_reviews",
            description="获取游戏的玩家点评列表，返回点评内容、评分、点评者。",
            inputSchema={
                "type": "object",
                "properties": {
                    "game_id": {"type": "integer", "description": "游戏 ID"}
                },
                "required": ["game_id"]
            }
        ),
        # ---- 🟡 个人 ----
        Tool(
            name="get_my_profile",
            description="获取当前登录用户的基本信息：昵称、头像、GitHub 主页。【注意】不返回手机号、邮箱等敏感字段。需要登录。",
            inputSchema={
                "type": "object",
                "properties": {}
            }
        ),
        Tool(
            name="get_my_collections",
            description="获取当前用户收藏的文章列表。需要登录。",
            inputSchema={
                "type": "object",
                "properties": {}
            }
        ),
        Tool(
            name="get_my_followed_topics",
            description="获取当前用户关注的话题列表。需要登录。",
            inputSchema={
                "type": "object",
                "properties": {}
            }
        ),
        Tool(
            name="get_my_articles",
            description="获取当前用户自己发布的文章列表。需要登录。",
            inputSchema={
                "type": "object",
                "properties": {
                    "page": {"type": "integer", "description": "页码，默认 1"},
                    "size": {"type": "integer", "description": "每页数量，默认 10"}
                }
            }
        ),
        Tool(
            name="get_my_game_plays",
            description="获取当前用户的游戏游玩/通关记录。需要登录。",
            inputSchema={
                "type": "object",
                "properties": {}
            }
        ),
        Tool(
            name="get_my_wallet",
            description="获取当前用户的积分余额。需要登录。",
            inputSchema={
                "type": "object",
                "properties": {}
            }
        ),
    ]


# ============================================================
# 🔵 MCP 工具调用实现
# ============================================================

def _token_from(args: dict) -> str:
    """从参数中取 token（由 Agent config 注入）。"""
    return args.get("token", "")


def _article_list_item(a: dict) -> dict:
    """抽取文章列表中一篇文章的关键字段。"""
    return _pick(a, "id", "title", "summary",
                 "authorId", "authorName", "authorAvatar",
                 "viewCount", "likeCount", "commentCount",
                 "createdAt", "tags")


def _article_detail(a: dict) -> dict:
    """抽取文章详情的完整字段（含正文）。"""
    fields = ("id", "title", "summary", "content",
              "authorId", "authorName", "authorAvatar",
              "viewCount", "likeCount", "commentCount",
              "createdAt", "updatedAt", "tags")
    result = _pick(a, *fields)
    # 内容可能很长，截断超过 3000 字符的部分
    if "content" in result and isinstance(result["content"], str) and len(result["content"]) > 3000:
        result["content"] = result["content"][:3000] + "...(内容较长，已截断)"
    return result


@server.call_tool()
async def handle_call_tool(name: str, arguments: dict) -> list[TextContent]:
    try:
        token = _token_from(arguments)

        # ---- 🟢 文章 ----
        if name == "search_articles":
            query = arguments.get("query")
            if not isinstance(query, str) or not query.strip():
                return [TextContent(type="text", text=_err("参数 query 必须为非空字符串"))]
            data = client.search(
                query.strip(),
                int(arguments.get("page", 1)),
                int(arguments.get("size", 10)),
            )
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            articles = data.get("articles", []) or data.get("article_list", [])
            total = data.get("total", len(articles))
            return [TextContent(type="text", text=_ok({
                "articles": [_article_list_item(a) for a in articles],
                "total": total
            }))]

        elif name == "get_article":
            data = client.get_article(arguments["article_id"])
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            # Go 可能返回 {article: {...}} 或直接返回 article 字段
            article = data.get("article", data)
            return [TextContent(type="text", text=_ok(_article_detail(article)))]

        elif name == "list_latest_articles":
            page = arguments.get("page", 1)
            size = arguments.get("size", 6)
            data = client.list_latest_articles(page=page, size=size)
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            articles = data.get("article_list", [])
            return [TextContent(type="text", text=_ok({
                "articles": [_article_list_item(a) for a in articles],
                "total": data.get("total", len(articles))
            }))]

        elif name == "get_article_leaderboard":
            tp = arguments.get("type", "view")
            data = client.get_article_leaderboard(tp=tp)
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            articles = data.get("article_list", [])
            return [TextContent(type="text", text=_ok({
                "articles": [_article_list_item(a) for a in articles],
                "type": tp
            }))]

        elif name == "get_article_comments":
            data = client.get_article_comments(arguments["article_id"])
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            comments = data.get("comments", []) or data.get("comment_list", [])
            return [TextContent(type="text", text=_ok({
                "comments": [_pick(c, "id", "content", "userId", "userName",
                                   "createdAt", "likeCount") for c in comments]
            }))]

        # ---- 🟢 用户 ----
        elif name == "get_user_public":
            data = client.get_user_public(arguments["user_id"])
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            return [TextContent(type="text", text=_ok(_pick(data, "user_id", "user_name", "avatar", "github", "follower_count")))]

        # ---- 🟢 话题 ----
        elif name == "list_topics":
            data = client.list_topics()
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            topics = data.get("topics", []) or data.get("topic_list", [])
            return [TextContent(type="text", text=_ok({
                "topics": [_pick(t, "id", "name", "description") for t in topics]
            }))]

        elif name == "get_topic":
            data = client.get_topic(arguments["topic_id"])
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            topic = data.get("topic", data)
            return [TextContent(type="text", text=_ok(_pick(topic, "id", "name", "description", "createdAt", "articleCount")))]

        elif name == "get_topic_articles":
            data = client.get_topic_articles(arguments["topic_id"])
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            articles = data.get("articles", []) or data.get("article_list", [])
            return [TextContent(type="text", text=_ok({
                "articles": [_article_list_item(a) for a in articles]
            }))]

        elif name == "get_topic_discussions":
            data = client.get_topic_discussions(arguments["topic_id"])
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            discussions = data.get("discussions", []) or data.get("discussion_list", [])
            return [TextContent(type="text", text=_ok({
                "discussions": [_pick(d, "id", "content", "userId", "userName", "createdAt") for d in discussions]
            }))]

        # ---- 🟢 游戏 ----
        elif name == "list_games":
            data = client.list_games()
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            games = data.get("games", []) or data.get("game_list", [])
            return [TextContent(type="text", text=_ok({
                "games": [_pick(g, "id", "name", "publisher", "releaseAt") for g in games]
            }))]

        elif name == "get_game":
            data = client.get_game(arguments["game_id"])
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            game = data.get("game", data)
            return [TextContent(type="text", text=_ok(
                _pick(game, "id", "name", "description", "publisher", "developer", "releaseAt", "achievements")
            ))]

        elif name == "get_game_reviews":
            data = client.get_game_reviews(arguments["game_id"])
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            reviews = data.get("reviews", []) or data.get("review_list", [])
            return [TextContent(type="text", text=_ok({
                "reviews": [_pick(r, "id", "content", "rating", "userId", "userName", "reviewedAt") for r in reviews]
            }))]

        # ---- 🟡 个人 ----
        elif name == "get_my_profile":
            if not token: return [TextContent(type="text", text=_err("需要登录"))]
            data = client.get_my_profile(token)
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            return [TextContent(type="text", text=_ok(_pick(data, "user_id", "user_name", "avatar", "github")))]

        elif name == "get_my_collections":
            if not token: return [TextContent(type="text", text=_err("需要登录"))]
            data = client.get_my_collections(token)
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            articles = data.get("article_list", []) or data.get("collections", [])
            return [TextContent(type="text", text=_ok({
                "collections": [_article_list_item(a) for a in articles]
            }))]

        elif name == "get_my_followed_topics":
            if not token: return [TextContent(type="text", text=_err("需要登录"))]
            data = client.get_my_followed_topics(token)
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            topics = data.get("list", []) or data.get("topics", []) or data.get("topic_list", [])
            return [TextContent(type="text", text=_ok({
                "topics": [{
                    "topicId": t.get("topicId") or t.get("topic_id"),
                    "topicName": t.get("topicName") or t.get("topic_name") or t.get("name", ""),
                } for t in topics]
            }))]

        elif name == "get_my_articles":
            if not token: return [TextContent(type="text", text=_err("需要登录"))]
            page = arguments.get("page", 1)
            size = arguments.get("size", 10)
            data = client.get_my_articles(token, page=page, size=size)
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            articles = data.get("article_list", [])
            return [TextContent(type="text", text=_ok({
                "articles": [_article_list_item(a) for a in articles],
                "total": data.get("total", len(articles))
            }))]

        elif name == "get_my_game_plays":
            if not token: return [TextContent(type="text", text=_err("需要登录"))]
            data = client.get_my_game_plays(token)
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            plays = data.get("plays", []) or data.get("play_list", [])
            return [TextContent(type="text", text=_ok({
                "plays": [_pick(p, "gameId", "gameName", "playedAt", "hours") for p in plays]
            }))]

        elif name == "get_my_wallet":
            if not token: return [TextContent(type="text", text=_err("需要登录"))]
            data = client.get_my_wallet(token)
            if "_error" in data: return [TextContent(type="text", text=_err(data["_error"]))]
            return [TextContent(type="text", text=_ok(_pick(data, "points", "balance", "wallet")))]

        else:
            return [TextContent(type="text", text=_err(f"未知工具: {name}"))]

    except Exception as e:
        return [TextContent(type="text", text=_err(f"工具执行失败: {e}"))]


# ============================================================
# 启动
# ============================================================

async def main():
    async with stdio_server() as (read_stream, write_stream):
        await server.run(read_stream, write_stream, server.create_initialization_options())


if __name__ == "__main__":
    asyncio.run(main())
