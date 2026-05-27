#!/usr/bin/env python3
"""单条 KB 对话探针：走完整 /chat 链路并打印工具 + RAG 指标。"""
from __future__ import annotations

import json
import sys
import time
import urllib.request
from pathlib import Path

AGENT_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(AGENT_DIR))

from eval.scorer import score_turn  # noqa: E402
from eval.sse_collector import SseTurnCollector  # noqa: E402

MSG = "艾尔登法环是什么类型的游戏"
EXPECT = {
    "status": "ok",
    "require_task_completed": True,
    "require_full_pipeline": True,
    "disposition_any": ["answer", "proceed", "clarify", "cannot"],
    "max_total_ms": 55000,
    "kb_expected_sections": [{"article_id": "game:elden-ring", "section": "游戏名片"}],
}


def main() -> int:
    body = json.dumps(
        {
            "message": MSG,
            "user_id": 88001,
            "token": "",
            "chat_session_id": "eval_probe_one",
            "history_owned_by_gateway": False,
        }
    ).encode()
    req = urllib.request.Request(
        "http://127.0.0.1:9091/chat",
        data=body,
        headers={"Content-Type": "application/json", "Accept": "text/event-stream"},
    )
    col = SseTurnCollector(user_message=MSG)
    t0 = time.perf_counter()
    with urllib.request.urlopen(req, timeout=120) as resp:
        for raw in resp:
            col.observe_line(raw.decode("utf-8", errors="replace"))
    turn = col.finalize()
    scored = score_turn(turn, EXPECT, has_token=False)
    if turn.get("usage"):
        scored["metrics"]["usage"] = turn["usage"]
    m = scored["metrics"]
    wall = int((time.perf_counter() - t0) * 1000)

    print(f"问题: {MSG}")
    print(f"结果: {'PASS' if scored['passed'] else 'FAIL'}")
    print(
        f"耗时 total_ms={m.get('timing_total_ms')} wall={wall}ms  "
        f"tools={m.get('tool_call_count')}"
    )
    tok = m.get("usage") or {}
    print(
        f"Token total={tok.get('total_tokens')} "
        f"input={tok.get('input_tokens')} output={tok.get('output_tokens')}"
    )
    if m.get("kb_recall_at_k") is not None:
        print(
            f"KB Recall@k={m['kb_recall_at_k']:.1%}  "
            f"MRR={m['kb_mrr']:.4f}  hit={m.get('kb_hit')}"
        )
    print(f"链路 pipeline_ok={m.get('pipeline_ok')}  orch_cycles={m.get('orch_cycles')}")
    ret = (turn.get("retrieves") or [{}])[0]
    mems = ret.get("memories") or []
    print(f"retrieve hits={ret.get('hits')}  sse_memories={len(mems)}")
    if mems:
        top = mems[0]
        print(f"Top1: {top.get('article_id')} / {top.get('section_path')}")
    for f in scored.get("failures") or []:
        print(f"FAIL: {f}")
    return 0 if scored["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
