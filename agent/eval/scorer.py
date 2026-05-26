"""按 case expect 对 turn 打指标并判定 pass/fail。"""

from __future__ import annotations

from typing import Any


def _servers(turn: dict[str, Any]) -> list[str]:
    route = turn.get("route")
    if not isinstance(route, dict):
        return []
    s = route.get("servers")
    return list(s) if isinstance(s, list) else []


def _tool_names(turn: dict[str, Any]) -> list[str]:
    tools = turn.get("tools")
    if not isinstance(tools, list):
        return []
    return [str(t.get("tool", "")) for t in tools if isinstance(t, dict) and t.get("tool")]


def _last_disposition(turn: dict[str, Any]) -> str:
    analyses = turn.get("analyses")
    if not isinstance(analyses, list) or not analyses:
        return ""
    last = analyses[-1]
    return str(last.get("disposition", "")).strip() if isinstance(last, dict) else ""


def _total_ms(turn: dict[str, Any]) -> int:
    timings = turn.get("timings")
    if isinstance(timings, dict) and timings.get("total_ms") is not None:
        return int(timings["total_ms"])
    return int(turn.get("total_ms") or 0)


def _execute_ms(turn: dict[str, Any]) -> int | None:
    timings = turn.get("timings")
    if isinstance(timings, dict) and timings.get("execute_ms") is not None:
        return int(timings["execute_ms"])
    return None


def _retrieval_latency_ms(turn: dict[str, Any]) -> int | None:
    timings = turn.get("timings")
    if isinstance(timings, dict) and timings.get("retrieve_ms") is not None:
        return int(timings["retrieve_ms"])
    retrieves = turn.get("retrieves")
    if isinstance(retrieves, list) and retrieves:
        durs = [
            int(r["duration_ms"])
            for r in retrieves
            if isinstance(r, dict) and r.get("duration_ms") is not None
        ]
        if durs:
            return max(durs)
    return None


def compute_task_completed(turn: dict[str, Any], metrics: dict[str, Any]) -> bool:
    """
    任务完成（结果导向，比 case pass 宽松）：
    流正常、工具有结果、非 cannot/clarify、对用户有实质回复。
    """
    if not metrics.get("stream_ok") or turn.get("error"):
        return False
    early = str(turn.get("early_exit", "")).strip().lower()
    if early in ("cannot", "error", "clarify"):
        return False
    if _last_disposition(turn) == "cannot":
        return False
    if not metrics.get("tools_all_ok"):
        return False
    out = turn.get("output") if isinstance(turn.get("output"), dict) else {}
    return bool(str(out.get("text", "")).strip())


