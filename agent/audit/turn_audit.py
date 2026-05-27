"""
离线 Turn 核查：从 Go ChatProxy 写入的 agent_turn 日志抽样，规则 + 可选 LLM 根因分析。

不接入在线 SSE；输入为日志文件或 JSONL（每行一条 turn 或 zap JSON 行）。
"""

from __future__ import annotations

import json
import random
import re
from typing import Any

from infra.config import get_prompts, load_config
from infra.llm_util import chat_openai_kwargs

_USER_INTENT_KEYWORDS = ("我的", "收藏", "积分", "个人", "资料", "钱包")


def _strip_json_fence(text: str) -> str:
    t = (text or "").strip()
    if t.startswith("```"):
        t = re.sub(r"^```(?:json)?\s*", "", t, flags=re.IGNORECASE)
        if t.endswith("```"):
            t = t[:-3].strip()
    return t


def _is_turn_record(obj: dict[str, Any]) -> bool:
    return isinstance(obj, dict) and obj.get("schema_version") and obj.get("request_id")


def extract_turn_from_log_line(line: str) -> dict[str, Any] | None:
    """从单行日志解析 agent_turn。支持 zap JSON（msg=agent_turn + turn）或纯 turn JSONL。"""
    line = line.strip()
    if not line or line.startswith("#"):
        return None
    try:
        obj = json.loads(line)
    except json.JSONDecodeError:
        return None
    if not isinstance(obj, dict):
        return None
    if _is_turn_record(obj):
        return obj
    turn = obj.get("turn")
    if isinstance(turn, dict) and _is_turn_record(turn):
        return turn
    msg = str(obj.get("msg") or obj.get("message") or "").strip()
    if msg == "agent_turn" and isinstance(turn, dict):
        return turn
    return None


def load_turns_from_file(path: str) -> list[dict[str, Any]]:
    seen: set[str] = set()
    out: list[dict[str, Any]] = []
    with open(path, "r", encoding="utf-8", errors="replace") as f:
        for line in f:
            turn = extract_turn_from_log_line(line)
            if not turn:
                continue
            rid = str(turn.get("request_id", "")).strip()
            if rid and rid in seen:
                continue
            if rid:
                seen.add(rid)
            out.append(turn)
    return out


def sample_turns(
    turns: list[dict[str, Any]],
    n: int,
    *,
    seed: int | None = None,
) -> list[dict[str, Any]]:
    if n <= 0 or not turns:
        return []
    if len(turns) <= n:
        return list(turns)
    rng = random.Random(seed)
    idx = rng.sample(range(len(turns)), n)
    idx.sort()
    return [turns[i] for i in idx]


def run_rule_audit(turn: dict[str, Any]) -> list[dict[str, Any]]:
    """零 LLM 规则核查，返回 issue 列表。"""
    issues: list[dict[str, Any]] = []
    status = str(turn.get("status", "")).strip()
    rid = str(turn.get("request_id", "")).strip()

    if status in ("error", "aborted"):
        issues.append({
            "layer": "stream",
            "severity": "fail" if status == "error" else "warn",
            "summary": f"轮次状态为 {status}",
            "request_id": rid,
        })
    if turn.get("error"):
        err = turn["error"]
        if isinstance(err, dict):
            issues.append({
                "layer": "agent",
                "severity": "fail",
                "summary": f"Agent 报错: {err.get('code', '')} {err.get('message', '')}".strip(),
                "request_id": rid,
            })

    route = turn.get("route") if isinstance(turn.get("route"), dict) else {}
    servers = route.get("servers") or []
    if isinstance(servers, list) and "user" in servers and not route.get("has_token"):
        issues.append({
            "layer": "route",
            "severity": "fail",
            "summary": "路由选了 user 组但 has_token=false",
            "request_id": rid,
        })

    msg = ""
    inp = turn.get("input")
    if isinstance(inp, dict):
        msg = str(inp.get("message", ""))
    if msg and any(k in msg for k in _USER_INTENT_KEYWORDS):
        if isinstance(servers, list) and "user" not in servers:
            issues.append({
                "layer": "route",
                "severity": "warn",
                "summary": "用户话含个人中心意图但未选 user 组",
                "request_id": rid,
            })

    tools = turn.get("tools") if isinstance(turn.get("tools"), list) else []
    for t in tools:
        if not isinstance(t, dict):
            continue
        if t.get("ok") is False:
            issues.append({
                "layer": "execute",
                "severity": "warn",
                "summary": f"工具 {t.get('tool', '?')} 调用失败",
                "request_id": rid,
                "tool": t.get("tool"),
            })

    analyses = turn.get("analyses") if isinstance(turn.get("analyses"), list) else []
    last_disp = ""
    if analyses and isinstance(analyses[-1], dict):
        last_disp = str(analyses[-1].get("disposition", "")).strip()
    plans = turn.get("plans") if isinstance(turn.get("plans"), list) else []
    needs_tools = any(isinstance(p, dict) and p.get("requires_tools") for p in plans)
    if needs_tools and not tools and status == "ok":
        issues.append({
            "layer": "execute",
            "severity": "warn",
            "summary": "计划需要工具但未记录任何 tool 调用",
            "request_id": rid,
        })

    output = turn.get("output") if isinstance(turn.get("output"), dict) else {}
    out_text = str(output.get("text", "")).strip()
    if status == "ok" and last_disp in ("", "proceed", "answer") and not out_text:
        issues.append({
            "layer": "output",
            "severity": "fail",
            "summary": "status=ok 但最终答复为空",
            "request_id": rid,
        })

    if not turn.get("planning_cycle_complete") and status == "ok" and last_disp == "proceed":
        issues.append({
            "layer": "orchestration",
            "severity": "warn",
            "summary": "disposition=proceed 但 planning_cycle_complete=false",
            "request_id": rid,
        })

    return issues


