#!/usr/bin/env python3
"""
A/B 测评：对 control / treatment 各跑一轮 turn_cases，再对比通过率与回归 case。

  python agent/scripts/run_ab_eval.py run --variant control --out eval_ab/control.json
  python agent/scripts/run_ab_eval.py run --variant treatment --out eval_ab/treatment.json
  python agent/scripts/run_ab_eval.py compare eval_ab/control.json eval_ab/treatment.json
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path

import yaml

AGENT_DIR = Path(__file__).resolve().parents[1]
REPO_ROOT = AGENT_DIR.parent
EXP_PATH = AGENT_DIR / "cases" / "experiments.yaml"
RUN_TURN = AGENT_DIR / "scripts" / "run_agent_eval.py"


def _load_yaml(path: Path) -> dict:
    with open(path, "r", encoding="utf-8") as f:
        return yaml.safe_load(f) or {}


def cmd_run(args: argparse.Namespace) -> int:
    exp = _load_yaml(EXP_PATH)
    variants = exp.get("variants") or {}
    vkey = args.variant
    if vkey not in variants:
        print(f"unknown variant {vkey!r}, choose from {list(variants)}")
        return 2

    v = variants[vkey]
    url = (args.url or v.get("agent_url") or os.getenv("AGENT_URL", "http://127.0.0.1:9091")).rstrip("/")
    if not url.endswith("/chat"):
        url += "/chat"

    case_filter = args.case or ",".join(exp.get("case_filter") or [])
    cmd = [
        sys.executable,
        str(RUN_TURN),
        "--url",
        url,
        "--out",
        args.out,
    ]
    if case_filter:
        cmd.extend(["--case", case_filter])
    if args.token:
        cmd.extend(["--token", args.token])

    print(f"A/B run variant={vkey} label={v.get('label')} url={url}")
    proc = subprocess.run(cmd, cwd=str(REPO_ROOT))
    if proc.returncode != 0 and Path(args.out).is_file():
        # 仍保留报告供 compare；非 0 表示批次未过阈值
        pass
    meta_path = Path(args.out).with_suffix(".meta.json")
    meta_path.write_text(
        json.dumps({"variant": vkey, "label": v.get("label"), "url": url}, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )
    return proc.returncode


def cmd_compare(args: argparse.Namespace) -> int:
    exp = _load_yaml(EXP_PATH)
    th = exp.get("thresholds") or {}

    control = json.loads(Path(args.control).read_text(encoding="utf-8"))
    treatment = json.loads(Path(args.treatment).read_text(encoding="utf-8"))

    from eval.ab_compare import compare_ab  # noqa: E402

    cmp = compare_ab(
        control,
        treatment,
        min_pass_rate_delta=float(th.get("min_pass_rate_delta", -0.05)),
        max_regression_cases=int(th.get("max_regression_cases", 1)),
    )
    p95_delta = cmp["p95_ms_delta"]
    max_p95 = int(th.get("max_p95_ms_increase", 8000))
    avg_delta = cmp.get("avg_total_ms_delta", 0)
    max_avg = int(th.get("max_avg_total_ms_increase", 5000))
    task_delta = cmp.get("task_completion_rate_delta", 0)
    min_task = float(th.get("min_task_completion_rate_delta", -0.05))
    cmp["latency_regression"] = p95_delta > max_p95 or avg_delta > max_avg
    cmp["task_regression"] = task_delta < min_task
    cmp["ab_pass"] = cmp["ab_pass"] and not cmp["latency_regression"] and not cmp["task_regression"]

    print(json.dumps(cmp, ensure_ascii=False, indent=2))
    if args.out:
        Path(args.out).write_text(json.dumps(cmp, ensure_ascii=False, indent=2), encoding="utf-8")
    return 0 if cmp.get("ab_pass") else 1


def main() -> int:
    p = argparse.ArgumentParser(description="Agent A/B 测评")
    sub = p.add_subparsers(dest="cmd", required=True)

    pr = sub.add_parser("run", help="跑单变体（写入 JSON 报告）")
    pr.add_argument("--variant", required=True, choices=["control", "treatment"])
    pr.add_argument("--url", default="")
    pr.add_argument("--token", default=os.getenv("AGENT_EVAL_TOKEN", ""))
    pr.add_argument("--case", default="")
    pr.add_argument("--out", required=True)

    pc = sub.add_parser("compare", help="对比两份报告")
    pc.add_argument("control")
    pc.add_argument("treatment")
    pc.add_argument("--out", default="")

    args = p.parse_args()
    if args.cmd == "run":
        return cmd_run(args)
    return cmd_compare(args)


if __name__ == "__main__":
    raise SystemExit(main())