def score_turn(
    turn: dict[str, Any],
    expect: dict[str, Any],
    *,
    has_token: bool,
) -> dict[str, Any]:
    """返回 metrics 字典 + passed(bool) + failures(list[str])。"""
    failures: list[str] = []
    metrics: dict[str, Any] = {}

    status = str(turn.get("status", "")).strip()
    expect_status = str(expect.get("status", "ok")).strip()
    metrics["stream_ok"] = status == expect_status and not turn.get("error")
    if not metrics["stream_ok"]:
        failures.append(f"status={status!r} expect={expect_status!r}")

    servers = _servers(turn)
    must_inc = [str(x) for x in (expect.get("servers_must_include") or [])]
    must_exc = [str(x) for x in (expect.get("servers_must_exclude") or [])]
    route_ok = all(s in servers for s in must_inc) and all(s not in servers for s in must_exc)
    metrics["route_match"] = route_ok if (must_inc or must_exc) else True
    if not metrics["route_match"]:
        failures.append(f"servers={servers} want +{must_inc} -{must_exc}")

    if has_token:
        token_ok = ("user" not in servers) or ("user" in servers)
    else:
        token_ok = "user" not in servers
    route = turn.get("route") if isinstance(turn.get("route"), dict) else {}
    if route.get("has_token") is False and "user" in servers:
        token_ok = False
    metrics["route_token_consistent"] = token_ok
    if not token_ok:
        failures.append(f"token/route inconsistent servers={servers} has_token={route.get('has_token')}")

    names = _tool_names(turn)
    tools_any = [str(x) for x in (expect.get("tools_any") or [])]
    tools_min = int(expect.get("tools_min_count", 0) or 0)
    tools_max = expect.get("tools_max_count")
    tools_none = bool(expect.get("tools_none"))

    if tools_none or tools_max == 0:
        tools_match = len(names) == 0
    elif tools_any:
        tools_match = any(t in names for t in tools_any)
    else:
        tools_match = len(names) >= tools_min
    if tools_min and not tools_any:
        tools_match = tools_match and len(names) >= tools_min
    metrics["tools_match"] = tools_match
    metrics["tool_call_count"] = len(names)
    if not tools_match:
        failures.append(f"tools={names} expect any={tools_any} min={tools_min} max={tools_max}")

    tools = turn.get("tools") if isinstance(turn.get("tools"), list) else []
    if tools:
        metrics["tools_all_ok"] = all(isinstance(t, dict) and t.get("ok") is not False for t in tools)
        if not metrics["tools_all_ok"]:
            failures.append("some tool ok=false")
    else:
        metrics["tools_all_ok"] = True

    disp = _last_disposition(turn)
    disp_any = [str(x) for x in (expect.get("disposition_any") or [])]
    metrics["disposition_match"] = (disp in disp_any) if disp_any else True
    if disp_any and not metrics["disposition_match"]:
        failures.append(f"disposition={disp!r} not in {disp_any}")

    early = str(turn.get("early_exit", "")).strip()
    out = turn.get("output") if isinstance(turn.get("output"), dict) else {}
    text = str(out.get("text", "")).strip()
    if early in ("clarify", "cannot"):
        metrics["output_ok"] = True
    else:
        metrics["output_ok"] = bool(text)
    if not metrics["output_ok"]:
        failures.append("output empty")

    rw = turn.get("rewrite")
    metrics["rewrite_nonempty"] = bool(
        isinstance(rw, dict) and str(rw.get("text", "")).strip()
    ) or bool(str(turn.get("input", {}).get("message", "")).strip())

    total = _total_ms(turn)
    metrics["timing_total_ms"] = total
    ex_ms = _execute_ms(turn)
    if ex_ms is not None:
        metrics["timing_execute_ms"] = ex_ms
    ret_ms = _retrieval_latency_ms(turn)
    if ret_ms is not None:
        metrics["retrieval_latency_ms"] = ret_ms
    cap = expect.get("max_total_ms")
    if cap is not None:
        metrics["timing_under_cap"] = total <= int(cap)
        if not metrics["timing_under_cap"]:
            failures.append(f"total_ms={total} > cap={cap}")
    else:
        metrics["timing_under_cap"] = True

    analyses = turn.get("analyses") if isinstance(turn.get("analyses"), list) else []
    metrics["orch_cycles"] = len(analyses)

    retrieves = turn.get("retrieves") if isinstance(turn.get("retrieves"), list) else []
    hits = sum(int(r.get("hits") or 0) for r in retrieves if isinstance(r, dict))
    metrics["memory_retrieved"] = hits > 0
    if expect.get("require_memory") and not metrics["memory_retrieved"]:
        failures.append("expected memory retrieve hits>0")

    metrics["task_completed"] = compute_task_completed(turn, metrics)
    if expect.get("require_task_completed") and not metrics["task_completed"]:
        failures.append("task not completed (outcome)")

    passed = len(failures) == 0
    return {"passed": passed, "failures": failures, "metrics": metrics}


def _percentile(vals: list[int], p: float) -> int:
    if not vals:
        return 0
    ordered = sorted(vals)
    i = max(0, int(len(ordered) * p) - 1)
    return ordered[min(i, len(ordered) - 1)]


def _mean(vals: list[int]) -> int:
    if not vals:
        return 0
    return int(round(sum(vals) / len(vals)))


