"""从 Agent SSE v1 fact 帧聚合成与 Go agent_turn 对齐的 turn dict。"""

from __future__ import annotations

import json
import time
from typing import Any


class SseTurnCollector:
    def __init__(self, *, user_message: str, request_id: str = "") -> None:
        self.user_message = user_message
        self.request_id = request_id
        self._started = time.time()
        self._answer_parts: list[str] = []
        self._pending_tools: dict[str, dict[str, Any]] = {}

        self.turn: dict[str, Any] = {
            "schema_version": 1,
            "request_id": request_id,
            "status": "ok",
            "input": {"message": user_message},
            "analyses": [],
            "retrieves": [],
            "plans": [],
            "tools": [],
            "timings": {},
            "planning_cycle_complete": False,
        }

    def observe_line(self, line: str) -> None:
        line = line.strip()
        if not line.startswith("data: "):
            return
        try:
            data = json.loads(line[6:])
        except json.JSONDecodeError:
            return
        if data.get("v") == 1 and data.get("kind"):
            self._observe_fact(data)
        elif data.get("type") == "token":
            self._answer_parts.append(str(data.get("content", "")))

    def _observe_fact(self, frame: dict[str, Any]) -> None:
        if frame.get("request_id"):
            self.turn["request_id"] = frame["request_id"]
            self.request_id = frame["request_id"]

        kind = frame.get("kind")
        phase = frame.get("phase")
        status = frame.get("status")
        payload = frame.get("payload") or {}
        if not isinstance(payload, dict):
            payload = {}
        dur = frame.get("duration_ms")

        if kind == "control":
            if phase == "done":
                self.turn["planning_cycle_complete"] = bool(payload.get("planning_cycle_complete"))
                if payload.get("early_exit"):
                    self.turn["early_exit"] = str(payload["early_exit"])
                timings = payload.get("timings")
                if isinstance(timings, dict):
                    self.turn["timings"] = {str(k): int(v) for k, v in timings.items()}
                usage = payload.get("usage")
                if isinstance(usage, dict) and usage:
                    self.turn["usage"] = usage
            elif phase == "error":
                self.turn["status"] = "error"
                self.turn["error"] = {
                    "code": str(payload.get("code", "")),
                    "message": str(payload.get("message", "")),
                }
            return

        if kind == "chunk" and phase == "llm_output" and status == "delta":
            delta = (payload.get("delta") or "") if isinstance(payload, dict) else ""
            if delta:
                self._answer_parts.append(str(delta))
            return

        if kind != "stage" or status != "end":
            if kind == "stage" and phase == "tool_invoke" and status == "start":
                self._tool_start(payload)
            return

        dms = int(dur) if dur is not None else 0
        if phase == "rewrite":
            out = payload.get("output") or {}
            self.turn["rewrite"] = {
                "text": str(out.get("rewritten", "")),
                "duration_ms": dms,
            }
        elif phase == "route":
            out = payload.get("output") or {}
            self.turn["route"] = {
                "servers": list(out.get("servers") or []),
                "tool_hints": list(out.get("tool_hints") or []),
                "memory_hints": list(out.get("memory_hints") or []),
                "has_token": bool(out.get("has_token")),
                "duration_ms": dms,
            }
        elif phase == "tool_retrieve":
            out = payload.get("output") or {}
            self.turn["tool_retrieve"] = {
                "tools": list(out.get("tools") or []),
                "mode": str(out.get("mode", "")),
                "count": int(out.get("count") or 0),
                "duration_ms": dms,
            }
        elif phase == "analyse":
            out = payload.get("output") or {}
            inp = payload.get("input") or {}
            self.turn["analyses"].append({
                "phase": str(out.get("phase") or inp.get("phase") or ""),
                "cycle": int(out.get("cycle") or 0),
                "disposition": str(out.get("disposition", "")),
                "intent": str(out.get("intent", "")),
                "duration_ms": dms,
            })
        elif phase == "retrieve":
            out = payload.get("output") or {}
            mems = out.get("memories") if isinstance(out.get("memories"), list) else []
            self.turn["retrieves"].append({
                "hits": int(out.get("hits") or 0),
                "memories": mems,
                "duration_ms": dms,
            })
        elif phase == "plan":
            out = payload.get("output") or {}
            self.turn["plans"].append({
                "requires_tools": bool(out.get("requires_tools", True)),
                "step_count": int(out.get("step_count") or 0),
                "duration_ms": dms,
            })
        elif phase == "output":
            out = payload.get("output") or {}
            text = str(out.get("reply_preview", ""))
            if not self.turn.get("output"):
                self.turn["output"] = {"text": "", "char_count": 0, "duration_ms": dms}
            if text:
                self.turn["output"]["preview"] = text
                self.turn["output"]["duration_ms"] = dms
        elif phase == "tool_invoke":
            self._tool_end(payload, dms)

    def _tool_start(self, payload: dict[str, Any]) -> None:
        tc = str(payload.get("tool_call_id", ""))
        inp = payload.get("input") or {}
        if tc:
            self._pending_tools[tc] = {
                "tool_call_id": tc,
                "tool": str(inp.get("tool", "")),
                "args_summary": inp.get("args_summary") if isinstance(inp.get("args_summary"), dict) else {},
            }

    def _tool_end(self, payload: dict[str, Any], dur: int) -> None:
        tc = str(payload.get("tool_call_id", ""))
        inp = payload.get("input") or {}
        out = payload.get("output") or {}
        rec = self._pending_tools.pop(tc, None) or {"tool_call_id": tc or f"tc_{len(self.turn['tools'])}"}
        if not rec.get("tool"):
            rec["tool"] = str(inp.get("tool", ""))
        if not rec.get("args_summary") and isinstance(inp.get("args_summary"), dict):
            rec["args_summary"] = inp.get("args_summary")
        rec["ok"] = bool(out.get("ok", True))
        if out.get("error_code"):
            rec["error_code"] = str(out["error_code"])
        if out.get("row_count") is not None:
            rec["row_count"] = int(out["row_count"])
        rec["duration_ms"] = dur
        self.turn["tools"].append(rec)

    def finalize(self) -> dict[str, Any]:
        text = "".join(self._answer_parts).strip()
        if not self.turn.get("output"):
            self.turn["output"] = {"text": text, "char_count": len(text), "preview": ""}
        else:
            self.turn["output"]["text"] = text
            self.turn["output"]["char_count"] = len(text)
        for tc, pending in self._pending_tools.items():
            self.turn["tools"].append(pending)
        self._pending_tools.clear()

        if not self.turn.get("timings"):
            self.turn["total_ms"] = int((time.time() - self._started) * 1000)
        else:
            self.turn["total_ms"] = int(self.turn["timings"].get("total_ms") or 0)
        self.turn["finished_at_ms"] = int(time.time() * 1000)
        return self.turn
