#!/usr/bin/env python3
"""汇总 dialogue_metrics_final.json 中 short_15 指标。"""
import json
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]
final_path = ROOT / "eval_reports" / "dialogue_metrics_final.json"
cases_path = ROOT / "cases" / "dialogue_metrics_cases.yaml"

d = json.loads(final_path.read_text(encoding="utf-8"))
cases = yaml.safe_load(cases_path.read_text(encoding="utf-8"))
msgs = [t.get("message", "") for t in next(s["turns"] for s in cases["sessions"] if s["id"] == "short_15")]

print("=== short_15 会话汇总 (dialogue_metrics_final.json) ===\n")
for s in d["aggregate"]["sessions"]:
    if s.get("session_id") == "short_15":
        for k, v in sorted(s.items()):
            print(f"{k}: {v}")
        break

print("\n=== 逐轮明细 ===\n")
rows = [r for r in d["results"] if r.get("session_id") == "short_15"]
rows.sort(key=lambda x: x.get("global_index", 0))

for r in rows:
    gi = r.get("global_index", 0)
    m = r.get("metrics", {})
    msg = msgs[gi - 1] if 1 <= gi <= len(msgs) else "?"
    status = "PASS" if r.get("passed") else "FAIL"
    line = (
        f"[{gi:2d}] {status}  {m.get('timing_total_ms', 0):>6}ms  "
        f"tools={m.get('tool_call_count', 0)}  orch={m.get('orch_cycles', 0)}  "
        f"task={m.get('task_completed')}  pipe={m.get('pipeline_ok')}"
    )
    print(line)
    print(f"     {msg}")
    kb_keys = [k for k in m if k.startswith("kb_")]
    if kb_keys:
        kb = {k: m[k] for k in kb_keys}
        print(f"     KB: {kb}")
    if r.get("failures"):
        print(f"     失败: {r['failures']}")
    print()
