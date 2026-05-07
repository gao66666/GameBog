import json
from datetime import datetime, timezone

import redis.asyncio as redis
from langchain_openai import ChatOpenAI
from langchain_core.messages import HumanMessage, SystemMessage

from config import load_config


_redis = None


async def init_redis():
    global _redis
    cfg = load_config().get("redis", {})
    _redis = redis.Redis(
        host=cfg.get("host", "localhost"),
        port=cfg.get("port", 6379),
        password=cfg.get("password", ""),
        db=cfg.get("db", 0),
        decode_responses=True,
    )


def _r():
    return _redis


def _norm_chat_session_id(session_id: str | None) -> str:
    s = (session_id or "").strip()
    return s if s else "default"


def _short_list_key(user_id: int, session_id: str | None = None) -> str:
    sid = _norm_chat_session_id(session_id)
    return f"agent:short:{user_id}:{sid}:list"


def _short_state_key(user_id: int, session_id: str | None = None) -> str:
    sid = _norm_chat_session_id(session_id)
    return f"agent:short:{user_id}:{sid}:state"


def _mid_summary_key(user_id: int) -> str:
    return f"agent:mid:{user_id}:summary:list"


async def get_short_messages(user_id: int, limit: int, session_id: str | None = None) -> list[dict]:
    if limit <= 0:
        return []
    key = _short_list_key(user_id, session_id)
    raw = await _r().lrange(key, -limit, -1)
    messages = []
    for item in raw:
        try:
            msg = json.loads(item)
            if isinstance(msg, dict) and msg.get("role") and msg.get("content") is not None:
                messages.append({"role": msg["role"], "content": msg["content"]})
        except json.JSONDecodeError:
            continue
    return messages


async def append_short_messages(
    user_id: int, messages: list[dict], ttl_minutes: int, session_id: str | None = None
) -> None:
    if not messages:
        return
    key = _short_list_key(user_id, session_id)
    state_key = _short_state_key(user_id, session_id)
    ttl_seconds = max(1, ttl_minutes * 60)
    now = datetime.utcnow().isoformat()
    last = messages[-1]

    pipe = _r().pipeline()
    for msg in messages:
        pipe.rpush(key, json.dumps(msg, ensure_ascii=False))
    pipe.expire(key, ttl_seconds)

    state = {
        "updated_at": now,
        "last_role": last.get("role", ""),
        "last_content": last.get("content", ""),
    }
    pipe.set(state_key, json.dumps(state, ensure_ascii=False))
    pipe.expire(state_key, ttl_seconds)
    await pipe.execute()


async def pop_short_messages(user_id: int, count: int, session_id: str | None = None) -> int:
    if count <= 0:
        return 0
    key = _short_list_key(user_id, session_id)
    pipe = _r().pipeline()
    for _ in range(count):
        pipe.lpop(key)
    results = await pipe.execute()
    return sum(1 for item in results if item is not None)


def _utc_now_z() -> str:
    return datetime.utcnow().replace(microsecond=0).isoformat() + "Z"


def _parse_iso_z(s: str) -> datetime | None:
    if not s or not isinstance(s, str):
        return None
    t = s.strip()
    if t.endswith("Z"):
        t = t[:-1] + "+00:00"
    try:
        dt = datetime.fromisoformat(t)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        return dt
    except ValueError:
        return None


