"""
GoBlog Agent HTTP 服务（三阶段：改写 → 路由选组 → 执行）
启动: uvicorn main:app --host 0.0.0.0 --port 9091
"""
import json
import os
import traceback
import time

from typing import Literal

from fastapi import FastAPI, Header, HTTPException
from fastapi.responses import StreamingResponse
from pydantic import BaseModel, Field

from agent_core import _route, build_agent
from rag import rewrite_query
from config import load_config
from agent_log import get_logger
from memory_manager import get_long_term_context, init_memory_services, ingest_document_chunk, maybe_commit_long_term
from memory_store import (
    append_short_messages,
    clear_session,
    clear_short_messages,
    get_mid_summaries,
    get_short_messages,
    init_redis,
    pop_short_messages,
    refresh_session,
    update_mid_summary,
)


# ============================================================
# lifespan：启动时初始化 Redis 连接
# ============================================================
from contextlib import asynccontextmanager


@asynccontextmanager
async def lifespan(app: FastAPI):
    await init_redis()
    await init_memory_services()
    _log.info("Agent 服务启动完成")
    yield
    _log.info("Agent 服务关闭")


app = FastAPI(title="GoBlog Agent", version="0.4.0", lifespan=lifespan)
_log = get_logger("main")
_ERROR_LOG = get_logger("agent_error")


class AgentRequest(BaseModel):
    message: str
    user_id: int = 0
    token: str = ""
    # 由 Go 网关注入：博客 Redis 中的短期对话；为真时不在本进程 Redis 读写短期列表
    conversation_history: list[dict] | None = None
    history_owned_by_gateway: bool = False
    # 网关侧「对话线程」id（与 SSE 里 refresh_session 的 session_id 无关）
    chat_session_id: str = ""


class MemoryCommitRequest(BaseModel):
    user_id: int
    reason: str = "session_end"


class ClearShortRequest(BaseModel):
    user_id: int = Field(..., ge=1)
    session_id: str | None = Field(None, description="只清空该对话桶；为空则清空用户全部本机 short")


class DocumentIngestRequest(BaseModel):
    """读入文档块写入全局长期向量库（与对话抽取记忆区分）。"""

    article_id: str = Field(..., min_length=1)
    chunk_index: int = Field(..., ge=0)
    content: str = Field(..., min_length=1)
    content_revision: int = Field(1, ge=1)
    source: str = Field(..., min_length=1, description="来源标识或 URL，写入 payload.source")
    ingest_kind: Literal["raw_chunk", "summary"] = "raw_chunk"
    game_name: str | None = None
    section_path: list[str] | None = None
    preview: str | None = Field(None, description="可选；默认取 content 前 240 字")


@app.get("/health")
async def health():
    return {"status": "ok"}


