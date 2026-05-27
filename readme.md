# GameBog

基于 **Gin + GORM + Redis + NSQ + Kafka + Elasticsearch** 的社区和商城后端，含 Vue 前端与 Python AI 助手。

网页链接地址:http://49.235.172.68:8084

## 功能概览

- 用户注册登录、JWT 鉴权、关注与粉丝
- 文章发布/编辑/搜索，点赞与阅读异步统计（NSQ + Redis + MySQL）
- 评论、私信、话题与游戏库
- 通知：Kafka 异步 + WebSocket 实时推送，离线补偿
- 积分系统 & **积分商城**：赚取积分 → 兑换商品（Redis Lua 扣库存 + MySQL 占码）
- 全文检索（Elasticsearch），热门与排行榜（Redis ZSet）
- AI 个人助手（`agent/`，对接博客 MCP 与 RAG）
- 多渠道 IM 机器人（飞书 / Slack / 企业微信 / 钉钉，可选）

## 技术栈

| 层        | 技术                      |
| --------- | ------------------------- |
| 后端      | Go、Gin、GORM、MySQL      |
| 缓存/队列 | Redis、NSQ、Kafka         |
| 搜索      | Elasticsearch             |
| 前端      | Vue 3、Vite、Element Plus |
| 部署      | Docker Compose、Nginx     |

## 目录结构

`bootstrap` · `router` · `handler` · `service` · `database` · `mq` · `middleware` · `setting` · `webapp` · `agent`

## 服务与端口

| 服务                | 地址                  |
| ------------------- | --------------------- |
| 后端 API            | http://127.0.0.1:8084 |
| 前端（Vite 热更新） | http://127.0.0.1:5173 |
| NSQ Admin           | http://127.0.0.1:4171 |
| Qdrant              | http://127.0.0.1:6333 |
| MongoDB             | 127.0.0.1:27017       |
| Redis               | 127.0.0.1:6379        |
| MySQL               | 127.0.0.1:3306        |
| Kafka               | 127.0.0.1:9092        |

## 快速启动

**启动依赖服务：**

```bash
docker compose up -d
```

**本机启动 Go 后端：**

```bash
go run .
```

**全栈开发（Go + Vue 热更新，无需本机 Node）：**

```bash
docker compose --profile dev up -d
docker compose --profile dev up -d --force-recreate dev
```

- 前端：http://127.0.0.1:5173  
- 后端：http://127.0.0.1:8084  

新增/修改 Go 接口后需重建 dev 容器：`docker compose --profile dev up -d --force-recreate dev`

**生产/容器一键：**

```bash
docker compose --profile app up -d --build
```

健康检查：`GET /healthz`、`GET /readyz`

**AI 助手：** 见 [agent/README.md](agent/README.md)

## 配置

主配置：`setting/common.yaml`。生产环境务必覆盖 `auth.jwt_secret`。

常用项：`mq.nsq.enabled`、`mq.kafka`、搜索与 Agent 相关配置见 `agent/config/config.yaml`。

### 多渠道 IM 机器人（可选）

Go 侧统一收发层：`handler/channel`（Hub + Adapter），将各平台 webhook 归一化后调用 Python Agent（`AGENT_URL`），再异步回发平台。无需 JWT；平台用户映射为合成 `user_id`（Redis），会话与 Web 侧边栏按 `chat_id` / 频道隔离。

| 通道 | Webhook 路径 | 启用条件（环境变量） |
|------|----------------|----------------------|
| 飞书 | `POST /api/v1/channel/feishu/webhook`（兼容 `/channel/feishu/event`） | `FEISHU_APP_ID` + `FEISHU_APP_SECRET` |
| Slack | `POST /api/v1/channel/slack/webhook` | `SLACK_BOT_TOKEN` + `SLACK_SIGNING_SECRET`；群聊 @ 需 `SLACK_BOT_USER_ID` |
| 企业微信 | `GET/POST /api/v1/channel/wecom/webhook` | `WECOM_CORP_ID`、`WECOM_AGENT_ID`、`WECOM_SECRET`、`WECOM_TOKEN`、`WECOM_AES_KEY` |
| 钉钉 | `POST /api/v1/channel/dingtalk/webhook` | `DINGTALK_APP_SECRET`（Outgoing 机器人加签） |

**飞书**：订阅 `im.message.receive_v1`；单聊直接发文字，群聊需 @ 机器人。可选 `FEISHU_VERIFICATION_TOKEN`、`FEISHU_ENCRYPT_KEY`。

**Slack**：Event Subscriptions 指向 webhook；需 `chat:write` 等 Bot 权限；`url_verification` 由适配器自动响应。

**企业微信**：自建应用「接收消息」回调 URL 填 webhook；GET 用于 URL 校验（解密 echostr），POST 为加密 XML。

**钉钉**：自定义机器人开启 **Outgoing**，消息接收地址填 webhook；回复走回调中的 `sessionWebhook`。

以上通道均需 Go 后端配置 `AGENT_URL` 且 Agent 进程可用。

搜索服务默认关闭，如需启用 Elasticsearch，请参考 [docker-compose.yml](docker-compose.yml) 中注释的 elasticsearch 服务。

API 前缀 `/api/v1`，鉴权头 `Authorization: Bearer <token>`。

### 后台内部 API（不经前端）

配置 `ADMIN_API_SECRET`（或 `security.admin_api_secret`），请求头 `X-Admin-Key: <secret>`：

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/internal/games` | 上新游戏，自动创建并绑定「游戏:名称」话题；返回 `game_id`、`topic_id` |
| POST | `/api/v1/internal/topics` | 创建长期话题；body `{ "name": "话题名" }` |
| POST | `/api/v1/internal/kb/markdown` | 将 Markdown 分片写入公共 Qdrant（代理 Agent）；需 `AGENT_URL`，建议同时配置 `DOCUMENT_INGEST_SECRET` |

`kb/markdown` body 示例：`article_id`、`markdown`、可选 `game_name`、`source`、`content_revision`、`chunk_size`。

## AI 智能助手

项目包含一个完整的 Python AI 助手（`agent/`），基于大语言模型 + 三层记忆系统 + MCP 工具集，为博客用户提供自然语言交互体验。

核心特性：
- **单轮编排**：Query 改写 → 路由选组 → Hybrid 工具检索 → analyse（四态）→ plan → execute → output 成稿（SSE 流式）
- **三层渐进记忆**：Redis 短期 → LLM 中期摘要 → MongoDB + Qdrant 长期记忆（同轮异步沉淀）
- **MCP 工具集**：public / user / general 分组，路由轻量选组 + 向量/词面召回候选工具
- **Hybrid RAG**：Dense（改写分意图）+ Sparse（memory_hints）归一化加权融合；KB 按 Markdown 标题 + 递归切分
- **可观测性**：`request_id` 贯穿、结构化 fact 帧、端到端测评门禁、Prometheus 指标

> 详细文档、启动方式、API 参考、配置说明见 **[agent/README.md](agent/README.md)**

## 测试

```bash
go test ./...
go vet ./...
```

数据迁移见 `database/migrations/README.md`。
