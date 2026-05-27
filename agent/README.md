# GoBlog Agent

GoBlog 项目的智能对话 Agent，基于大语言模型 + 三层记忆系统 + MCP 工具集，为博客用户提供自然语言交互助手服务。

> Agent 角色名：**小博** — 热情、简洁的博客智能助手。

---

## 目录

- [核心架构](#核心架构)
- [单轮编排流程](#单轮编排流程)
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

入口：**Web** `POST /chat`（经 Go ChatProxy SSE），或 **IM** 各平台 webhook → Go `handler/channel` → 同一 Agent 链路。

```
用户消息
    → Query 改写
    → 路由选组（public / user / general）+ memory_hints
    → Hybrid 工具检索（组内向量 + 词面 Top-K）
    → analyse（四态 disposition）
         ├─ answer / clarify / cannot → 直接 output
         └─ proceed → 长期记忆检索 → plan → execute（MCP 工具）
              → output 成稿 → SSE 流式答复
    → 短期/中期落盘 + 异步长期记忆沉淀
```

与 Go 后端：Gin 负责 JWT、限流、Redis 会话；`AGENT_URL` HTTP/SSE 代理至本服务，结构化 fact 由 Go `PresentationTranslator` 译为中文进度（见项目根目录 [readme.md](../readme.md) 多渠道 IM 配置）。

## 单轮编排流程

### Query 改写 (`core/rag.py`)

- 补全省略、指代，复合问题用 `；` 拆成多检索要点（供 Dense 分路向量检索）
- 输出 `memory_hints` 供 Sparse（BM25）与路由侧使用

### 路由选组 (`core/agent_core.py`)

路由阶段**不加载全部工具**，只选 MCP 组并给出 `tool_hints` / `memory_hints`：

| Server 组 | 说明 |
| --------- | ---- |
| `public`  | 文章/话题/游戏库等公开只读 API |
| `user`    | 需 JWT：我的资料、收藏、积分等 |
| `general` | 时间、计算、字数等本地工具 |

未传 `token` 时 `user` 组不可用。

### Hybrid 工具检索 (`tools/tool_retrieval.py`)

在已选组内用「改写句 + tool_hints」做 embedding 与词面 Hybrid 召回，执行阶段再注入完整 tool schema（`dynamic_tool_injection`）。

### analyse → plan → execute → output (`core/planning_round.py`)

**analyse disposition（四态，单轮无 replan/close）**：

| disposition | 行为 |
|-------------|------|
| `answer`    | 无需工具/记忆，直接成稿 |
| `clarify`   | 追问用户 |
| `cannot`    | 说明无法完成 |
| `proceed`   | 检索记忆 → 计划 → 调工具 → output 综合成稿 |

`proceed` 路径：`retrieve_long_term_for_turn`（Mongo + Qdrant Hybrid，含公共游戏 Wiki KB）→ `run_plan` → `stream_tool_execution` → `run_output`。

### SSE 阶段（fact 帧，见 `core/stream_emitter.py`）

| phase | 说明 |
|-------|------|
| `session` | 会话 ID |
| `rewrite` | 改写结果 |
| `route` | 路由组 + hints |
| `tool_retrieve` | 召回的工具名列表 |
| `analyse` | disposition + 分析摘要 |
| `retrieve` | 长期记忆/KB 命中 |
| `plan` | 计划步骤 |
| `tool_invoke` | 工具开始/结束 |
| `output` / `answer` | 最终回复流 |
| `done` | 耗时与 token 统计 |

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
│   ├── rag.py                 # Query 改写 + 多意图拆分
│   ├── planning_round.py      # analyse（四态）→ plan → execute → output
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

本项目 Python 环境统一使用 conda 环境 **`langchain_agent`**（与 `run_eval.ps1` 一致）。

```powershell
cd agent
conda activate langchain_agent
# 或直接使用: M:\conda_envs\langchain_agent\python.exe

pip install -r requirements.txt
python main.py
# → http://0.0.0.0:9091
```

### 4. 试用

```powershell
conda activate langchain_agent

# 一键测评（离线默认；Agent :9091 在线时加 --e2e）
python run_eval.py
python run_eval.py --e2e --token=xxx

# 30 轮对话 E2E（15 短 + 15 长，完整链路 + 工具 + KB RAG）
python scripts/run_dialogue_metrics_eval.py --token <JWT>
```

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

响应：SSE 流式（`text/event-stream`），阶段见[单轮编排流程](#单轮编排流程)中的 SSE 表。

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
| `orchestration` | 工具检索、`max_orchestration_cycles`（默认 1，单轮）   |

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
