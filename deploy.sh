#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

# 基础检查
command -v docker >/dev/null 2>&1 || { echo "docker 未安装"; exit 1; }
docker compose version >/dev/null 2>&1 || { echo "docker compose 不可用（需要 docker compose v2）"; exit 1; }

echo "[1/2] 构建并启动容器（含 GoBlog Golang 服务）"
docker compose up -d --build --remove-orphans

echo "[2/2] 当前容器状态"
docker compose ps

echo
echo "访问: http://<你的服务器IP>:8084"
echo "日志: docker compose logs -f app"
