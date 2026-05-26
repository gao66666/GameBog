#!/usr/bin/env bash
# Agent 测评一键入口
#   ./run_eval.sh
#   ./run_eval.sh --e2e --token "$AGENT_EVAL_TOKEN"
#   ./run_eval.sh --chat "现在几点"

set -euo pipefail
cd "$(dirname "$0")"
exec python run_eval.py "$@"
