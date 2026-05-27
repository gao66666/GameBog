"""
Structured SSE fact 层：统一 Envelope 封装，可选镜像旧版 type 事件（过渡期兼容 Go 落库与前端）。
契约见 shared/stream_schema.json。
"""

from __future__ import annotations

import ast
import json
import time
from typing import Any, Iterator

from tools.tool_runtime import summarize_args

PROTOCOL_VERSION = 1

_LIST_COUNT_KEYS = (
    "articles",
    "article_list",
    "topics",
    "topic_list",
    "games",
    "game_list",
    "comments",
    "comment_list",
    "reviews",
    "review_list",
    "collections",
    "discussions",
    "discussion_list",
    "game_plays",
    "orders",
    "transactions",
)


def coerce_tool_output_text(raw: Any) -> str:
    """LangChain on_tool_end 的 output 可能是 str / dict / list。"""
    if raw is None:
        return ""
    if isinstance(raw, str):
        return raw
    if isinstance(raw, (dict, list)):
        return json.dumps(raw, ensure_ascii=False, default=str)
    return str(raw)


def _parse_tool_result_obj(stripped: str) -> dict[str, Any] | None:
    if not stripped:
        return None
    if stripped.startswith("{"):
        try:
            obj = json.loads(stripped)
            return obj if isinstance(obj, dict) else None
        except json.JSONDecodeError:
            pass
        try:
            obj = ast.literal_eval(stripped)
            return obj if isinstance(obj, dict) else None
        except (SyntaxError, ValueError):
            return None
    return None


def _count_rows(obj: dict[str, Any]) -> int | None:
    for key in _LIST_COUNT_KEYS:
        items = obj.get(key)
        if isinstance(items, list):
            return len(items)
    total = obj.get("total")
    if isinstance(total, bool):
        return None
    if isinstance(total, (int, float)):
        return int(total)
    return None


def parse_tool_output(output: str | dict | list | Any) -> dict[str, Any]:
    """从工具返回推断 ok / row_count / preview（供 tool_invoke end payload）。"""
    text = coerce_tool_output_text(output)
    preview = text[:100] + ("..." if len(text) > 100 else "")
    ok = True
    error_code: str | None = None
    row_count: int | None = None

    obj = _parse_tool_result_obj(text.strip())
    if isinstance(obj, dict):
        if obj.get("_error"):
            ok = False
            error_code = "upstream_error"
        else:
            row_count = _count_rows(obj)
    elif any(x in text for x in ("失败", "未登录", "不存在", "错误", "未知工具")):
        ok = False
        error_code = "tool_error"

    out: dict[str, Any] = {"ok": ok, "preview": preview}
    if error_code:
        out["error_code"] = error_code
    if row_count is not None:
        out["row_count"] = row_count
    return out


def _sse_line(data: dict) -> str:
    return f"data: {json.dumps(data, ensure_ascii=False)}\n\n"


def _long_term_memory_refs(hits: list[dict[str, Any]] | None) -> list[dict[str, Any]]:
    """长期记忆召回引用（仅 store/id/kind/score，不含 content）。"""
    refs: list[dict[str, Any]] = []
    for h in hits or []:
        if not isinstance(h, dict):
            continue
        store = str(h.get("store", "")).strip()
        mid = str(h.get("memory_id", "") or h.get("fingerprint", "") or h.get("point_id", "")).strip()
        if not store or not mid:
            continue
        ref: dict[str, Any] = {"store": store, "id": mid}
        kind = str(h.get("kind", "")).strip()
        if kind:
            ref["kind"] = kind
        if h.get("score") is not None:
            try:
                ref["score"] = round(float(h["score"]), 4)
            except (TypeError, ValueError):
                pass
        refs.append(ref)
    return refs


