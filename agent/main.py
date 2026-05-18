"""
GoBlog Agent HTTP：改写 → 路由 →（编排开启时）分析₁→检索→计划→分析₂→执行。
编排开启且 resolution=proceed 时，必须完整跑完检索/计划/收口再进执行器；追问/拒绝为有意短路。
启动: uvicorn main:app --host 0.0.0.0 --port 9091
"""

import json
import os
import traceback
import time

from typing import Any, Literal

from fastapi import FastAPI, Header, HTTPException
from fastapi.responses import Response, StreamingResponse
from pydantic import BaseModel, Field

from agent_core import _route, build_agent
from agent_metrics import CHAT_REQUESTS
from memory_turn import persist_chat_turn
from planning_round import (
    analysis_pre_resolution,
    build_round_system_message,
    early_reply_from_analysis_pre,
    planning_round_enabled,
    run_analysis_close,
    run_analysis_pre,
    run_plan,
)
from reflection import (
    commit_long_term_on_session_end,
    retrieve_long_term_for_turn,
    schedule_async_self_reflection,
)
from rag import rewrite_query
from config import get_prompts, load_config
from agent_log import get_logger
from memory_manager import init_memory_services, ingest_document_chunk
from memory_store import (
    clear_session,
    clear_short_messages,
    get_mid_summaries,
    get_short_messages,
    init_redis,
    redis_ping,
    refresh_session,
)


# ============================================================
# lifespan：启动时初始化 Redis 连接
# ============================================================
from contextlib import asynccontextmanager


@asynccontextmanager
async def lifespan(app: FastAPI):
    await init_redis()
    await init_memory_services()
    get_prompts()
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
    request_id: str = ""
    conversation_history: list[dict] | None = None
    history_owned_by_gateway: bool = False
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


@app.get("/readyz")
async def readyz():
    if await redis_ping():
        return {"status": "ready"}
    raise HTTPException(status_code=503, detail="redis unreachable")


@app.get("/metrics")
async def metrics():
    try:
        from prometheus_client import CONTENT_TYPE_LATEST, generate_latest

        return Response(generate_latest(), media_type=CONTENT_TYPE_LATEST)
    except ImportError:
        raise HTTPException(status_code=501, detail="prometheus_client not installed") from None


