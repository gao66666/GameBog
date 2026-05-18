# GameBog Vue 前端

全站页面已迁移至 Vue 3。**推荐用 Docker 跑 Node/npm**，无需本机安装。

## Docker 开发（推荐）

在项目根目录：

```bash
# 起 MySQL / Redis / Kafka / NSQ + Go 后端 + Vite 前端
docker compose --profile dev up -d
```

| 服务 | 地址 |
|------|------|
| 前端（Vite 热更新） | http://127.0.0.1:5173 |
| 后端 API / 构建后 SPA | http://127.0.0.1:8084 |

只构建一次 dist（让 8084 直接出 Vue 页面，不走 5173）：

```bash
docker compose --profile dev run --rm webapp-build
```

然后刷新 http://127.0.0.1:8084 即可。

## 本机开发（可选）

需安装 Node.js 18+ 与 npm：

```bash
cd webapp
npm install
npm run dev
```

Go：`go run .`（勿与 `goblog-dev` 容器同时占用 8084）。

## 生产镜像

`docker compose --profile app build app` 会在 Dockerfile 内自动 `npm run build`，无需单独构建前端。
