# GoBlog Agent

GoBlog 项目的智能对话 Agent，基于大语言模型 + 三层记忆系统 + MCP 工具集，为博客用户提供自然语言交互助手服务。

> Agent 角色名：**小博** — 热情、简洁的博客智能助手。

---

## 目录

- [核心架构](#核心架构)
- [三阶段处理流程](#三阶段处理流程)
- [记忆系统](#记忆系统)
- [MCP 工具集](#mcp-工具集)
- [目录结构](#目录结构)
- [环境依赖](#环境依赖)
- [快速启动](#快速启动)
- [API 参考](#api-参考)
- [配置说明](#配置说明)
- [日志](#日志)
- [Demo](#demo)

---

## 核心架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                        用户请求 (POST /chat)                        │
└──────────────────────────┬──────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────────┐
│  阶段 0: Query 改写 (rag.rewrite_query)                              │
│  用 LLM 补全省略/指代/拆分复合问题，提升检索质量                      │
└──────────────────────────┬───────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────────┐
│  阶段 1: 路由选组 (agent_core._route)                                │
│  用 LLM 判断需要哪个 MCP Server 组: [public] / [user] / [public,user]│
└──────────────────────────┬───────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────────┐
│  阶段 2: Agent 执行 (agent_core.build_agent + agent.astream_events)  │
│  加载选中组的 MCP 工具 + 上下文(短/中/长期记忆) → SSE 流式输出       │
└──────────────────────────┬───────────────────────────────────────────┘
                           │
                           ▼
               ┌───────────────────────┐
               │  三层记忆自动落盘      │
               │  短期→中期→长期 渐进归档 │
               └───────────────────────┘
```

## 三阶段处理流程

### 阶段 0 — Query 改写 (`rag.py`)

将用户原始问题用 LLM 改写为更适合检索的表述：
- 补全省略和指代（如"那个游戏" → 具体名称）
- 使用标准术语
- 拆分复合问题为多个检索要点

### 阶段 1 — 路由选组 (`agent_core.py`)

两阶段 Agent 设计的关键：路由阶段**不加载全部工具**，只给 LLM 两组 Server 的摘要描述，LLM 决策成本低，返回需要哪几组：

| Server 组 | 摘要说明                                                                 |
| --------- | ------------------------------------------------------------------------ |
| `public`  | 公开只读：搜索文章、文章详情/排行榜/评论、话题、游戏库、用户公开资料     |
| `user`    | 用户授权（需 token）：我的资料、我的文章、收藏、关注话题、游戏记录、积分余额/流水/兑换记录 |

路由时的额外处理：
- 若未传入 `token`，`user` 组不可用
- 路由结果为空则 Agent 退化为纯闲聊模式

### 阶段 2 — Agent 执行 (`agent_core.py`)

根据路由结果加载对应的 MCP 工具 + `get_current_time` 内置工具，构建 Agent，注入上下文后流式执行。

**上下文组装**（按优先级）：
1. **长期记忆** — 来自 MongoDB（结构化事实）+ Qdrant（向量经验），按相关度排序
2. **中期摘要** — 来自 Redis，之前轮次的 LLM 摘要
3. **短期对话** — 最近几轮用户/助手原文
4. **当前问题** — 改写后的 query

**流式事件**（SSE `data:` 格式）：

| 事件类型     | 说明                     |
| ------------ | ------------------------ |
| `session`    | Session ID               |
| `rewrite`    | 改写结果 + 耗时          |
| `route`      | 路由结果 + 耗时          |
| `token`      | LLM 输出 token 流        |
| `tool_start` | 工具调用开始             |
| `tool_end`   | 工具调用结束（结果预览） |
| `done`       | 完成                     |
| `error`      | 异常（含 traceback）     |

---

## 记忆系统

三层渐进式记忆架构，从短期（Session）到长期（持久化）自动流转。

### 短期记忆 — Redis List

| 属性 | 值                                         |
| ---- | ------------------------------------------ |
| 存储 | Redis List（`agent:short:{user_id}:list`） |
| TTL  | 默认 60 分钟                               |
| 容量 | 最近 12 条对话（`short_window_messages`）  |
| 用途 | 当前会话上下文，原文保留                   |

超出容量时：**顶部 N 条被 LLM 压缩为中期摘要**，然后从短期中弹出。

### 中期记忆 — Redis List

| 属性 | 值                                               |
| ---- | ------------------------------------------------ |
| 存储 | Redis List（`agent:mid:{user_id}:summary:list`） |
| TTL  | 默认 7 天                                        |
| 容量 | 最多 20 条摘要                                   |
| 格式 | `{topic, stage, pending, subtask, facts}`        |
| 用途 | 跨短期会话的上下文摘要                           |

`stage` 取值：`提问 / 信息收集中 / 执行中 / 答疑中 / 已完结`

### 长期记忆 — MongoDB + Qdrant

| 属性     | MongoDB                                                                | Qdrant                 |
| -------- | ---------------------------------------------------------------------- | ---------------------- |
| 存储内容 | 结构化事实（规则/身份/偏好/状态）                                      | 经验/案例/背景（向量） |
| 字段     | `kind`(profile/rule/preference)、`content`、`importance`、`confidence` | 文本向量 + payload     |
| 用途     | 精确事实查询                                                           | 相似度语义检索         |

**触发判定**（`should_trigger_long_term_by_prompt`）：LLM 判断对话是否有长期保留价值。

**提取写入**（`_extract_memory_items` → `_store_memory_items`）：
1. LLM 从对话中提取原子记忆条目（最多 5 条），标记去向（mongo/qdrant）
2. **决策引擎**（`decision_llm`）对比已有记忆，决定每条的操作：
   - Mongo：`add` / `overwrite` / `delete` / `ignore`
   - Qdrant：`add` / `lower_confidence` / `ignore`

**召回**（`get_long_term_context`）：Mongo 精确查询 + Qdrant 向量检索 → 合并去重 → 按 `score` 排序。

**触发时机**：
- `auto` — 每轮对话完成后自动评估
- `session_end` — 会话结束时显式触发（`POST /memory/session-end`）
- `timeout` — Session 超时
- `explicit_remember` — 用户明确说"记住"

### Session 管理

| 功能     | 说明                                           |
| -------- | ---------------------------------------------- |
| 创建     | `create_session()` — 首次请求时自动创建        |
| 续期     | `refresh_session()` — 每次请求续期 TTL         |
| 结束     | `clear_session()` — 配合长期记忆 commit        |
| 空闲超时 | 默认 30 分钟（`session_idle_timeout_minutes`） |

---

## MCP 工具集

### Public — 公开只读（`mcp_public.py`）

无需鉴权，覆盖浏览场景：

| 工具                      | 说明                          |
| ------------------------- | ----------------------------- |
| `search_articles`         | 全文搜索文章                  |
| `get_article_detail`      | 文章详情（正文截断 2000 字）  |
| `list_latest_articles`    | 最新文章列表                  |
| `get_article_leaderboard` | 排行榜（view/like）           |
| `get_article_comments`    | 文章评论                      |
| `list_all_topics`         | 话题列表                      |
| `get_topic_detail`        | 话题详情                      |
| `get_topic_articles`      | 话题下的文章                  |
| `get_topic_discussions`   | 话题下的讨论                  |
| `list_all_games`          | 游戏库列表                    |
| `get_game_detail`         | 游戏详情                      |
| `get_game_reviews`        | 游戏玩家点评                  |
| `get_user_public_profile` | 用户公开资料（不含手机/邮箱） |

### User — 用户授权（`mcp_user.py`）

需要 JWT token，通过 `RunnableConfig` 注入（LLM 不可见，安全隔离）：

| 工具                     | 说明         |
| ------------------------ | ------------ |
| `me_get_profile`         | 我的个人资料 |
| `me_get_articles`        | 我的文章列表 |
| `me_get_collections`     | 我的收藏     |
| `me_get_followed_topics` | 我关注的话题 |
| `me_get_game_plays`      | 我的游戏记录 |
| `me_get_wallet`          | 我的积分余额 |
| `me_get_points_transactions` | 积分流水（分页） |
| `me_get_mall_orders`     | 积分商城兑换记录 |
| `me_get_game_orders`     | 游戏库购买记录 |

### MCP Stdio Server（`mcp_server.py`）

独立的 MCP 协议实现，可通过 `mcp` CLI 或 `langchain_mcp_adapters` 连接，提供与 LangChain Agent 相同的工具集。

---

## 目录结构

```
agent/
├── main.py                    # FastAPI 入口 (POST /chat, /memory/*)
├── run_eval.py                # 测评一键入口
├── run_eval.ps1 / run_eval.sh
├── requirements.txt
├── README.md
│
├── config/                    # 静态配置（改完 prompts 可热加载）
│   ├── config.yaml            # LLM / Redis / Mongo / Qdrant 等
│   └── prompts.yaml           # 路由 / analyse / plan / output 提示词
│
├── core/                      # 对话主链路
│   ├── agent_core.py          # 路由选组 + Agent 构建 + 工具执行
│   ├── rag.py                 # Query 改写
│   ├── planning_round.py      # analyse → plan → execute → output
│   ├── reflection.py          # 长期记忆召回 / 落盘
│   ├── stream_emitter.py      # SSE v1 fact 帧
│   ├── token_usage.py         # 各阶段 token 聚合
│   └── chat_phases.py         # 阶段枚举
│
├── memory/                    # 三层记忆
│   ├── memory_manager.py      # MongoDB + Qdrant + KB 检索
│   ├── memory_store.py        # Redis 短/中期 + Session
│   └── memory_turn.py         # 每轮落盘与摘要触发
│
├── tools/                     # MCP 工具与博客 API
│   ├── tool_runtime.py        # tools_registry.json 驱动
│   ├── tool_retrieval.py      # 工具向量检索
│   ├── mcp_public.py / mcp_user.py / mcp_general.py
│   ├── mcp_server.py          # 独立 MCP stdio Server
│   ├── blog_client.py         # Go 后端 HTTP 客户端
│   └── general_client.py      # 时间 / 计算 / 字数（本地）
│
├── infra/                     # 基础设施
│   ├── config.py              # 配置加载
│   ├── agent_log.py           # 日志（写入 ../logger/agent_log/）
│   └── agent_metrics.py       # Prometheus 指标
│
├── audit/                     # 生产 turn 抽样审计
│   └── turn_audit.py
│
├── eval/                      # 打分器 + metrics_spec.yaml
├── cases/                     # 测评用例 YAML + KB 语料
├── scripts/                   # 测评 / seed / audit 脚本
├── tests/                     # 单元测试 + SSE 联调
└── demos/                     # LangChain 示例（非生产路径）
```

---

## 环境依赖

| 服务              | 用途                        | 必需 |
| ----------------- | --------------------------- | ---- |
| Redis             | 短期/中期记忆、Session 存储 | ✅    |
| MongoDB           | 长期记忆（结构化事实）      | ✅    |
| Qdrant            | 长期记忆（向量检索）        | ✅    |
| GoBlog 后端       | 博客业务 API                | ✅    |
| 智谱 API / DeepSeek / OpenAI | LLM 推理、Embedding（智谱） | ✅    |

注意：`config/config.yaml` 内包含密钥配置，请替换为你自己的 key，避免在公共仓库暴露。

---

## 快速启动

### 1. 启动依赖服务

```bash
# 使用项目根目录的 docker-compose
cd ..
docker compose up -d redis mongodb qdrant

# 启动 Go 后端
go run main.go
```

### 2. 配置 LLM

编辑 `agent/config/config.yaml`。对话默认 **DeepSeek**（`deepseek-chat`），密钥推荐环境变量：

```powershell
set DEEPSEEK_API_KEY=sk-...
# 向量 embedding 仍走智谱（DeepSeek 无 embedding API）
set ZHIPU_API_KEY=...
python main.py
```

如需连接不同后端环境，请修改 `blog_api_url`。

### 3. 启动 Agent

```bash
cd agent

# 创建虚拟环境（推荐）
python -m venv .venv
.venv\Scripts\activate  # Windows
# source .venv/bin/activate  # Linux/Mac

# 安装依赖
pip install -r requirements.txt

# 启动
python main.py
# → http://0.0.0.0:9091
```

### 4. 试用

```bash
# CLI 测试（只读，不带用户 token）
# 一键离线测评（单元测试 + 注册表 + KB dry-run）
python run_eval.py

# Agent 在线时加 E2E
python run_eval.py --e2e --token=xxx

# 手工 SSE 联调
python tests/test_agent.py "有什么好玩的游戏"
python tests/test_agent.py "我的积分有多少" --token=xxx

# CLI 测试（完整功能，带用户 token）
python tests/test_agent.py "我叫张三，喜欢RPG游戏" --token=xxx

# 直接调 HTTP
curl -N -X POST http://127.0.0.1:9091/chat ^
  -H "Content-Type: application/json" ^
  -d "{\"message\":\"推荐几个游戏\",\"user_id\":1,\"token\":\"\"}"
```

### 5. 验证记忆系统

```bash
# 第一轮：记住用户信息
python test_agent.py "我叫张三，喜欢RPG游戏" --token=xxx
# 第二轮：从长期记忆召回
python tests/test_agent.py "我上次说自己喜欢什么类型的游戏" --token=xxx
```

---

## API 参考

### `POST /chat`

请求体：

```json
{
  "message": "用户问题",
  "user_id": 1,
  "token": "JWT token（可选，不传则无 user 工具）"
}
```

响应：SSE 流式（`text/event-stream`），事件见[阶段 2 事件表](#阶段-2--agent-执行)。

### `POST /memory/session-end`

手动触发长期记忆写入 + 清理 Session：

```json
{
  "user_id": 1,
  "reason": "session_end"
}
```

### `GET /health`

健康检查：`{"status": "ok"}`

---

## 配置说明

完整配置见 `config/config.yaml`，通过 `infra/config.py` 的 `load_config()` 加载。

### LLM 配置段

| 配置段               | 用途                           | 默认模型      |
| -------------------- | ------------------------------ | ------------- |
| `llm`                | 主 Agent 推理                  | `deepseek-chat` |
| `summary_llm`        | 中期摘要生成                   | `deepseek-chat` |
| `memory_manager_llm` | 长期记忆提取/判定              | `deepseek-chat` |
| `decision_llm`       | 长期记忆决策（新增/覆盖/忽略） | `deepseek-chat` |
| `embedding`          | 向量检索（智谱 embedding-3）   | 见 `ZHIPU_API_KEY` |
| `embedding`          | Qdrant 向量化                  | `embedding-3` |

常用配置段：

| 配置段          | 说明                                                    |
| --------------- | ------------------------------------------------------- |
| `blog_api_url`  | Go 后端 API 地址（默认 `http://127.0.0.1:8084/api/v1`） |
| `redis`         | 短/中期记忆与 Session 存储                              |
| `mongo`         | 长期记忆结构化存储                                      |
| `qdrant`        | 长期记忆向量检索                                        |
| `memory`        | 记忆窗口/TTL/检索策略                                   |
| `orchestration` | 改写-路由-工具检索编排策略                              |

各段均可独立配置 `base_url` / `api_key` / `model` / `temperature` / `thinking`。

### Memory 参数

| 参数                           | 默认值 | 说明                     |
| ------------------------------ | ------ | ------------------------ |
| `short_ttl_minutes`            | 60     | 短期记忆过期时间         |
| `mid_ttl_days`                 | 7      | 中期摘要过期时间         |
| `mid_max_items`                | 8      | 中期摘要最大条数         |
| `short_window_messages`        | 12     | 短期容量，超过后触发压缩 |
| `long_term_retrieval_limit`    | 6      | 每次召回长期记忆条数     |
| `session_idle_timeout_minutes` | 30     | Session 空闲超时         |
| `summary_enabled`              | true   | 是否启用中期摘要         |

### 数据服务

| 段       | 默认地址                                |
| -------- | --------------------------------------- |
| `redis`  | `localhost:6379`                        |
| `mongo`  | `mongodb://root:123456@localhost:27017` |
| `qdrant` | `http://127.0.0.1:6333`                 |

### 环境变量

| 变量                     | 说明                                  |
| ------------------------ | ------------------------------------- |
| `REDIS_HOST`             | Redis 地址（Docker 内通常为 `redis`） |
| `REDIS_PORT`             | Redis 端口                            |
| `REDIS_PASSWORD`         | Redis 密码                            |
| `REDIS_DB`               | Redis DB 索引                         |
| `DOCUMENT_INGEST_SECRET` | 文档导入密钥（可选）                  |

---

## 日志

使用 `agent_log.py` 模块，日志输出到 `logger/agent_log/` 目录：

| 特性   | 说明                                                                           |
| ------ | ------------------------------------------------------------------------------ |
| 切割   | 按天自动切割（`TimedRotatingFileHandler`）                                     |
| 保留   | 保留 7 天                                                                      |
| 格式   | `%(asctime)s [%(levelname)s] %(name)s: %(message)s`                            |
| 输出   | 文件（DEBUG）+ 控制台（INFO）                                                  |
| Logger | `main` — 业务日志 / `agent_error` — 异常日志 / `blog_client` — HTTP 客户端日志 |

---

## Demo

为方便快速体验，提供了两个独立 Demo（不依赖 Redis/Mongo/Qdrant）：

### `agent_demo.py` — 交互式

```bash
python agent_demo.py
```

多轮对话，带 ConversationBufferMemory，支持计算器/时间/单词长度三个工具。

### `agent_demo_simple.py` — 最简版

```bash
python agent_demo_simple.py
```

单次调用，演示 `create_agent()` + `astream_events` 基本用法。
