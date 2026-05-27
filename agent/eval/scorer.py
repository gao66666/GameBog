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


def _extract_kb_hits(turn: dict[str, Any]) -> list[dict[str, Any]]:
    """从 turn 的 retrieve events 中提取知识库记忆。"""
    retrieves = turn.get("retrieves") if isinstance(turn.get("retrieves"), list) else []
    hits: list[dict[str, Any]] = []
    seen: set[str] = set()
    for r in retrieves:
        mems = r.get("memories") if isinstance(r.get("memories"), list) else []
        for m in mems:
            if not isinstance(m, dict):
                continue
            pid = str(m.get("memory_id", "") or m.get("id", "")).strip()
            if pid and pid not in seen:
                seen.add(pid)
                hits.append(m)
    return hits


def _kb_section_key(memory: dict[str, Any]) -> tuple[str, str] | None:
    """从记忆行提取 (article_id, section_title)。"""
    aid = str(memory.get("article_id", "")).strip()
    sp = memory.get("section_path")
    if isinstance(sp, list) and sp:
        sec = str(sp[-1]).strip()
    else:
        sec = str(memory.get("section_title", "")).strip()
    if aid and sec:
        return (aid, sec)
    return None


def score_kb_retrieval(
    hits: list[dict[str, Any]],
    expected_sections: list[dict[str, str]],
    k: int = 10,
) -> dict[str, Any]:
    """计算一轮对话中的 KB RAG Recall@K 和 MRR。"""
    if not expected_sections:
        return {"kb_recall_at_k": None, "kb_mrr": None, "kb_hit": None, "kb_passed": True}

    expected: list[tuple[str, str]] = []
    for item in expected_sections:
        aid = str(item.get("article_id", "")).strip()
        sec = str(item.get("section", "")).strip()
        if aid and sec:
            expected.append((aid, sec))

    if not expected:
        return {"kb_recall_at_k": None, "kb_mrr": None, "kb_hit": None, "kb_passed": True}

    top = hits[:k]
    ranked = [_kb_section_key(m) for m in top if _kb_section_key(m) is not None]

    # Recall
    found = sum(1 for exp in expected if exp in ranked)
    recall = found / len(expected) if expected else 0

    # MRR
    first_rank = None
    for rank, key in enumerate(ranked, 1):
        if key in expected:
            first_rank = rank
            break
    mrr = 1.0 / first_rank if first_rank else 0.0

    hit = first_rank is not None and first_rank <= k

    return {
        "kb_recall_at_k": round(recall, 4),
        "kb_mrr": round(mrr, 4),
        "kb_hit": hit,
        "kb_passed": hit and recall >= 0.5,
    }