def _truncate_json(data: Any, max_chars: int) -> str:
    s = json.dumps(data, ensure_ascii=False)
    if len(s) <= max_chars:
        return s
    return s[: max_chars - 24] + "\n…(turn truncated)…"


def _audit_llm():
    from langchain_openai import ChatOpenAI

    cfg = load_config()
    ac = cfg.get("turn_audit", {}) or {}
    sc = cfg.get("summary_llm") or {}
    merged = {
        **cfg,
        **sc,
        "model": ac.get("model", sc.get("model", cfg.get("model", "gpt-4o-mini"))),
        "base_url": ac.get("base_url", sc.get("base_url", cfg.get("base_url"))),
        "api_key": ac.get("api_key", sc.get("api_key", cfg.get("api_key"))),
        "thinking": ac.get("thinking", sc.get("thinking", cfg.get("thinking"))),
    }
    return ChatOpenAI(**chat_openai_kwargs(merged, temperature=0))


async def run_llm_audit(
    turn: dict[str, Any],
    rule_issues: list[dict[str, Any]],
    *,
    max_turn_chars: int | None = None,
) -> dict[str, Any] | None:
    from langchain_core.messages import HumanMessage

    cfg = load_config().get("turn_audit", {}) or {}
    cap = int(max_turn_chars if max_turn_chars is not None else cfg.get("max_turn_json_chars", 14000))
    turn_json = _truncate_json(turn, cap)
    rules_json = json.dumps(rule_issues, ensure_ascii=False)
    prompt = get_prompts()["turn_audit"].format(
        turn_json=turn_json,
        rule_issues_json=rules_json,
    )
    resp = await _audit_llm().ainvoke([HumanMessage(content=prompt)])
    raw = getattr(resp, "content", None) or ""
    try:
        obj = json.loads(_strip_json_fence(str(raw)))
        return obj if isinstance(obj, dict) else None
    except json.JSONDecodeError:
        return None


async def audit_one_turn(
    turn: dict[str, Any],
    *,
    use_llm: bool = True,
    max_turn_chars: int | None = None,
) -> dict[str, Any]:
    rule_issues = run_rule_audit(turn)
    verdict = "ok"
    if any(i.get("severity") == "fail" for i in rule_issues):
        verdict = "fail"
    elif rule_issues:
        verdict = "warn"

    report: dict[str, Any] = {
        "request_id": turn.get("request_id"),
        "user_id": turn.get("user_id"),
        "status": turn.get("status"),
        "rule_verdict": verdict,
        "rule_issues": rule_issues,
        "llm_audit": None,
    }

    if use_llm:
        llm_out = await run_llm_audit(turn, rule_issues, max_turn_chars=max_turn_chars)
        report["llm_audit"] = llm_out
        if isinstance(llm_out, dict) and llm_out.get("verdict"):
            report["verdict"] = llm_out.get("verdict")
        else:
            report["verdict"] = verdict
    else:
        report["verdict"] = verdict

    return report
