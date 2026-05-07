# GoBlog Agent

GoBlog 的智能对话 Agent 服务：大语言模型 + **路由选工具** + **LangChain 工具循环（ReAct 风格）** + **短/中/长期记忆** + **MCP 语义工具集**，通过 SSE 向博客前端或网关提供流式回复。

> 角色名：**小博** — 热情、简洁的博客助手。

---

## 目录

- [架构总览](#架构总览)
- [三阶段请求流水线](#三阶段请求流水线)
- [执行阶段：上下文如何拼接](#执行阶段上下文如何拼接)
- [记忆系统](#记忆系统)
- [Qdrant 混合检索（长期向量）](#qdrant-混合检索长期向量)
- [文档入库（Wiki / 百科块）](#文档入库wiki--百科块)
- [MCP 工具集](#mcp-工具集)
- [目录结构](#目录结构)
- [环境依赖](#环境依赖)
- [快速启动](#快速启动)
- [API 参考](#api-参考)
- [配置说明](#配置说明)
- [日志](#日志)
- [Demo](#demo)
- [设计定位与理性评价](#设计定位与理性评价)

---

## 架构总览

```
用户 POST /chat
    │
    ▼
┌─────────────────────────────────────┐
│ 阶段 0  Query 改写 (rag.rewrite_query)   │  ← 仅用于长期记忆检索查询；用户原文仍进 Agent
└─────────────────┬───────────────────┘
                  ▼
┌─────────────────────────────────────┐
│ 阶段 1  路由选组 (agent_core._route)     │  ← 选 public / user / 组合 / 空（闲聊）
└─────────────────┬───────────────────┘
                  ▼
┌─────────────────────────────────────┐
│ 阶段 2  build_agent + astream_events    │  ← 工具 + 上下文，SSE 输出
└─────────────────┬───────────────────┘
                  ▼
        短期 / 中期滚动、长期记忆按需提交
```

---

## 三阶段请求流水线

### 阶段 0 — Query 改写（`rag.py`）

用 LLM 将用户问题改写成更适合 **长期记忆向量检索** 的表述（补全省略、标准化术语等）。

- **注意**：改写结果用于 `get_long_term_context(..., query=rewritten)`；**当前轮发给 Agent 的用户消息仍是原始 `message`**，不是改写句。

### 阶段 1 — 路由选组（`agent_core.py`）

两阶段设计：**路由阶段不加载全部工具**，只给两组 MCP（`public` / `user`）的摘要，由 LLM 输出 JSON 选择需要的组。

| 组 | 说明 |
| --- | --- |
| `public` | 公开只读：文章、话题、游戏库、用户公开资料等 |
| `user` | 需 JWT：我的资料、文章、收藏、关注、游戏记录、积分等 |

- 未传 `token` 时不能使用 `user`。
- 路由结果为空时，工具较少，偏闲聊。

### 阶段 2 — Agent 执行（`agent_core.py` + `main.py`）

`create_agent` 注入 **系统提示**（角色与行为规则，含复杂任务分步、何时结束工具调用等），加载选中工具与 `get_current_time`，对组装好的 `messages` 做 **流式事件**（`on_chat_model_stream` / `on_tool_*`）。

**SSE 事件类型**（节选）：

| 类型 | 含义 |
| --- | --- |
| `session` | Session 信息 |
| `rewrite` | 改写结果与耗时 |
| `route` | 路由结果与耗时 |
| `token` | 模型输出片段 |
| `tool_start` / `tool_end` | 工具调用 |
| `done` / `error` | 结束或错误 |

---

## 执行阶段：上下文如何拼接

`main.py` 中传入模型的 `messages` **顺序**为：

1. （可选）**中期记忆**：`system`，内容为 `中期记忆(JSON 列表):` + Redis 中期摘要 JSON  
2. （可选）**长期记忆**：`system`，内容为 `长期记忆(JSON 列表):` + Mongo + Qdrant 合并后的召回 JSON  
3. **短期对话**：最近若干轮 `user`/`assistant`（Redis 或网关注入的 `conversation_history`）  
4. **当前用户输入**：`{"role":"user","content": message}`  

此外，**框架层**另有一条 **Agent 系统提示**（`SYSTEM_PROMPT`），与上述列表共同构成完整指令上下文。顺序微调可能对侧重点有轻微影响；**当前用户句宜保持在末尾**。

---

## 记忆系统

三层渐进：**短期（Redis List）→ 中期（Redis 摘要）→ 长期（Mongo + Qdrant）**。短期超窗时由摘要模型压缩并滚动到中期。

### 长期记忆 — MongoDB + Qdrant

| 存储 | 内容侧重 |
| --- | --- |
| MongoDB | 结构化事实：`profile` / `rule` / `preference` 等 |
| Qdrant | 向量 + payload：对话抽取的经验句段、以及下文所述 **文档块** |

**写入路径**

1. **对话抽取**：触发判定 → `_extract_memory_items` → `decision_llm` 决策 → 写入。Qdrant 侧点带 `memory_scope=dialogue`（及个人 `user_id`）。
2. **文档入库**：HTTP `POST /memory/ingest-document`，写入全局知识：`user_id=0`、`memory_scope=document`，带 `article_id`、`chunk_index`、`content_revision`、`source`、`ingest_kind` 等 payload。

**召回（`get_long_term_context`）**

- `user_id > 0`：**Mongo 本人事实** + **Qdrant（本人 dialogue ∪ 全局 document）**。  
- `user_id ≤ 0`：仅 **Qdrant 全局文档库**（供未登录时也能检索百科类片段）。

Mongo 侧当前实现为按用户拉取活跃事实批次；与 Qdrant 结果按 **`score` 合并排序**后截断。

---

## Qdrant 混合检索（长期向量）

默认 **`memory.hybrid_search: true`**（可在 `config.yaml` 关闭退回纯向量）。

1. **向量路**：对用户查询（改写句用于召回）做 embedding，在 scope 过滤下 `search` 一批候选。  
2. **可选字面路**（`hybrid_keyword_recall`）：对 `payload.content` 使用 **`MatchTextAny`** + `scroll`，多捞字面命中的块（单次有 `limit`，非全表拉回 Python）。  
3. 两路 **点 ID 去并**（有上限），`retrieve` 后计算 **dense（余弦）** 与 **keyword（原句拆 token 重合）**，归一化后加权融合，再乘 confidence 与时间衰减得到最终 `score`。

权重与候选规模见配置项：`hybrid_dense_weight`、`hybrid_keyword_weight`、`hybrid_prefetch_limit`、`hybrid_max_union_points`。

---

## 文档入库（Wiki / 百科块）

`POST /memory/ingest-document`：将单块正文写入 Qdrant 全局文档库；同一 `article_id + chunk_index + content_revision` **upsert 覆盖**。

若配置了 **`document_ingest_secret`**（或环境变量 `DOCUMENT_INGEST_SECRET`），请求需带 **`X-Ingest-Key`**。

请求体字段参见下文 [API 参考](#api-参考)。

---

## MCP 工具集

### Public（`mcp_public.py`）

搜索文章、文章详情/排行榜/评论、话题与讨论、游戏库与点评、用户公开资料等（只读）。

### User（`mcp_user.py`）

需 JWT；个人资料、我的文章、收藏、关注话题、游戏记录、积分等。Token 通过 `RunnableConfig` 注入，不暴露给模型提示词。

### Stdio（`mcp_server.py`）

独立 MCP 服务进程，工具能力与 LangChain 侧对齐，便于外部客户端接入。

---

## 目录结构

```
agent/
├── main.py              # FastAPI：/chat、/memory/session-end、/memory/ingest-document、/health
├── config.py / config.yaml
├── agent_core.py        # 路由、SYSTEM_PROMPT、build_agent
├── rag.py               # Query 改写
├── blog_client.py       # 调用 Go 博客 API
├── memory_manager.py    # 长期记忆、Qdrant 混合检索、文档入库
├── memory_store.py      # 短期/中期、Session
├── mcp_public.py / mcp_user.py / mcp_server.py
├── agent_log.py
├── test_agent.py、agent_demo*.py
├── requirements.txt
└── README.md
```

---

## 环境依赖

| 服务 | 用途 |
| --- | --- |
| Redis | 短期/中期、Session |
| MongoDB | 长期结构化事实 |
| Qdrant | 长期向量 + 文档块 |
| GoBlog API | 工具调博客业务 |
| 智谱等 OpenAI 兼容 API | LLM、Embedding |

---

## 快速启动

```bash
# 依赖（示例）
cd ..
docker compose up -d redis mongodb qdrant

cd agent
python -m venv .venv
.venv\Scripts\activate   # Windows
pip install -r requirements.txt
python main.py           # 默认 0.0.0.0:9091
```

配置好 `config.yaml` 中的 `llm` / `embedding` / 数据服务地址后再启动。

---

## API 参考

### `POST /chat`

```json
{
  "message": "用户问题",
  "user_id": 1,
  "token": "可选 JWT",
  "conversation_history": null,
  "history_owned_by_gateway": false
}
```

- `conversation_history`：可选；由网关注入博客 Redis 中的短期消息列表时，设 `history_owned_by_gateway: true`，本服务不再读本地 Redis 短期列表。

响应：**SSE**（`text/event-stream`）。

### `POST /memory/session-end`

结束会话时触发长期记忆收尾与 Session 清理。

```json
{ "user_id": 1, "reason": "session_end" }
```

### `POST /memory/ingest-document`

写入全局文档向量块（需密钥时加头 `X-Ingest-Key`）。

```json
{
  "article_id": "词条或文章标识",
  "chunk_index": 0,
  "content": "块正文",
  "content_revision": 1,
  "source": "来源 URL 或稳定键",
  "ingest_kind": "raw_chunk",
  "game_name": null,
  "section_path": ["章节", "可选"],
  "preview": null
}
```

### `GET /health`

`{"status":"ok"}`

---

## 配置说明

主要键：`llm`、`summary_llm`、`memory_manager_llm`、`decision_llm`、`embedding`、`redis`、`mongo`、`qdrant`、`memory`、`blog_api_url`、`document_ingest_secret`。

### Memory（节选）

| 参数 | 说明 |
| --- | --- |
| `long_term_retrieval_limit` | 每次召回长期条数上限 |
| `hybrid_search` | 是否启用 Qdrant 混合检索 |
| `hybrid_prefetch_limit` | 向量/字面各路候选规模 |
| `hybrid_max_union_points` | 融合前去并后的最大点数 |
| `hybrid_dense_weight` / `hybrid_keyword_weight` | 融合权重（内部会归一化） |
| `hybrid_keyword_recall` | 是否启用 MatchTextAny 扩展召回 |

---

## 日志

`agent_log.py`：按天滚动，默认保留 7 天，路径参见运行目录下的 `logger/agent_log/`。

---

## Demo

- `agent_demo.py` / `agent_demo_simple.py`：本地体验 LangChain Agent（与生产记忆/路由解耦）。

---

## 设计定位与理性评价

**定位**：面向博客场景的 **单体 Agent 服务**——在「能做复杂产品」与「依赖少、可维护」之间取了折中：路由减工具面、记忆分层、Wiki 与对话记忆分 scope，适合中小流量与个人/小团队维护。

**优点（相对同类开源博客助手）**

- 流水线清晰：**改写 → 路由 → 工具 Agent**，不是单轮 completion。  
- **工具与权限分离**（public / user），token 不喂给提示词。  
- **记忆模型完整**：短中长按窗口与触发策略演进；长期带 **决策 LLM** 缓解冲突；Qdrant 支持 **混合检索** 与 **独立文档入库**，便于接 Wiki。  
- **可观测**：SSE 事件拆分改写、路由、工具阶段。

**局限与风险（诚实描述）**

- **规划**：复杂任务主要靠 **系统提示** 约束「先计划再执行」，没有独立的结构化 Plan-and-Execute 图或评测集。  
- **质量上界**：路由、记忆提取、决策均依赖 LLM，错误会级联；生产上宜加日志抽检与关键路径监控。  
- **Mongo 长期召回**当前偏「按用户拉一批事实」，与 query 的语义对齐不如向量侧精确（若需可后续升级为检索或摘要筛选）。  
- **混合检索**是 dense + 字面启发式 +（可选）MatchTextAny，**不是** Qdrant 文档里那种 **dense+sparse 双向量 RRF**；数据量大时建议给 `content` 建 payload 全文索引以减轻服务端压力。

**总评**：作为博客内置助手，架构 **完整、边界清楚**，在单体项目里属于 **扎实可用的一档**；若对标商业「Agent 平台」或强合规场景，还需补充 **评测、追踪、与人审知识库闭环** 等工程化能力。是否「够用」取决于你的产品预期与流量规模，而非缺一两项时髦名词。