def score_full_pipeline(turn: dict[str, Any], expect: dict[str, Any]) -> dict[str, Any]:
    """
    完整 Agent 链路：改写 → 路由 → 工具检索 → 分析 →（记忆检索）→ 计划 → 执行 → 成稿。
    expect.require_full_pipeline 为真时参与 pass/fail。
    """
    if not expect.get("require_full_pipeline"):
        return {"pipeline_ok": None}

    failures: list[str] = []
    rw = turn.get("rewrite")
    rewrite_ok = isinstance(rw, dict) and bool(str(rw.get("text", "")).strip())
    if not rewrite_ok:
        failures.append("pipeline: missing rewrite")

    route = turn.get("route")
    route_ok = isinstance(route, dict) and isinstance(route.get("servers"), list)
    if not route_ok:
        failures.append("pipeline: missing route")

    tool_ret = turn.get("tool_retrieve")
    tool_retrieve_ok = isinstance(tool_ret, dict)
    if not tool_retrieve_ok:
        failures.append("pipeline: missing tool_retrieve")

    analyses = turn.get("analyses") if isinstance(turn.get("analyses"), list) else []
    analyse_ok = len(analyses) >= 1
    if not analyse_ok:
        failures.append("pipeline: missing analyse")

    plans = turn.get("plans") if isinstance(turn.get("plans"), list) else []
    plan_ok = len(plans) >= 1
    if not plan_ok:
        failures.append("pipeline: missing plan")

    cycle_ok = bool(turn.get("planning_cycle_complete"))
    if not cycle_ok:
        failures.append("pipeline: planning_cycle_complete=false")

    timings = turn.get("timings") if isinstance(turn.get("timings"), dict) else {}
    execute_ms = int(timings.get("execute_ms") or 0)
    tools = turn.get("tools") if isinstance(turn.get("tools"), list) else []
    names = _tool_names(turn)
    tools_any = [str(x) for x in (expect.get("tools_any") or [])]
    tools_min = int(expect.get("tools_min_count", 0) or 0)

    if tools_any or tools_min > 0:
        tool_exec_ok = any(t in names for t in tools_any) if tools_any else len(names) >= tools_min
        if not tool_exec_ok:
            failures.append(f"pipeline: tool execute expected any={tools_any} min={tools_min} got={names}")
    else:
        retrieves = turn.get("retrieves") if isinstance(turn.get("retrieves"), list) else []
        retrieve_hits = sum(int(r.get("hits") or 0) for r in retrieves if isinstance(r, dict))
        kb_exp = expect.get("kb_expected_sections")
        if kb_exp:
            execute_ok = retrieve_hits > 0 or execute_ms > 0 or len(tools) > 0
        else:
            execute_ok = execute_ms > 0 or len(tools) > 0
        if not execute_ok:
            failures.append("pipeline: missing execute (tools or execute_ms)")

    out = turn.get("output") if isinstance(turn.get("output"), dict) else {}
    output_ok = bool(str(out.get("text", "")).strip()) or str(turn.get("early_exit", "")).strip() in (
        "clarify",
        "cannot",
        "answer",
    )
    if not output_ok:
        failures.append("pipeline: missing output")

    return {
        "pipeline_ok": len(failures) == 0,
        "pipeline_rewrite_ok": rewrite_ok,
        "pipeline_route_ok": route_ok,
        "pipeline_tool_retrieve_ok": tool_retrieve_ok,
        "pipeline_analyse_ok": analyse_ok,
        "pipeline_plan_ok": plan_ok,
        "pipeline_cycle_ok": cycle_ok,
        "pipeline_execute_ms": execute_ms,
        "pipeline_failures": failures,
    }


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

    # ── KB RAG 指标 ──
    kb_exp = expect.get("kb_expected_sections")
    if kb_exp:
        kb_hits = _extract_kb_hits(turn)
        kb_scored = score_kb_retrieval(kb_hits, kb_exp, k=10)
        metrics.update(kb_scored)
        if not kb_scored.get("kb_passed"):
            failures.append(f"kb_retrieval recall={kb_scored.get('kb_recall_at_k')} mrr={kb_scored.get('kb_mrr')}")

    metrics["task_completed"] = compute_task_completed(turn, metrics)
    if expect.get("require_task_completed") and not metrics["task_completed"]:
        failures.append("task not completed (outcome)")

    pipe = score_full_pipeline(turn, expect)
    for k, v in pipe.items():
        if k != "pipeline_failures":
            metrics[k] = v
    if pipe.get("pipeline_ok") is False:
        failures.extend(pipe.get("pipeline_failures") or [])

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
    pipe_cases = [r for r in results if r.get("metrics", {}).get("pipeline_ok") is not None]
    pipe_ok = sum(1 for r in pipe_cases if r.get("metrics", {}).get("pipeline_ok"))

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
    if pipe_cases:
        agg["pipeline_pass_rate"] = round(pipe_ok / len(pipe_cases), 3)

    # ── KB RAG 指标 ──
    kb_recalls = [r.get("metrics", {}).get("kb_recall_at_k") for r in results if r.get("metrics", {}).get("kb_recall_at_k") is not None]
    kb_mrrs = [r.get("metrics", {}).get("kb_mrr") for r in results if r.get("metrics", {}).get("kb_mrr") is not None]
    if kb_recalls:
        agg["kb_mean_recall_at_k"] = round(sum(kb_recalls) / len(kb_recalls), 4)
        agg["kb_mean_mrr"] = round(sum(kb_mrrs) / len(kb_mrrs), 4) if kb_mrrs else 0
        agg["kb_cases"] = len(kb_recalls)
        agg["kb_pass_rate"] = round(sum(1 for r in results if r.get("metrics", {}).get("kb_passed")) / len(kb_recalls), 3)

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
