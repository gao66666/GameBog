# GoBlog

基于 **Gin + GORM + Redis + NSQ + Kafka + Elasticsearch** 的社区论坛，含 Vue 前端与 Python AI 助手。

## 界面预览

| 首页 | 文章详情 |
|:---:|:---:|
| ![首页](sample/首页展示.png) | ![文章详情](sample/文章详情页展示.png) |

| 游戏库 | 个人中心 |
|:---:|:---:|
| ![游戏库](sample/游戏库展示.png) | ![个人中心](sample/个人中心展示.png) |

![AI 助手](sample/个人助手展示.png)

## 功能概览

- 用户注册登录、JWT 鉴权、关注与粉丝
- 文章发布/编辑/搜索，点赞与阅读异步统计（NSQ + Redis + MySQL）
- 评论、私信、话题与游戏库
- 通知：Kafka 异步 + WebSocket 实时推送，离线补偿
- 全文检索（Elasticsearch），热门与排行榜（Redis ZSet）
- AI 个人助手（`agent/`，对接博客 MCP 与 RAG）

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go、Gin、GORM、MySQL |
| 缓存/队列 | Redis、NSQ、Kafka |
| 搜索 | Elasticsearch |
| 前端 | Vue 3、Vite、Element Plus |
| 部署 | Docker Compose、Nginx |

## 目录结构

`bootstrap` · `router` · `handler` · `service` · `database` · `mq` · `middleware` · `setting` · `webapp` · `agent`

## 快速启动

**仅依赖：**

```bash
docker compose up -d
go run main.go
```

**全栈开发（Go + Vue 热更新，无需本机 Node）：**

```bash
docker compose --profile dev up -d
```

- 前端：http://127.0.0.1:5173  
- 后端：http://127.0.0.1:8084  

**生产/容器一键：**

```bash
docker compose --profile app up -d --build
```

健康检查：`GET /healthz`、`GET /readyz`

## 配置

主配置：`setting/common.yaml`。生产环境务必覆盖 `auth.jwt_secret`。

常用项：`mq.nsq.enabled`、`mq.kafka`、搜索与 Agent 相关配置见 `agent/config.yaml`。

API 前缀 `/api/v1`，鉴权头 `Authorization: Bearer <token>`。

## 测试

```bash
go test ./...
go vet ./...
```

数据迁移见 `database/migrations/README.md`。