def aggregate_batch(results: list[dict[str, Any]], thresholds: dict[str, Any]) -> dict[str, Any]:
    n = len(results)
    if n == 0:
        return {"cases": 0}

    passed = sum(1 for r in results if r.get("passed"))
    task_done = sum(1 for r in results if r.get("metrics", {}).get("task_completed"))
    stream_ok = sum(1 for r in results if r.get("metrics", {}).get("stream_ok"))
    route_ok = sum(1 for r in results if r.get("metrics", {}).get("route_match"))
    tool_cases = [r for r in results if (r.get("metrics", {}).get("tool_call_count") or 0) > 0]
    tool_ok_sum = sum(
        1 for r in tool_cases if r.get("metrics", {}).get("tools_all_ok")
    )
    totals = [int(r.get("metrics", {}).get("timing_total_ms") or 0) for r in results]
    executes = [
        int(r["metrics"]["timing_execute_ms"])
        for r in results
        if r.get("metrics", {}).get("timing_execute_ms") is not None
    ]
    retrievals = [
        int(r["metrics"]["retrieval_latency_ms"])
        for r in results
        if r.get("metrics", {}).get("retrieval_latency_ms") is not None
    ]
    tool_sel = [
        r for r in results
        if r.get("metrics", {}).get("tool_call_count", 0) > 0
        and "tools_match" in (r.get("metrics") or {})
    ]
    tool_sel_ok = sum(1 for r in tool_sel if r.get("metrics", {}).get("tools_match"))

    agg = {
        "cases": n,
        "case_pass_rate": round(passed / n, 3),
        "task_completion_rate": round(task_done / n, 3),
        "stream_ok_rate": round(stream_ok / n, 3),
        "route_match_rate": round(route_ok / n, 3),
        "tool_ok_rate": round(tool_ok_sum / len(tool_cases), 3) if tool_cases else 1.0,
        "avg_total_ms": _mean(totals),
        "median_total_ms": _percentile(totals, 0.5),
        "total_ms_p95": _percentile(totals, 0.95),
        "total_ms_p99": _percentile(totals, 0.99),
        "passed": passed,
        "tasks_completed": task_done,
        "failed": n - passed,
    }
    if executes:
        agg["avg_execute_ms"] = _mean(executes)
    if retrievals:
        agg["retrieval_latency_p95_ms"] = _percentile(retrievals, 0.95)
    if tool_sel:
        agg["tool_selection_accuracy"] = round(tool_sel_ok / len(tool_sel), 3)
    if tool_cases:
        agg["tool_execution_success_rate"] = agg["tool_ok_rate"]

    checks = [
        agg["case_pass_rate"] >= float(thresholds.get("case_pass_rate_min", 0.85)),
        agg["task_completion_rate"] >= float(thresholds.get("task_completion_rate_min", 0.88)),
        agg["stream_ok_rate"] >= float(thresholds.get("stream_ok_rate_min", 0.95)),
        agg["route_match_rate"] >= float(thresholds.get("route_match_rate_min", 0.9)),
        not tool_cases or agg["tool_ok_rate"] >= float(thresholds.get("tool_ok_rate_min", 0.9)),
        agg["total_ms_p95"] <= int(thresholds.get("total_ms_p95_max", 45000)),
    ]
    if thresholds.get("total_ms_p99_max") is not None:
        checks.append(agg["total_ms_p99"] <= int(thresholds["total_ms_p99_max"]))
    avg_cap = thresholds.get("avg_total_ms_max")
    if avg_cap is not None:
        checks.append(agg["avg_total_ms"] <= int(avg_cap))
    ret_cap = thresholds.get("retrieval_latency_p95_max_ms")
    if ret_cap is not None and retrievals:
        checks.append(agg.get("retrieval_latency_p95_ms", 0) <= int(ret_cap))
    tsa_min = thresholds.get("tool_selection_accuracy_min")
    if tsa_min is not None and tool_sel:
        checks.append(agg.get("tool_selection_accuracy", 0) >= float(tsa_min))
    tes_min = thresholds.get("tool_execution_success_rate_min")
    if tes_min is not None and tool_cases:
        checks.append(agg.get("tool_execution_success_rate", 0) >= float(tes_min))

    agg["batch_pass"] = all(checks)
    return agg
