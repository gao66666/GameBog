#!/usr/bin/env python3
"""
Agent 测评一键入口（离线默认；加 --e2e 需 Agent :9091 在线）

用法（在 agent 目录或仓库根）:
  python run_eval.py                    # 单元测试 + 注册表 + KB 分块 dry-run
  python run_eval.py --e2e              # 再加 turn/tool E2E
  python run_eval.py --e2e --token JWT  # 含 user 组 tool case
  python run_eval.py --rag              # 再加 memory/kb RAG（需 Qdrant + 已 seed）
  python run_eval.py --chat "现在几点"  # 手工 SSE 联调
"""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
import urllib.error
import urllib.request
from pathlib import Path

AGENT_DIR = Path(__file__).resolve().parent
TESTS_DIR = AGENT_DIR / "tests"
SCRIPTS_DIR = AGENT_DIR / "scripts"


def _subprocess_env() -> dict[str, str]:
    env = os.environ.copy()
    root = str(AGENT_DIR)
    env["PYTHONPATH"] = root + os.pathsep + env.get("PYTHONPATH", "")
    return env


def _run(cmd: list[str], *, label: str) -> int:
    print(f"\n{'=' * 60}\n>> {label}\n{'=' * 60}")
    print(" ", " ".join(cmd), flush=True)
    r = subprocess.run(cmd, cwd=AGENT_DIR, env=_subprocess_env())
    if r.returncode != 0:
        print(f"FAIL {label} (exit {r.returncode})")
    else:
        print(f"OK {label}")
    return r.returncode


def _agent_base_url() -> str:
    base = os.getenv("AGENT_URL", "http://127.0.0.1:9091").rstrip("/")
    return base


def _agent_healthy() -> bool:
    url = f"{_agent_base_url()}/health"
    try:
        with urllib.request.urlopen(url, timeout=3) as resp:
            return resp.status == 200
    except (urllib.error.URLError, OSError):
        return False


def run_unit_tests() -> int:
    py = sys.executable
    try:
        import pytest  # noqa: F401

        return _run([py, "-m", "pytest", str(TESTS_DIR), "-q", "--ignore", str(TESTS_DIR / "test_agent.py")], label="单元测试 (pytest)")
    except ImportError:
        failed = 0
        for f in sorted(TESTS_DIR.glob("test_*.py")):
            if f.name == "test_agent.py":
                continue
            code = _run([py, str(f.relative_to(AGENT_DIR))], label=f"单元测试 {f.name}")
            if code != 0:
                failed = code
        return failed


def run_offline_checks() -> int:
    code = _run(
        [sys.executable, str(SCRIPTS_DIR / "validate_tools_registry.py")],
        label="工具注册表校验",
    )
    if code != 0:
        return code
    return _run(
        [sys.executable, str(SCRIPTS_DIR / "seed_kb_eval.py"), "--dry-run"],
        label="KB 分块 dry-run",
    )


def run_e2e(token: str) -> int:
    if not _agent_healthy():
        print(f"\nAgent not ready ({_agent_base_url()}/health), skip E2E")
        return 1
    py = sys.executable
    env = _subprocess_env()
    if token:
        env["AGENT_EVAL_TOKEN"] = token
    print(f"\n{'=' * 60}\n>> E2E turn_cases\n{'=' * 60}")
    r1 = subprocess.run([py, str(SCRIPTS_DIR / "run_agent_eval.py")], cwd=AGENT_DIR, env=env)
    print(f"\n{'=' * 60}\n>> E2E tool_cases\n{'=' * 60}")
    cmd = [py, str(SCRIPTS_DIR / "run_tool_eval.py")]
    if token:
        cmd.extend(["--token", token])
    r2 = subprocess.run(cmd, cwd=AGENT_DIR, env=env)
    if r1.returncode != 0:
        return r1.returncode
    return r2.returncode


def run_rag_eval() -> int:
    py = sys.executable
    r1 = _run([py, str(SCRIPTS_DIR / "run_memory_eval.py")], label="Memory RAG eval")
    r2 = _run([py, str(SCRIPTS_DIR / "run_kb_rag_eval.py")], label="KB RAG eval")
    if r1 != 0:
        return r1
    return r2


def run_chat(message: str, token: str) -> int:
    cmd = [sys.executable, str(TESTS_DIR / "test_agent.py"), message]
    if token:
        cmd.append(f"--token={token}")
    return _run(cmd, label="SSE 联调")


def main() -> int:
    os.chdir(AGENT_DIR)
    if str(AGENT_DIR) not in sys.path:
        sys.path.insert(0, str(AGENT_DIR))

    p = argparse.ArgumentParser(description="Agent 测评一键入口")
    p.add_argument("--e2e", action="store_true", help="Agent 在线时跑 turn + tool E2E")
    p.add_argument("--rag", action="store_true", help="跑 memory/kb RAG 离线集（需 Qdrant）")
    p.add_argument("--token", default=os.getenv("AGENT_EVAL_TOKEN", ""), help="JWT，供 E2E user 组")
    p.add_argument("--chat", metavar="MSG", default="", help="手工 SSE 联调一句")
    p.add_argument("--skip-unit", action="store_true", help="跳过单元测试")
    args = p.parse_args()

    if args.chat:
        return run_chat(args.chat, args.token.strip())

    exit_code = 0
    if not args.skip_unit:
        exit_code = run_unit_tests() or exit_code
    exit_code = run_offline_checks() or exit_code

    if args.e2e:
        exit_code = run_e2e(args.token.strip()) or exit_code
    if args.rag:
        exit_code = run_rag_eval() or exit_code

    print(f"\n{'=' * 60}")
    print("全部完成" if exit_code == 0 else f"存在失败 (exit {exit_code})")
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
