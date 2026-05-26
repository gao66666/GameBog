"""
GoBlog Agent HTTP：改写 → 路由 → 工具检索 → analyse↔(plan→execute) → output 成稿。
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

from core.agent_core import _route, build_agent, resolve_tools_for_execution, stream_tool_execution
from infra.agent_metrics import CHAT_REQUESTS
from memory.memory_turn import persist_chat_turn
from core.planning_round import (
    EXECUTE_DISPOSITIONS,
    TERMINAL_DISPOSITIONS,
    build_execute_messages,
    max_orchestration_cycles,
    normalize_disposition,
    run_output,
    run_analyse,
    run_plan,
)
from core.reflection import (
    commit_long_term_on_session_end,
    retrieve_long_term_for_turn,
    schedule_post_turn_memory,
)
from core.rag import rewrite_query
from core.token_usage import get_turn_usage_tracker, reset_turn_usage_tracker
from infra.config import get_prompts, load_config
from infra.agent_log import get_logger
from memory.memory_manager import init_memory_services, ingest_document_chunk
from memory.memory_store import (
    clear_session,
    clear_short_messages,
    get_mid_summaries,
    get_short_messages,
    init_redis,
    redis_ping,
    refresh_session,
)
from core.stream_emitter import StreamEmitter
from tools.tool_retrieval import retrieve_tools_for_turn


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
    chat_session_id: str = Field("", description="可选；指定则只 flush 该对话桶，否则 flush 用户全部 short 桶")


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
        reset_turn_usage_tracker()
        wall_start = time.time()
        planning_cycle_complete = False
        timings: dict[str, Any] = {}
        retrieved_tool_names: list[str] = []
        tool_retrieve_mode = ""
        code_to_name: dict[str, str] = {}
        analyse: dict[str, Any] = {}
        plan: dict[str, Any] = {}
        all_tool_results: list[dict] = []
        disposition = "proceed"
        em: StreamEmitter | None = None
        try:
            cfg = load_config()
            mem_cfg = cfg.get("memory", {})
            short_limit = mem_cfg.get("short_window_messages", 12)
            short_ttl_minutes = mem_cfg.get("short_ttl_minutes", 60)
            mid_limit = mem_cfg.get("mid_max_items", 8)
            mongo_retrieval_limit = mem_cfg.get(
                "mongo_rule_preference_limit", mem_cfg.get("mongo_retrieval_limit", 5)
            )
            qdrant_retrieval_limit = mem_cfg.get("long_term_retrieval_limit", 10)
            session_idle = mem_cfg.get("session_idle_timeout_minutes", 30)

            extra_log = {"user_id": req.user_id}
            if rid:
                extra_log["request_id"] = rid
            _log.info("chat start", extra=extra_log)

            csid = (req.chat_session_id or "").strip() or "default"
            em = StreamEmitter(rid, chat_session_id=csid, legacy_mirror=False)

            session = await refresh_session(req.user_id, session_idle)
            for line in em.emit_session(session["session_id"]):
                yield line

            t0 = time.time()
            rewritten = await rewrite_query(req.message)
            rewrite_ms = int((time.time() - t0) * 1000)
            timings["rewrite_ms"] = rewrite_ms

            for line in em.emit_rewrite(req.message, rewritten, rewrite_ms):
                yield line

            t1 = time.time()
            has_token = bool(req.token)
            route_result = await _route(req.message, has_token, rewritten=rewritten)
            servers = route_result.servers
            tool_hints = route_result.tool_hints
            memory_hints = route_result.memory_hints
            route_ms = int((time.time() - t1) * 1000)
            timings["route_ms"] = route_ms

            for line in em.emit_route(
                req.message, servers, has_token, route_ms,
                tool_hints=tool_hints,
                memory_hints=memory_hints,
            ):
                yield line

            t_tr = time.time()
            retrieved_tool_names, tool_retrieve_mode = await retrieve_tools_for_turn(
                rewritten or req.message,
                servers,
                tool_hints=tool_hints,
            )
            tool_retrieve_ms = int((time.time() - t_tr) * 1000)
            timings["tool_retrieve_ms"] = tool_retrieve_ms
            for line in em.emit_tool_retrieve(
                rewritten or req.message,
                servers,
                retrieved_tool_names,
                tool_retrieve_mode,
                tool_retrieve_ms,
                tool_hints=tool_hints,
            ):
                yield line

            user_msg = {"role": "user", "content": req.message}
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

            long_term_context: list = []
            assistant_text = ""
            output_disposition = disposition
            force_output_note = ""

            long_term_fetched = False
            executed_once = False
            prior_analyse: dict[str, Any] = {}
            max_cycles = max_orchestration_cycles()
            orch_cycle = 0
            terminal = False

            while orch_cycle < max_cycles and not terminal:
                phase = "post" if executed_once else "pre"
                t_a = time.time()
                analyse, analyse_ms, code_to_name = await run_analyse(
                    phase=phase,
                    user_message=req.message,
                    rewritten=rewritten or req.message,
                    servers=servers,
                    retrieved_tool_names=retrieved_tool_names,
                    recent_messages=recent_messages,
                    mid_summaries=mid_summaries,
                    tool_results=all_tool_results if phase == "post" else None,
                    prior_analyse=prior_analyse if phase == "post" else None,
                    plan=plan if phase == "post" else None,
                    cycle=orch_cycle + 1,
                )
                disposition = normalize_disposition(analyse)
                key = "analyse_post_ms" if phase == "post" else "analyse_pre_ms"
                timings[key] = analyse_ms
                for line in em.emit_analyse(
                    req.message,
                    analyse,
                    disposition,
                    analyse_ms,
                    phase=phase,
                    cycle=orch_cycle + 1,
                ):
                    yield line

                if disposition in TERMINAL_DISPOSITIONS:
                    output_disposition = disposition
                    terminal = True
                    break

                if disposition not in EXECUTE_DISPOSITIONS:
                    _log.warning("未知 disposition=%s，按 proceed 处理", disposition)
                    disposition = "proceed"

                if not long_term_fetched:
                    t_r = time.time()
                    long_term_context = await retrieve_long_term_for_turn(
                        req.user_id,
                        rewritten or req.message,
                        mongo_limit=mongo_retrieval_limit,
                        qdrant_limit=qdrant_retrieval_limit,
                        memory_hints=memory_hints,
                    )
                    retrieve_ms = int((time.time() - t_r) * 1000)
                    timings["retrieve_ms"] = retrieve_ms
                    for line in em.emit_retrieve(
                        rewritten or req.message,
                        len(long_term_context),
                        retrieve_ms,
                        memory_hints=memory_hints,
                        long_term_hits=long_term_context,
                    ):
                        yield line
                    long_term_fetched = True

                plan, plan_ms = await run_plan(
                    analyse=analyse,
                    user_message=req.message,
                    rewritten=rewritten or req.message,
                    servers=servers,
                    retrieved_tool_names=retrieved_tool_names,
                    code_to_name=code_to_name,
                    long_term_context=long_term_context,
                )
                timings["plan_ms"] = timings.get("plan_ms", 0) + plan_ms
                for line in em.emit_plan(plan, plan_ms):
                    yield line

                if not plan.get("requires_tools", True):
                    output_disposition = "close"
                    terminal = True
                    break

                exec_tool_names = resolve_tools_for_execution(
                    servers,
                    analyse=analyse,
                    plan=plan,
                    retrieved_tool_names=retrieved_tool_names,
                    code_to_name=code_to_name,
                )
                agent = build_agent(
                    servers,
                    tool_names=exec_tool_names,
                    execute_mode=True,
                )
                exec_messages = build_execute_messages(
                    plan=plan,
                    analyse=analyse,
                    long_term_context=long_term_context,
                    recent_messages=recent_messages,
                    user_msg=user_msg,
                )
                agent_config = {
                    "configurable": {"thread_id": str(req.user_id), "token": req.token}
                }
                batch_results: list[dict] = []
                t_ex = time.time()
                async for kind, payload in stream_tool_execution(
                    agent, exec_messages, agent_config, em
                ):
                    if kind == "line":
                        yield payload
                    elif kind == "result":
                        batch_results = payload
                timings["execute_ms"] = timings.get("execute_ms", 0) + int(
                    (time.time() - t_ex) * 1000
                )
                all_tool_results.extend(batch_results)
                prior_analyse = analyse
                executed_once = True
                orch_cycle += 1

            if not terminal:
                output_disposition = "close"
                force_output_note = (
                    f"已达最大编排轮次 {max_cycles}，请基于已有工具结果谨慎成稿。"
                )

            final_reply, output_ms = await run_output(
                disposition=output_disposition,
                analyse=analyse,
                plan=plan,
                tool_results=all_tool_results,
                user_message=req.message,
                long_term_context=long_term_context,
                force_note=force_output_note,
            )
            timings["output_ms"] = output_ms
            for line in em.emit_output(final_reply[:240], output_ms):
                yield line

            for i in range(0, len(final_reply), 48):
                for line in em.emit_chunk(final_reply[i : i + 48], channel="answer"):
                    yield line
            assistant_text = final_reply.strip()
            planning_cycle_complete = True

            timings["total_ms"] = int((time.time() - wall_start) * 1000)

            mid_summary_rolled = False
            if assistant_text:
                mid_summary_rolled = await persist_chat_turn(
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

            early_exit = (
                output_disposition
                if output_disposition in ("clarify", "cannot", "answer")
                else None
            )
            if mid_summary_rolled:
                schedule_post_turn_memory(
                    user_id=req.user_id,
                    request_id=rid,
                    user_message=req.message,
                    assistant_text=assistant_text,
                    chat_session_id=csid,
                    early_exit=early_exit,
                    planning_cycle_complete=planning_cycle_complete,
                    timings=timings,
                )
            CHAT_REQUESTS.labels("ok").inc()
            usage_tracker = get_turn_usage_tracker()
            usage = usage_tracker.to_dict() if usage_tracker else {}
            for line in em.emit_done(
                timings=timings,
                planning_cycle_complete=planning_cycle_complete,
                early_exit=early_exit,
                usage=usage or None,
            ):
                yield line

        except Exception:
            CHAT_REQUESTS.labels("error").inc()
            _ERROR_LOG.exception(
                "chat 异常",
                extra={"user_id": req.user_id, "request_id": rid},
            )
            err_em = em if em is not None else StreamEmitter(rid, legacy_mirror=False)
            for line in err_em.emit_error("服务暂时不可用，请稍后再试", code="INTERNAL"):
                yield line

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
    """会话结束/超时/WS 关闭：剩余短期压中期摘要 → 长期记忆收尾 → 清 session。"""
    sid = (req.chat_session_id or "").strip() or None
    result = await commit_long_term_on_session_end(
        req.user_id, reason=req.reason, chat_session_id=sid
    )
    await clear_session(req.user_id)
    return {"status": "ok", "result": result}


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=9091)
