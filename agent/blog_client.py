"""Go Blog API 客户端 —— 纯 HTTP 调用，不做任何字段过滤。字段抽取由 MCP 层负责。"""

import json
import urllib.error
import urllib.request
from typing import Optional
from urllib.parse import quote_plus, urlencode

from config import load_config
from agent_log import get_logger

_log = get_logger("blog_client")


class BlogClient:
    """对 Go 后端 API 的 HTTP 客户端。只负责请求/响应解析，不做数据过滤。"""

    def __init__(self, base_url: str = "", timeout: int = 30):
        cfg = load_config()
        self.base_url = base_url or cfg.get("blog_api_url", "http://127.0.0.1:8084/api/v1")
        self.timeout = timeout

    # ================================================================
    # 内部方法
    # ================================================================

    def _get(self, path: str, params: dict | None = None, token: str = "") -> dict:
        url = f"{self.base_url}{path}"
        if params:
            # 必须用 UTF-8 百分号编码；否则中文会触发 ascii codec 错误
            flat = {k: str(v) for k, v in params.items() if v is not None and str(v) != ""}
            if flat:
                url += "?" + urlencode(flat, encoding="utf-8", quote_via=quote_plus)
        return self._request(url, method="GET", token=token)

    def _post(self, path: str, body: dict | None = None, token: str = "") -> dict:
        url = f"{self.base_url}{path}"
        return self._request(url, method="POST", body=body, token=token)

    def _request(self, url: str, method: str = "GET", body: dict | None = None, token: str = "") -> dict:
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = f"Bearer {token}"

        data_bytes = None
        if body:
            data_bytes = json.dumps(body).encode("utf-8")

        req = urllib.request.Request(url, data=data_bytes, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                raw = json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            err_body = e.read().decode("utf-8", errors="replace")
            _log.error("HTTPError", extra={"url": url, "status": e.code, "body": err_body[:200]})
            return {"_error": f"HTTP {e.code}: {err_body}"}
        except urllib.error.URLError as e:
            _log.error("URLError", extra={"url": url, "reason": str(e.reason)})
            return {"_error": f"请求失败: {e.reason}"}
        except TimeoutError:
            _log.error("Timeout", extra={"url": url, "timeout": self.timeout})
            return {"_error": f"请求超时 ({self.timeout}s)"}
        except Exception as e:
            _log.error("RequestError", extra={"url": url, "error": str(e)})
            return {"_error": str(e)}

        if isinstance(raw, dict):
            if raw.get("code") == 0:
                return raw.get("data", raw)
            return {"_error": raw.get("message") or raw.get("msg") or f"code={raw.get('code')}"}
        return raw

    # ================================================================
    # 🟢 公开 API
    # ================================================================

    def search(self, q: str, page: int = 1, size: int = 10) -> dict:
        return self._get("/search", {"q": q, "page": str(page), "size": str(size)})

    def get_article(self, article_id: int) -> dict:
        return self._get(f"/articles/{article_id}")

    def list_latest_articles(self, author_id: int = 0, page: int = 1, size: int = 6) -> dict:
        params = {"page": str(page), "size": str(size)}
        if author_id:
            params["author_id"] = str(author_id)
        return self._get("/articles/latest", params)

    def get_article_leaderboard(self, tp: str = "view", page: int = 1, size: int = 10) -> dict:
        return self._get("/articles/leaderboard", {"type": tp, "page": str(page), "size": str(size)})

    def get_article_comments(self, article_id: int) -> dict:
        return self._get("/articles/comments", {"article_id": str(article_id)})

    def get_user_public(self, user_id: int) -> dict:
        return self._get(f"/users/{user_id}")

    def list_topics(self) -> dict:
        return self._get("/topics")

    def get_topic(self, topic_id: int) -> dict:
        return self._get(f"/topics/{topic_id}")

    def get_topic_articles(self, topic_id: int) -> dict:
        return self._get(f"/topics/{topic_id}/articles")

    def get_topic_discussions(self, topic_id: int) -> dict:
        return self._get(f"/topics/{topic_id}/discussions")

    def list_games(self) -> dict:
        return self._get("/games")

    def get_game(self, game_id: int) -> dict:
        return self._get(f"/games/{game_id}")

    def get_game_reviews(self, game_id: int) -> dict:
        return self._get(f"/games/{game_id}/reviews")

    # ================================================================
    # 🟡 个人 API（需 token）
    # ================================================================

    def get_my_profile(self, token: str) -> dict:
        return self._get("/users/me", token=token)

    def get_my_collections(self, token: str) -> dict:
        return self._get("/articles/collect", token=token)

    def get_my_followed_topics(self, token: str) -> dict:
        return self._get("/follow/topics", token=token)

    def get_my_articles(self, token: str, page: int = 1, size: int = 10) -> dict:
        return self._get("/articles", {"page": str(page), "size": str(size)}, token=token)

    def get_my_game_plays(self, token: str) -> dict:
        return self._get("/games/play", token=token)

    def get_my_wallet(self, token: str) -> dict:
        return self._get("/points/wallet", token=token)


# 全局单例
_client: Optional[BlogClient] = None


def get_client() -> BlogClient:
    global _client
    if _client is None:
        _client = BlogClient()
    return _client