@app.post("/chat")
async def chat(req: AgentRequest):
    """三阶段 Agent：改写 → 路由 → 执行，SSE 流式返回。"""
    async def event_stream():
        try:
            cfg = load_config()
            mem_cfg = cfg.get("memory", {})
            short_limit = mem_cfg.get("short_window_messages", 12)
            short_ttl_minutes = mem_cfg.get("short_ttl_minutes", 60)
            mid_limit = mem_cfg.get("mid_max_items", 8)
            mongo_retrieval_limit = mem_cfg.get("mongo_retrieval_limit", 10)
            qdrant_retrieval_limit = mem_cfg.get("long_term_retrieval_limit", 6)
            session_idle = mem_cfg.get("session_idle_timeout_minutes", 30)

            # Session 检查：过期自动 commit，然后创建/续期新 session
            session = await refresh_session(req.user_id, session_idle)
            yield _sse({"type": "session", "session_id": session["session_id"]})

            # ---------- 阶段 0：query 改写 ----------
            t0 = time.time()
            rewritten = await rewrite_query(req.message)
            rewrite_ms = int((time.time() - t0) * 1000)

            yield _sse({
                "type": "rewrite",
                "original": req.message,
                "rewritten": rewritten,
                "rewrite_ms": rewrite_ms,
            })

            # ---------- 阶段 1：路由 ----------
            t1 = time.time()
            has_token = bool(req.token)
            servers = await _route(req.message, has_token)
            route_ms = int((time.time() - t1) * 1000)

            yield _sse({
                "type": "route",
                "servers": servers,
                "route_ms": route_ms,
            })

            # ---------- 阶段 2：执行（统一处理：直出/追问/工具调用）----------
            agent = build_agent(servers)
            user_msg = {"role": "user", "content": req.message}

            csid = (req.chat_session_id or "").strip() or "default"
            if req.history_owned_by_gateway:
                short_messages = []
                for m in req.conversation_history or []:
                    if not isinstance(m, dict):
                        continue
                    role = m.get("role")
                    content = m.get("content")
                    if role and content is not None:
                        short_messages.append({"role": str(role), "content": str(content)})
            else:
                short_messages = await get_short_messages(req.user_id, short_limit, csid)
            mid_summaries = await get_mid_summaries(req.user_id, mid_limit)
            recent_limit = max(1, short_limit // 2)
            if len(short_messages) < short_limit:
                recent_messages = short_messages
            else:
                recent_messages = short_messages[-recent_limit:]

            messages = []
            if mid_summaries:
                summary_json = json.dumps(mid_summaries, ensure_ascii=False)
                messages.append({"role": "system", "content": f"中期记忆(JSON 列表): {summary_json}"})

            long_term_context = await get_long_term_context(
                req.user_id,
                rewritten or req.message,
                mongo_limit=mongo_retrieval_limit,
                qdrant_limit=qdrant_retrieval_limit,
            )
            if long_term_context:
                long_term_json = json.dumps(long_term_context, ensure_ascii=False)
                messages.append({"role": "system", "content": f"长期记忆(JSON 列表): {long_term_json}"})

            messages.extend(recent_messages)
            messages.append(user_msg)

            agent_config = {"configurable": {"thread_id": str(req.user_id), "token": req.token}}
            assistant_chunks = []

            async for event in agent.astream_events(
                {"messages": messages},
                config=agent_config,
                version="v2",
            ):
                kind = event.get("event", "")

                if kind == "on_chat_model_stream":
                    chunk = event.get("data", {}).get("chunk")
                    if chunk and chunk.content:
                        assistant_chunks.append(chunk.content)
                        yield _sse({"type": "token", "content": chunk.content})

                elif kind == "on_tool_start":
                    tool_name = event.get("name", "unknown")
                    yield _sse({"type": "tool_start", "tool": tool_name})

                elif kind == "on_tool_end":
                    output = str(event.get("data", {}).get("output", ""))
                    preview = output[:100] + ("..." if len(output) > 100 else "")
                    yield _sse({"type": "tool_end", "preview": preview})

            assistant_text = "".join(assistant_chunks).strip()
            if assistant_text:
                prev_summary = mid_summaries[-1] if mid_summaries else None
                # 双写：网关把短期存在博客 Redis（goblog:agent:chat:*），此处仍写 Agent 本机 Redis（agent:short:*），
                # 两套键名并存，供中期滚动摘要、WS 等与「本机 short」相关的逻辑使用。
                await append_short_messages(
                    req.user_id,
                    [
                        {"role": "user", "content": req.message},
                        {"role": "assistant", "content": assistant_text},
                    ],
                    short_ttl_minutes,
                    csid,
                )
                current_short_messages = await get_short_messages(req.user_id, short_limit, csid)
                if len(current_short_messages) >= short_limit:
                    summary_chunk = current_short_messages[:recent_limit]
                    new_summary = await update_mid_summary(req.user_id, prev_summary, summary_chunk)
                    if new_summary is not None:
                        await pop_short_messages(req.user_id, recent_limit, csid)

                await maybe_commit_long_term(
                    req.user_id,
                    reason="auto",
                    user_message=req.message,
                    assistant_text=assistant_text,
                    recent_messages=recent_messages,
                    mid_summaries=mid_summaries,
                )

            yield _sse({"type": "done"})

        except Exception:
            _ERROR_LOG.error("chat 异常", extra={"user_id": req.user_id, "traceback": traceback.format_exc()})
            yield _sse({"type": "error", "content": f"内部错误: {traceback.format_exc()}"})

    return StreamingResponse(
        event_stream(),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
    )


@app.post("/memory/ingest-document")
async def memory_ingest_document(
    req: DocumentIngestRequest,
    x_ingest_key: str | None = Header(None, alias="X-Ingest-Key"),
):
    """将 Wiki/百科等文档块写入 Qdrant（memory_scope=document）。配置 document_ingest_secret 非空时需校验 X-Ingest-Key。"""
    cfg = load_config()
    secret = (cfg.get("document_ingest_secret") or "").strip()
    if secret and (not x_ingest_key or x_ingest_key.strip() != secret):
        raise HTTPException(status_code=401, detail="invalid or missing X-Ingest-Key")

    try:
        result = await ingest_document_chunk(
            req.article_id,
            req.chunk_index,
            req.content,
            content_revision=req.content_revision,
            source=req.source,
            ingest_kind=req.ingest_kind,
            game_name=req.game_name,
            section_path=req.section_path,
            preview=req.preview,
        )
    except ValueError as e:
        raise HTTPException(status_code=400, detail=str(e)) from e
    except RuntimeError as e:
        raise HTTPException(status_code=503, detail=str(e)) from e

    return {"status": "ok", **result}


@app.post("/memory/clear-short")
async def memory_clear_short(
    req: ClearShortRequest,
    x_agent_secret: str | None = Header(None, alias="X-Agent-Secret"),
):
    """供 Go 网关在「清空会话」时同步清空 agent:short:*；可选 AGENT_INTERNAL_SECRET 鉴权。"""
    secret = (os.getenv("AGENT_INTERNAL_SECRET") or "").strip()
    if secret and (not x_agent_secret or x_agent_secret.strip() != secret):
        raise HTTPException(status_code=401, detail="unauthorized")
    sid = (req.session_id or "").strip() or None
    await clear_short_messages(req.user_id, sid)
    if sid is None:
        await clear_session(req.user_id)
    return {"status": "ok"}


@app.post("/memory/session-end")
async def memory_session_end(req: MemoryCommitRequest):
    """由前端/后端在会话结束、WS 关闭或超时后显式触发长期记忆收尾。同时清理 session。"""
    result = await maybe_commit_long_term(req.user_id, reason=req.reason)
    await clear_session(req.user_id)
    return {"status": "ok", "result": result}


def _sse(data: dict) -> str:
    return f"data: {json.dumps(data, ensure_ascii=False)}\n\n"


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=9091)