def _normalize_mid_facts_struct(summary: dict[str, object], mem_cfg: dict) -> None:
    """facts 统一为 [{at, text}]；兼容旧版字符串；按条数/总字数硬裁剪，从时间最早的一条开始丢。"""
    max_items = max(1, int(mem_cfg.get("mid_facts_max_items", 10)))
    max_total = max(1, int(mem_cfg.get("mid_facts_max_chars", 560)))
    max_each = max(40, int(mem_cfg.get("mid_fact_max_chars_per_item", 180)))

    now_z = _utc_now_z()
    raw = summary.get("facts")
    rows: list[dict[str, str]] = []

    if isinstance(raw, str) and raw.strip():
        t = raw.strip()
        if len(t) > max_each:
            t = t[: max_each - 10].rstrip() + "…"
        rows.append({"at": now_z, "text": t})
    elif isinstance(raw, list):
        for it in raw:
            if isinstance(it, dict):
                at = str(it.get("at") or it.get("t") or "").strip() or now_z
                txt = str(it.get("text") or it.get("v") or "").strip()
                if txt:
                    if len(txt) > max_each:
                        txt = txt[: max_each - 10].rstrip() + "…"
                    rows.append({"at": at, "text": txt})
            elif isinstance(it, str) and it.strip():
                txt = it.strip()
                if len(txt) > max_each:
                    txt = txt[: max_each - 10].rstrip() + "…"
                rows.append({"at": now_z, "text": txt})

    _epoch = datetime(1970, 1, 1, tzinfo=timezone.utc)
    rows.sort(key=lambda r: _parse_iso_z(r["at"]) or _epoch, reverse=True)
    rows = rows[:max_items]

    def _total_txt(rs: list[dict[str, str]]) -> int:
        return sum(len(x["text"]) for x in rs)

    while rows and _total_txt(rows) > max_total:
        rows.pop()

    summary["facts"] = rows


async def get_mid_summaries(user_id: int, limit: int) -> list[dict]:
    if limit <= 0:
        return []
    raw = await _r().lrange(_mid_summary_key(user_id), -limit, -1)
    items = []
    for item in raw:
        try:
            data = json.loads(item)
            if isinstance(data, dict):
                items.append(data)
        except json.JSONDecodeError:
            continue
    return items


async def update_mid_summary(user_id: int, prev_summary: dict | None, history_messages: list[dict]) -> dict | None:
    cfg = load_config()
    mem_cfg = cfg.get("memory", {})
    if not mem_cfg.get("summary_enabled", True):
        return None

    if not history_messages:
        return prev_summary

    summary_cfg = cfg.get("summary_llm", {})
    llm_kwargs = dict(
        model=summary_cfg.get("model", cfg["model"]),
        base_url=summary_cfg.get("base_url", cfg["base_url"]),
        api_key=summary_cfg.get("api_key", cfg["api_key"]),
        temperature=summary_cfg.get("temperature", 0),
    )
    if summary_cfg.get("thinking", False):
        llm_kwargs["extra_body"] = {"thinking": {"type": "enabled"}}
    else:
        llm_kwargs["extra_body"] = {"thinking": {"type": "disabled"}}
    llm = ChatOpenAI(**llm_kwargs)

    fc_max = int(mem_cfg.get("mid_facts_max_chars", 560))
    fi_max = int(mem_cfg.get("mid_facts_max_items", 10))

    system = (
        "你是对话摘要器，只输出严格 JSON，不要输出任何解释。"
        'JSON 结构：{"topic":"","stage":"","pending":[],"subtask":"","facts":[{"at":"","text":""}]}'
        "其中 stage 只能是：提问/信息收集中/执行中/答疑中/已完结。"
        "pending 最多 3 条。"
        "【facts 规则】facts 必须是数组，每项含 "
        '"at"（UTC ISO8601，结尾 Z）与 "text"（一句要点）。'
        "at 表示你认为本条要点「当前仍有效」的参照时间：新结论可用给定当前 UTC；延续旧要点可沿用原 at 或略更新。"
        "合并时要主动淘汰：与新增对话矛盾、已过时、重复或已被长期记忆接管的内容不要再保留。"
        "同一主题只保留最新一条；条目总数不要超过 "
        f"{fi_max} 条，text 合计不要超过约 {fc_max} 字。"
        "若先前摘要是旧版 facts 长字符串，请在本轮改写成上述数组结构。"
        "subtask 一句即可。"
        "你要基于先前摘要和新增的旧短期原文，生成新的中期摘要。"
    )

    prev = json.dumps(prev_summary or {}, ensure_ascii=False)
    history_json = json.dumps(history_messages, ensure_ascii=False)
    prompt = (
        f"当前 UTC：{_utc_now_z()}\n"
        f"先前摘要：{prev}\n"
        f"新增旧短期原文：{history_json}\n"
        "请更新摘要：topic/stage/pending/subtask 随对话演进；facts 输出带时间戳的要点数组，并删除过时项。"
    )

    response = await llm.ainvoke([
        SystemMessage(content=system),
        HumanMessage(content=prompt),
    ])

    text = response.content.strip()
    if text.startswith("```"):
        text = text.split("\n", 1)[1]
        if text.endswith("```"):
            text = text[:-3]

    try:
        summary = json.loads(text)
        if not isinstance(summary, dict):
            return None
    except json.JSONDecodeError:
        return None

    _normalize_mid_facts_struct(summary, mem_cfg)

    summary["updated_at"] = datetime.utcnow().replace(microsecond=0).isoformat() + "Z"

    ttl_days = mem_cfg.get("mid_ttl_days", 7)
    ttl_seconds = max(1, ttl_days * 24 * 3600)
    max_items = mem_cfg.get("mid_max_items", 8)

    key = _mid_summary_key(user_id)
    pipe = _r().pipeline()
    pipe.rpush(key, json.dumps(summary, ensure_ascii=False))
    if max_items > 0:
        pipe.ltrim(key, -max_items, -1)
    pipe.expire(key, ttl_seconds)
    await pipe.execute()
    return summary