class StreamEmitter:
    """单请求内 seq 单调递增；编排层唯一 SSE fact 出口。"""

    def __init__(
        self,
        request_id: str,
        *,
        chat_session_id: str = "",
        legacy_mirror: bool = True,
    ):
        self.request_id = request_id
        self.chat_session_id = chat_session_id
        self.legacy_mirror = legacy_mirror
        self._seq = 0
        self._tool_counter = 0
        self._tool_stack: list[str] = []
        self._tool_started_at: dict[str, float] = {}

    def _next_seq(self) -> int:
        self._seq += 1
        return self._seq

    def _fact(
        self,
        *,
        kind: str,
        phase: str,
        status: str,
        payload: dict[str, Any],
        duration_ms: int | None = None,
    ) -> dict[str, Any]:
        frame: dict[str, Any] = {
            "v": PROTOCOL_VERSION,
            "kind": kind,
            "seq": self._next_seq(),
            "request_id": self.request_id,
            "ts": int(time.time() * 1000),
            "phase": phase,
            "status": status,
            "payload": payload,
        }
        if duration_ms is not None:
            frame["duration_ms"] = duration_ms
        return frame

    def _yield_fact(self, frame: dict[str, Any], legacy: dict[str, Any] | None = None) -> Iterator[str]:
        yield _sse_line(frame)
        if self.legacy_mirror and legacy is not None:
            yield _sse_line(legacy)

    def emit_session(self, session_id: str) -> Iterator[str]:
        payload: dict[str, Any] = {"session_id": session_id}
        if self.chat_session_id:
            payload["chat_session_id"] = self.chat_session_id
        legacy = {"type": "session", "session_id": session_id, "request_id": self.request_id}
        yield from self._yield_fact(
            self._fact(kind="control", phase="session", status="end", payload=payload),
            legacy,
        )

    def emit_stage_end(
        self,
        phase: str,
        *,
        payload: dict[str, Any],
        duration_ms: int | None = None,
        legacy: dict[str, Any] | None = None,
    ) -> Iterator[str]:
        yield from self._yield_fact(
            self._fact(
                kind="stage",
                phase=phase,
                status="end",
                payload=payload,
                duration_ms=duration_ms,
            ),
            legacy,
        )

    def emit_rewrite(self, message: str, rewritten: str, duration_ms: int) -> Iterator[str]:
        yield from self.emit_stage_end(
            "rewrite",
            duration_ms=duration_ms,
            payload={
                "input": {"message": message},
                "output": {"rewritten": rewritten},
            },
            legacy={
                "type": "rewrite",
                "original": message,
                "rewritten": rewritten,
                "rewrite_ms": duration_ms,
                "request_id": self.request_id,
            },
        )

    def emit_route(
        self,
        message: str,
        servers: list[str],
        has_token: bool,
        duration_ms: int,
        *,
        tool_hints: list[str] | None = None,
        memory_hints: list[str] | None = None,
    ) -> Iterator[str]:
        hints = list(tool_hints or [])
        mem_hints = list(memory_hints or [])
        yield from self.emit_stage_end(
            "route",
            duration_ms=duration_ms,
            payload={
                "input": {"message": message},
                "output": {
                    "servers": servers,
                    "has_token": has_token,
                    "tool_hints": hints,
                    "memory_hints": mem_hints,
                },
            },
            legacy={
                "type": "route",
                "servers": servers,
                "tool_hints": hints,
                "memory_hints": mem_hints,
                "route_ms": duration_ms,
                "request_id": self.request_id,
            },
        )

    def emit_tool_retrieve(
        self,
        query: str,
        servers: list[str],
        tools: list[str],
        mode: str,
        duration_ms: int,
        *,
        tool_hints: list[str] | None = None,
    ) -> Iterator[str]:
        hints = list(tool_hints or [])
        yield from self.emit_stage_end(
            "tool_retrieve",
            duration_ms=duration_ms,
            payload={
                "input": {"query": query, "servers": servers, "tool_hints": hints},
                "output": {"tools": tools, "count": len(tools), "mode": mode},
            },
            legacy={
                "type": "tool_retrieve",
                "tool_retrieve_ms": duration_ms,
                "tools": tools,
                "mode": mode,
                "tool_hints": hints,
                "request_id": self.request_id,
            },
        )

    def emit_analyse(
        self,
        message: str,
        analyse: dict[str, Any],
        disposition: str,
        duration_ms: int,
        *,
        phase: str = "pre",
        cycle: int = 0,
    ) -> Iterator[str]:
        output: dict[str, Any] = {
            "disposition": disposition,
            "intent": (analyse.get("intent") or "")[:500],
            "analysis": (analyse.get("analysis") or "")[:2000],
            "phase": phase,
            "cycle": cycle,
        }
        if analyse.get("replan_reason"):
            output["replan_reason"] = analyse.get("replan_reason")
        yield from self.emit_stage_end(
            "analyse",
            duration_ms=duration_ms,
            payload={"input": {"message": message, "phase": phase}, "output": output},
            legacy={
                "type": "analysis_pre",
                "analysis_pre_ms": duration_ms,
                "intent": output["intent"],
                "resolution": disposition,
                "disposition": disposition,
                "analysis": output["analysis"],
                "candidate_tool_codes": analyse.get("candidate_tool_codes") or [],
                "request_id": self.request_id,
            },
        )

    def emit_analysis_pre(
        self,
        message: str,
        analysis_pre: dict[str, Any],
        resolution: str,
        duration_ms: int,
    ) -> Iterator[str]:
        yield from self.emit_analyse(message, analysis_pre, resolution, duration_ms, phase="pre")

    def emit_retrieve(
        self,
        query: str,
        hits: int,
        duration_ms: int,
        *,
        memory_hints: list[str] | None = None,
        long_term_hits: list[dict[str, Any]] | None = None,
    ) -> Iterator[str]:
        mem_hints = list(memory_hints or [])
        memories = _long_term_memory_refs(long_term_hits)
        yield from self.emit_stage_end(
            "retrieve",
            duration_ms=duration_ms,
            payload={
                "input": {
                    "query": query,
                    "memory_hints": mem_hints,
                },
                "output": {"hits": hits, "memories": memories},
            },
            legacy={
                "type": "retrieve",
                "retrieve_ms": duration_ms,
                "hits": hits,
                "memory_hints": mem_hints,
                "memories": memories,
                "request_id": self.request_id,
            },
        )

    def emit_plan(self, plan: dict[str, Any], duration_ms: int) -> Iterator[str]:
        yield from self.emit_stage_end(
            "plan",
            duration_ms=duration_ms,
            payload={
                "output": {
                    "requires_tools": bool(plan.get("requires_tools", True)),
                    "step_count": len(plan.get("plan_steps") or []),
                }
            },
            legacy={
                "type": "plan",
                "plan_ms": duration_ms,
                "requires_tools": bool(plan.get("requires_tools", True)),
                "plan_steps": len(plan.get("plan_steps") or []),
                "request_id": self.request_id,
            },
        )

    def emit_output(self, reply_preview: str, duration_ms: int) -> Iterator[str]:
        yield from self.emit_stage_end(
            "output",
            duration_ms=duration_ms,
            payload={"output": {"reply_preview": reply_preview[:240]}},
            legacy={
                "type": "output",
                "output_ms": duration_ms,
                "reply_preview": reply_preview[:240],
                "request_id": self.request_id,
            },
        )

    def _new_tool_call_id(self) -> str:
        self._tool_counter += 1
        tc = f"tc_{self._tool_counter}"
        self._tool_stack.append(tc)
        self._tool_started_at[tc] = time.time()
        return tc

    def emit_tool_start(self, tool_name: str, raw_args: dict[str, Any] | None) -> Iterator[str]:
        tool_call_id = self._new_tool_call_id()
        args = raw_args if isinstance(raw_args, dict) else {}
        args_summary = summarize_args(tool_name, args)
        payload = {
            "tool_call_id": tool_call_id,
            "input": {"tool": tool_name, "args_summary": args_summary},
        }
        legacy = {"type": "tool_start", "tool": tool_name, "request_id": self.request_id}
        yield from self._yield_fact(
            self._fact(kind="stage", phase="tool_invoke", status="start", payload=payload),
            legacy,
        )

    def emit_tool_end(self, tool_name: str, raw_args: dict[str, Any] | None, output: str) -> Iterator[str]:
        tool_call_id = self._tool_stack.pop() if self._tool_stack else f"tc_{self._tool_counter}"
        started = self._tool_started_at.pop(tool_call_id, None)
        duration_ms = int((time.time() - started) * 1000) if started else None
        args = raw_args if isinstance(raw_args, dict) else {}
        args_summary = summarize_args(tool_name, args)
        parsed = parse_tool_output(output)
        payload: dict[str, Any] = {
            "tool_call_id": tool_call_id,
            "input": {"tool": tool_name, "args_summary": args_summary},
            "output": parsed,
        }
        legacy = {
            "type": "tool_end",
            "preview": parsed.get("preview", ""),
            "request_id": self.request_id,
        }
        yield from self._yield_fact(
            self._fact(
                kind="stage",
                phase="tool_invoke",
                status="end",
                payload=payload,
                duration_ms=duration_ms,
            ),
            legacy,
        )

    def emit_chunk(self, delta: str, *, channel: str = "answer") -> Iterator[str]:
        legacy = {"type": "token", "content": delta}
        yield from self._yield_fact(
            self._fact(
                kind="chunk",
                phase="llm_output",
                status="delta",
                payload={"delta": delta, "channel": channel},
            ),
            legacy,
        )

    def emit_done(
        self,
        *,
        timings: dict[str, Any],
        planning_cycle_complete: bool,
        early_exit: str | None = None,
        usage: dict[str, Any] | None = None,
    ) -> Iterator[str]:
        payload: dict[str, Any] = {
            "timings": timings,
            "planning_cycle_complete": planning_cycle_complete,
        }
        if early_exit:
            payload["early_exit"] = early_exit
        if usage:
            payload["usage"] = usage
        legacy: dict[str, Any] = {
            "type": "done",
            "planning_cycle_complete": planning_cycle_complete,
            "request_id": self.request_id,
            "timings": timings,
        }
        if early_exit:
            legacy["early_exit"] = early_exit
        if usage:
            legacy["usage"] = usage
        yield from self._yield_fact(
            self._fact(kind="control", phase="done", status="end", payload=payload),
            legacy,
        )

    def emit_error(self, message: str, *, code: str = "INTERNAL", retryable: bool = True) -> Iterator[str]:
        safe = message.strip() or "服务暂时不可用，请稍后再试"
        if len(safe) > 200:
            safe = safe[:200]
        legacy = {"type": "error", "content": safe, "request_id": self.request_id}
        yield from self._yield_fact(
            self._fact(
                kind="control",
                phase="error",
                status="end",
                payload={"code": code, "message": safe, "retryable": retryable},
            ),
            legacy,
        )