@app.post("/chat")
async def chat(
    req: AgentRequest,
    x_request_id: str | None = Header(None, alias="X-Request-Id"),
):
    """SSE：含分段耗时、request_id；编排见 planning_round。"""
    async def event_stream():
        rid = (x_request_id or "").strip() or (req.request_id or "").strip()
        wall_start = time.time()
        use_planning_round = planning_round_enabled()
        planning_cycle_complete = not use_planning_round
        res_pre = "proceed"
        timings: dict[str, Any] = {}
        analysis_pre_ms = 0
        try:
            cfg = load_config()
            mem_cfg = cfg.get("memory", {})
            short_limit = mem_cfg.get("short_window_messages", 12)
            short_ttl_minutes = mem_cfg.get("short_ttl_minutes", 60)
            mid_limit = mem_cfg.get("mid_max_items", 8)
            mongo_retrieval_limit = mem_cfg.get("mongo_retrieval_limit", 10)
            qdrant_retrieval_limit = mem_cfg.get("long_term_retrieval_limit", 6)
            session_idle = mem_cfg.get("session_idle_timeout_minutes", 30)

            extra_log = {"user_id": req.user_id}
            if rid:
                extra_log["request_id"] = rid
            _log.info("chat start", extra=extra_log)

            session = await refresh_session(req.user_id, session_idle)
            yield _sse({"type": "session", "session_id": session["session_id"], "request_id": rid})

            t0 = time.time()
            rewritten = await rewrite_query(req.message)
            rewrite_ms = int((time.time() - t0) * 1000)
            timings["rewrite_ms"] = rewrite_ms

            yield _sse(
                {
                    "type": "rewrite",
                    "original": req.message,
                    "rewritten": rewritten,
                    "rewrite_ms": rewrite_ms,
                    "request_id": rid,
                }
            )

            t1 = time.time()
            has_token = bool(req.token)
            servers = await _route(req.message, has_token)
            route_ms = int((time.time() - t1) * 1000)
            timings["route_ms"] = route_ms

            yield _sse({"type": "route", "servers": servers, "route_ms": route_ms, "request_id": rid})

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

            messages: list[dict] = []
            if mid_summaries:
                summary_json = json.dumps(mid_summaries, ensure_ascii=False)
                messages.append({"role": "system", "content": f"中期记忆(JSON 列表): {summary_json}"})

            long_term_context: list = []

            if use_planning_round:
                analysis_pre, analysis_pre_ms = await run_analysis_pre(
                    user_message=req.message,
                    rewritten=rewritten or req.message,
                    servers=servers,
                    recent_messages=recent_messages,
                    mid_summaries=mid_summaries,
                )
                timings["analysis_pre_ms"] = analysis_pre_ms
                res_pre = analysis_pre_resolution(analysis_pre)
                yield _sse(
                    {
                        "type": "analysis_pre",
                        "analysis_pre_ms": analysis_pre_ms,
                        "intent": (analysis_pre.get("intent") or "")[:500],
                        "resolution": res_pre,
                        "candidate_tool_codes": analysis_pre.get("candidate_tool_codes") or [],
                        "request_id": rid,
                    }
                )

                early_text = early_reply_from_analysis_pre(analysis_pre)
                if res_pre in ("clarify", "cannot") and early_text:
                    for i in range(0, len(early_text), 48):
                        yield _sse({"type": "token", "content": early_text[i : i + 48]})
                    await persist_chat_turn(
                        user_id=req.user_id,
                        user_message=req.message,
                        assistant_text=early_text,
                        short_ttl_minutes=short_ttl_minutes,
                        short_limit=short_limit,
                        recent_limit=recent_limit,
                        chat_session_id=csid,
                        mid_summaries=mid_summaries,
                        recent_messages=recent_messages,
                        request_id=rid,
                    )
                    timings["total_ms"] = int((time.time() - wall_start) * 1000)
                    schedule_async_self_reflection(
                        user_id=req.user_id,
                        request_id=rid,
                        user_message=req.message,
                        assistant_text=early_text,
                        chat_session_id=csid,
                        early_exit=res_pre,
                        planning_cycle_complete=False,
                        timings=timings,
                    )
                    CHAT_REQUESTS.labels(f"early_{res_pre}").inc()
                    yield _sse(
                        {
                            "type": "done",
                            "early_exit": res_pre,
                            "planning_cycle_complete": False,
                            "request_id": rid,
                            "timings": timings,
                        }
                    )
                    return
                if res_pre in ("clarify", "cannot") and not early_text:
                    _log.warning(
                        "首轮分析 resolution=%s 但缺少追问/原因正文，降级为 proceed",
                        res_pre,
                        extra=extra_log,
                    )
                    res_pre = "proceed"

                t_r = time.time()
                long_term_context = await retrieve_long_term_for_turn(
                    req.user_id,
                    rewritten or req.message,
                    mongo_limit=mongo_retrieval_limit,
                    qdrant_limit=qdrant_retrieval_limit,
                )
                retrieve_ms = int((time.time() - t_r) * 1000)
                timings["retrieve_ms"] = retrieve_ms
                yield _sse(
                    {
                        "type": "retrieve",
                        "retrieve_ms": retrieve_ms,
                        "hits": len(long_term_context),
                        "request_id": rid,
                    }
                )

                plan, plan_ms = await run_plan(
                    analysis_pre=analysis_pre,
                    user_message=req.message,
                    rewritten=rewritten or req.message,
                    servers=servers,
                    long_term_context=long_term_context,
                )
                timings["plan_ms"] = plan_ms
                yield _sse(
                    {
                        "type": "plan",
                        "plan_ms": plan_ms,
                        "requires_tools": bool(plan.get("requires_tools", True)),
                        "plan_steps": len(plan.get("plan_steps") or []),
                        "request_id": rid,
                    }
                )

                analysis_close, analysis_close_ms = await run_analysis_close(
                    plan=plan,
                    user_message=req.message,
                    long_term_context=long_term_context,
                )
                timings["analysis_close_ms"] = analysis_close_ms
                yield _sse(
                    {
                        "type": "analysis_close",
                        "analysis_close_ms": analysis_close_ms,
                        "briefing_preview": (analysis_close.get("final_briefing") or "")[:240],
                        "request_id": rid,
                    }
                )

                planning_cycle_complete = True
                round_sys = build_round_system_message(
                    analysis_pre=analysis_pre,
                    plan=plan,
                    close=analysis_close,
                )
                if round_sys.strip():
                    messages.append({"role": "system", "content": round_sys})
            else:
                t_r = time.time()
                long_term_context = await retrieve_long_term_for_turn(
                    req.user_id,
                    rewritten or req.message,
                    mongo_limit=mongo_retrieval_limit,
                    qdrant_limit=qdrant_retrieval_limit,
                )
                retrieve_ms = int((time.time() - t_r) * 1000)
                timings["retrieve_ms"] = retrieve_ms
                yield _sse(
                    {
                        "type": "retrieve",
                        "retrieve_ms": retrieve_ms,
                        "hits": len(long_term_context),
                        "request_id": rid,
                    }
                )

            if use_planning_round and res_pre == "proceed" and not planning_cycle_complete:
                _ERROR_LOG.error(
                    "编排违反：resolution=proceed 时必须在执行前完成 检索→计划→分析收口",
                    extra=extra_log,
                )

            if long_term_context:
                long_term_json = json.dumps(long_term_context, ensure_ascii=False)
                messages.append({"role": "system", "content": f"长期记忆(JSON 列表): {long_term_json}"})

            messages.extend(recent_messages)
            messages.append(user_msg)

            agent_config = {"configurable": {"thread_id": str(req.user_id), "token": req.token}}
            assistant_chunks = []

            t_ex = time.time()
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
                    yield _sse({"type": "tool_start", "tool": tool_name, "request_id": rid})

                elif kind == "on_tool_end":
                    output = str(event.get("data", {}).get("output", ""))
                    preview = output[:100] + ("..." if len(output) > 100 else "")
                    yield _sse({"type": "tool_end", "preview": preview, "request_id": rid})

            timings["execute_ms"] = int((time.time() - t_ex) * 1000)
            timings["total_ms"] = int((time.time() - wall_start) * 1000)

            assistant_text = "".join(assistant_chunks).strip()
            if assistant_text:
                await persist_chat_turn(
                    user_id=req.user_id,
                    user_message=req.message,
                    assistant_text=assistant_text,
                    short_ttl_minutes=short_ttl_minutes,
                    short_limit=short_limit,
                    recent_limit=recent_limit,
                    chat_session_id=csid,
                    mid_summaries=mid_summaries,
                    recent_messages=recent_messages,
                    request_id=rid,
                )

            schedule_async_self_reflection(
                user_id=req.user_id,
                request_id=rid,
                user_message=req.message,
                assistant_text=assistant_text,
                chat_session_id=csid,
                early_exit=None,
                planning_cycle_complete=planning_cycle_complete,
                timings=timings,
            )
            CHAT_REQUESTS.labels("ok").inc()
            yield _sse(
                {
                    "type": "done",
                    "planning_cycle_complete": planning_cycle_complete,
                    "request_id": rid,
                    "timings": timings,
                }
            )

        except Exception:
            CHAT_REQUESTS.labels("error").inc()
            _ERROR_LOG.error(
                "chat 异常",
                extra={"user_id": req.user_id, "request_id": rid, "traceback": traceback.format_exc()},
            )
            yield _sse({"type": "error", "content": f"内部错误: {traceback.format_exc()}", "request_id": rid})

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
    result = await commit_long_term_on_session_end(req.user_id, reason=req.reason)
    await clear_session(req.user_id)
    return {"status": "ok", "result": result}


def _sse(data: dict) -> str:
    return f"data: {json.dumps(data, ensure_ascii=False)}\n\n"


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=9091)
