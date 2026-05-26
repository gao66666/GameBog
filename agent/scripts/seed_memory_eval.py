#!/usr/bin/env python3
"""
为 memory_cases 写入可检索的测试记忆（Mongo preference + rule）。

用法:
  python agent/scripts/seed_memory_eval.py --user-id 42 --tag eval_ab_2026
  # 将输出的 mongo:... 填入 cases/memory_cases.yaml expected_ids

不会删除已有记忆；重复运行会 upsert 同 fingerprint（同 title/content 则覆盖）。
"""

from __future__ import annotations

import argparse
import asyncio
import hashlib
import json
import sys
from pathlib import Path

AGENT_DIR = Path(__file__).resolve().parents[1]
if str(AGENT_DIR) not in sys.path:
    sys.path.insert(0, str(AGENT_DIR))

from memory.memory_manager import _apply_mongo_decisions, init_memory_services  # noqa: E402


def _fingerprint(user_id: int, kind: str, title: str, content: str) -> str:
    return hashlib.sha1(f"{user_id}:{kind}:{title}:{content}".encode("utf-8")).hexdigest()


async def main_async(user_id: int, tag: str) -> int:
    await init_memory_services()
    facts = [
        {
            "kind": "preference",
            "title": f"评测爱好-{tag}",
            "content": f"用户喜欢玩原神和星穹铁道（评测种子 {tag}）",
            "tags": [tag, "eval", "game"],
            "importance": 5,
        },
        {
            "kind": "rule",
            "title": f"回复风格-{tag}",
            "content": f"用户希望回答简洁，少用敬语（评测 {tag}）",
            "tags": [tag, "eval"],
            "importance": 4,
        },
    ]
    actions = []
    ids: list[str] = []
    for fact in facts:
        fp = _fingerprint(user_id, fact["kind"], fact["title"], fact["content"])
        ids.append(fp)
        actions.append({
            "action": "add",
            "fingerprint": fp,
            "kind": fact["kind"],
            "title": fact["title"],
            "content": fact["content"],
            "tags": fact["tags"],
            "importance": fact["importance"],
        })
        print(f"  mongo:{fp}  kind={fact['kind']}")

    n = _apply_mongo_decisions(user_id, actions)
    print(f"\n写入 {n} 条（user_id={user_id}）")
    print("填入 memory_cases.yaml 示例:")
    print(
        json.dumps(
            {
                "user_id": user_id,
                "query": "我喜欢玩什么游戏",
                "memory_hints": ["游戏"],
                "expect": {
                    "expected_ids": [f"mongo:{x}" for x in ids[:1]],
                    "forbidden_ids": [],
                    "min_recall_at_k": 1.0,
                    "k": 10,
                },
            },
            ensure_ascii=False,
            indent=2,
        )
    )
    return 0


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--user-id", type=int, required=True)
    p.add_argument("--tag", default="eval_seed")
    args = p.parse_args()
    if args.user_id <= 0:
        print("user-id must be > 0")
        return 2
    return asyncio.run(main_async(args.user_id, args.tag))


if __name__ == "__main__":
    raise SystemExit(main())
