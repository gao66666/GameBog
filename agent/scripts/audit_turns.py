#!/usr/bin/env python3
"""
从 agent_turn 日志随机抽样，规则 + LLM 核查并输出问题与改进建议。

用法（仓库根目录或 agent 目录）:

  python agent/scripts/audit_turns.py --log logger/agent_turn_log/agent_turn.log -n 5
  python agent/scripts/audit_turns.py --log turns.jsonl -n 10 --seed 42 --rules-only
  python agent/scripts/audit_turns.py --log webServer.log -n 3 --out audit_report.json

日志格式：每行 zap JSON（message/msg=agent_turn 且含 turn 字段），或每行一条 turn JSON。
"""

from __future__ import annotations

import argparse
import asyncio
import json
import sys
from pathlib import Path

AGENT_DIR = Path(__file__).resolve().parents[1]
if str(AGENT_DIR) not in sys.path:
    sys.path.insert(0, str(AGENT_DIR))

from audit.turn_audit import audit_one_turn, load_turns_from_file, sample_turns  # noqa: E402


def _parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="随机抽样 agent_turn 日志并核查")
    p.add_argument(
        "--log",
        default="logger/agent_turn_log/agent_turn.log",
        help="agent_turn 日志或 JSONL（默认 logger/agent_turn_log/agent_turn.log）",
    )
    p.add_argument("-n", "--sample", type=int, default=5, help="随机抽样条数（默认 5）")
    p.add_argument("--seed", type=int, default=None, help="随机种子（可复现）")
    p.add_argument("--rules-only", action="store_true", help="仅规则核查，不调用 LLM")
    p.add_argument("--out", default="", help="将完整报告写入 JSON 文件")
    p.add_argument("--status", default="", help="仅保留 turn.status 匹配（ok|error|aborted）")
    return p.parse_args()


async def _main() -> int:
    args = _parse_args()
    log_path = Path(args.log)
    if not log_path.is_file():
        print(f"文件不存在: {log_path}", file=sys.stderr)
        return 1

    turns = load_turns_from_file(str(log_path))
    if args.status:
        st = args.status.strip().lower()
        turns = [t for t in turns if str(t.get("status", "")).lower() == st]

    if not turns:
        print("未解析到任何 agent_turn 记录。请确认日志含 msg=agent_turn 或 turn JSONL。", file=sys.stderr)
        return 1

    picked = sample_turns(turns, args.sample, seed=args.seed)
    print(f"共 {len(turns)} 条 turn，抽样 {len(picked)} 条（seed={args.seed}）\n")

    reports: list[dict] = []
    for i, turn in enumerate(picked, 1):
        rid = turn.get("request_id", "?")
        print(f"=== [{i}/{len(picked)}] request_id={rid} status={turn.get('status')} ===")
        report = await audit_one_turn(turn, use_llm=not args.rules_only)
        reports.append(report)

        for issue in report.get("rule_issues") or []:
            print(f"  [规则/{issue.get('severity')}] {issue.get('layer')}: {issue.get('summary')}")

        llm = report.get("llm_audit")
        if isinstance(llm, dict):
            print(f"  [LLM] verdict={llm.get('verdict')} alignment={llm.get('alignment')}")
            if llm.get("issue_summary"):
                print(f"        问题: {llm.get('issue_summary')}")
            root = llm.get("root_cause") if isinstance(llm.get("root_cause"), dict) else {}
            if root.get("explanation"):
                print(f"        根因({root.get('layer', '?')}): {root.get('explanation')}")
            for sug in llm.get("improvement_suggestions") or []:
                if sug:
                    print(f"        建议: {sug}")
        elif not args.rules_only:
            print("  [LLM] 未返回有效 JSON")
        print()

    summary = {
        "sampled": len(picked),
        "total_in_file": len(turns),
        "fail": sum(1 for r in reports if r.get("verdict") == "fail"),
        "warn": sum(1 for r in reports if r.get("verdict") == "warn"),
        "ok": sum(1 for r in reports if r.get("verdict") == "ok"),
    }
    print("--- 汇总 ---")
    print(json.dumps(summary, ensure_ascii=False, indent=2))

    if args.out:
        out_path = Path(args.out)
        payload = {"summary": summary, "reports": reports}
        out_path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        print(f"\n报告已写入 {out_path}")

    return 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(_main()))