# ============================================================
# Session 管理
# ============================================================

def _session_key(user_id: int) -> str:
    return f"agent:session:{user_id}"


async def get_session(user_id: int) -> dict | None:
    """获取用户当前 session 信息。不存在或已过期返回 None。"""
    raw = await _r().get(_session_key(user_id))
    if not raw:
        return None
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return None


async def create_session(user_id: int, idle_timeout_minutes: int) -> dict:
    """创建新 session，写入 Redis 并设置 TTL。返回 session 信息。"""
    import uuid as _uuid
    now_iso = datetime.utcnow().isoformat()
    session = {
        "session_id": str(_uuid.uuid4()),
        "user_id": user_id,
        "created_at": now_iso,
        "last_active": now_iso,
    }
    ttl = max(60, idle_timeout_minutes * 60)
    await _r().set(_session_key(user_id), json.dumps(session, ensure_ascii=False), ex=ttl)
    return session


async def refresh_session(user_id: int, idle_timeout_minutes: int) -> dict:
    """续期 session TTL。session 不存在则创建新 session。"""
    session = await get_session(user_id)
    now_iso = datetime.utcnow().isoformat()
    if session is None:
        return await create_session(user_id, idle_timeout_minutes)
    session["last_active"] = now_iso
    ttl = max(60, idle_timeout_minutes * 60)
    await _r().set(_session_key(user_id), json.dumps(session, ensure_ascii=False), ex=ttl)
    return session


async def clear_session(user_id: int) -> None:
    """删除用户 session（session-end 时调用）。"""
    await _r().delete(_session_key(user_id))


async def clear_short_messages(user_id: int, session_id: str | None = None) -> None:
    """清空本机短期对话；指定 session_id 时只删该会话桶，否则删用户全部 short 相关键（含旧版扁平键）。"""
    if user_id <= 0:
        return
    if session_id and session_id.strip():
        sid = session_id.strip()
        await _r().delete(_short_list_key(user_id, sid))
        await _r().delete(_short_state_key(user_id, sid))
        return
    r = _r()
    pipe = r.pipeline()
    pipe.delete(f"agent:short:{user_id}:list")
    pipe.delete(f"agent:short:{user_id}:state")
    await pipe.execute()
    async for key in r.scan_iter(match=f"agent:short:{user_id}:*"):
        await r.delete(key)
