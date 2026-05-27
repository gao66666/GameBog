#!/usr/bin/env python3
"""重跑失败用例，打印 KB retrieve 排名与期望章节对比。"""
from __future__ import annotations

import json
import sys
import urllib.request
from pathlib import Path

import yaml

AGENT_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(AGENT_DIR))

from eval.scorer import score_kb_retrieval, _extract_kb_hits, _kb_section_key  # noqa: E402
from eval.sse_collector import SseTurnCollector  # noqa: E402

FAILED_GLOBAL = [11, 14, 16, 18, 28, 30]
CASES_PATH = AGENT_DIR / "cases" / "dialogue_metrics_cases.yaml"


def _flatten(blob: dict) -> list[dict]:
    defaults = blob.get("defaults") or {}
    base = dict(defaults.get("expect") or {})
    flat: list[dict] = []
    g = 0
    for sess in blob.get("sessions") or []:
        sid = sess["id"]
        csid = sess.get("chat_session_id") or sid
        kb_exp = sess.get("kb_expectations") or []
        turns = sess.get("turns") or []
        for i, t in enumerate(turns, 1):
            g += 1
            if isinstance(t, dict):
                msg = str(t.get("message", "")).strip()
                need_tok = bool(t.get("require_token"))
            else:
                msg = str(t).strip()
                need_tok = False
            expect = dict(base)
            if kb_exp and i - 1 < len(kb_exp):
                ekb = kb_exp[i - 1]
                if isinstance(ekb, dict) and ekb.get("expected_sections"):
                    expect["kb_expected_sections"] = ekb["expected_sections"]
            flat.append({
                "global_index": g,
                "session_id": sid,
                "chat_session_id": csid,
                "turn_index": i,
                "message": msg,
                "require_token": need_tok,
                "kb_expected_sections": expect.get("kb_expected_sections") or [],
            })
    return flat


def _section_label(m: dict) -> str:
    sp = m.get("section_path")
    if isinstance(sp, list) and sp:
        sec = str(sp[-1])
        aid = str(m.get("article_id", ""))
        return f"{aid} / {sec}"
    return str(m.get("article_id") or m.get("memory_id") or "?")


def _chat(msg: str, csid: str, token: str) -> dict:
    body = json.dumps({
        "message": msg,
        "user_id": 88001,
        "token": token or "",
        "chat_session_id": csid + "_inspect",
        "history_owned_by_gateway": False,
    }).encode()
    req = urllib.request.Request(
        "http://127.0.0.1:9091/chat",
        data=body,
        headers={"Content-Type": "application/json", "Accept": "text/event-stream"},
    )
    col = SseTurnCollector(user_message=msg)
    with urllib.request.urlopen(req, timeout=180) as resp:
        for raw in resp:
            col.observe_line(raw.decode("utf-8", errors="replace"))
    return col.finalize()


def main() -> int:
    blob = yaml.safe_load(CASES_PATH.read_text(encoding="utf-8"))
    flat = _flatten(blob)
    by_g = {x["global_index"]: x for x in flat}

    token = ""
    try:
        login = json.dumps({"tel": "13800088001", "password": "EvalTest123!"}).encode()
        req = urllib.request.Request(
            "http://127.0.0.1:8084/api/v1/login",
            data=login,
            headers={"Content-Type": "application/json"},
        )
        with urllib.request.urlopen(req, timeout=10) as r:
            token = json.loads(r.read().decode())["data"]["token"]
    except Exception:
        pass

    for g in FAILED_GLOBAL:
        item = by_g[g]
        msg = item["message"]
        tok = token if item["require_token"] else ""
        print("=" * 72)
        print(f"G{g} [{item['session_id']}] {msg[:60]}{'…' if len(msg) > 60 else ''}")
        exp = item["kb_expected_sections"]
        if exp:
            print("期望章节:", ", ".join(f"{e['article_id']}/{e['section']}" for e in exp))
        else:
            print("期望章节: (无 KB 断言)")
        print()

        turn = _chat(msg, item["chat_session_id"], tok)
        hits = _extract_kb_hits(turn)
        if exp:
            scored = score_kb_retrieval(hits, exp, k=10)
            print(f"KB 得分: recall={scored['kb_recall_at_k']:.0%} mrr={scored['kb_mrr']:.4f} hit={scored['kb_hit']}")

        exp_keys = {
            (str(e["article_id"]), str(e["section"]))
            for e in exp
            if e.get("article_id") and e.get("section")
        }

        print(f"\nTop-{min(10, len(hits))} 召回 (共 {len(hits)} 条 KB 记忆):")
        print(f"{'Rank':<5} {'Score':<8} {'Match':<6} {'article_id / section'}")
        print("-" * 60)
        for rank, m in enumerate(hits[:10], 1):
            key = _kb_section_key(m)
            match = "✓" if key and key in exp_keys else ""
            score = m.get("score", "-")
            if isinstance(score, float):
                score = f"{score:.4f}"
            scope = m.get("memory_scope", "")
            kind = m.get("kind", "")
            label = _section_label(m)
            extra = f" [{scope}/{kind}]" if scope or kind else ""
            print(f"{rank:<5} {str(score):<8} {match:<6} {label}{extra}")

        if exp_keys:
            ranked_keys = [_kb_section_key(m) for m in hits[:10] if _kb_section_key(m)]
            missing = [f"{a}/{s}" for a, s in sorted(exp_keys) if (a, s) not in ranked_keys]
            if missing:
                print(f"\n期望但未进 Top10: {', '.join(missing)}")

        tools = turn.get("tools") or []
        if tools:
            print(f"\n工具调用 ({len(tools)}):")
            for t in tools:
                ok = "ok" if t.get("ok") is not False else "FAIL"
                print(f"  - {t.get('tool')} [{ok}]")
        analyses = turn.get("analyses") or []
        if analyses:
            print(f"disposition 末轮: {analyses[-1].get('disposition')}")
        print()

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
