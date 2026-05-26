"""A/B 批次结果对比：同一 case 集在 control / treatment 上的指标差。"""

from __future__ import annotations

from typing import Any


def _by_case(results: list[dict[str, Any]]) -> dict[str, dict[str, Any]]:
    out: dict[str, dict[str, Any]] = {}
    for r in results:
        cid = str(r.get("case_id", ""))
        if cid:
            out[cid] = r
    return out


def compare_ab(
    control: dict[str, Any],
    treatment: dict[str, Any],
    *,
    min_pass_rate_delta: float = -0.05,
    max_regression_cases: int = 1,
) -> dict[str, Any]:
    """
    control / treatment 为 run_agent_eval 输出 JSON（含 results + aggregate）。
    判定 treatment 未显著劣化：通过率下降不超过 min_pass_rate_delta，且回归 case 数 <= max_regression_cases。
    """
    c_results = control.get("results") or []
    t_results = treatment.get("results") or []
    c_map = _by_case(c_results)
    t_map = _by_case(t_results)

    common = sorted(set(c_map) & set(t_map))
    regressions: list[dict[str, Any]] = []
    improvements: list[dict[str, Any]] = []

    for cid in common:
        cp = bool(c_map[cid].get("passed"))
        tp = bool(t_map[cid].get("passed"))
        if cp and not tp:
            regressions.append({
                "case_id": cid,
                "control_ms": c_map[cid].get("metrics", {}).get("timing_total_ms"),
                "treatment_ms": t_map[cid].get("metrics", {}).get("timing_total_ms"),
                "treatment_failures": t_map[cid].get("failures"),
            })
        elif tp and not cp:
            improvements.append({"case_id": cid})

    c_agg = control.get("aggregate") or {}
    t_agg = treatment.get("aggregate") or {}
    c_rate = float(c_agg.get("case_pass_rate", 0))
    t_rate = float(t_agg.get("case_pass_rate", 0))
    delta = round(t_rate - c_rate, 4)

    c_task = float(c_agg.get("task_completion_rate", c_rate))
    t_task = float(t_agg.get("task_completion_rate", t_rate))
    task_delta = round(t_task - c_task, 4)

    timing_c = int(c_agg.get("total_ms_p95", 0))
    timing_t = int(t_agg.get("total_ms_p95", 0))
    avg_c = int(c_agg.get("avg_total_ms", 0))
    avg_t = int(t_agg.get("avg_total_ms", 0))

    ab_pass = (
        delta >= float(min_pass_rate_delta)
        and len(regressions) <= int(max_regression_cases)
    )

    return {
        "cases_compared": len(common),
        "control_pass_rate": c_rate,
        "treatment_pass_rate": t_rate,
        "pass_rate_delta": delta,
        "control_task_completion_rate": c_task,
        "treatment_task_completion_rate": t_task,
        "task_completion_rate_delta": task_delta,
        "control_avg_total_ms": avg_c,
        "treatment_avg_total_ms": avg_t,
        "avg_total_ms_delta": avg_t - avg_c,
        "control_p95_ms": timing_c,
        "treatment_p95_ms": timing_t,
        "p95_ms_delta": timing_t - timing_c,
        "regressions": regressions,
        "improvements": improvements,
        "ab_pass": ab_pass,
    }
