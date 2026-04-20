# GoBlog

GoBlog 是一个基于 Gin + GORM + Redis + NSQ + Kafka + Elasticsearch 的论坛后端项目。

1. 用户与权限
用户注册、登录、信息管理，支持JWT鉴权中间件，保障接口安全。
用户关注/取关、粉丝列表等社交关系链。
2. 文章系统
文章的发布、编辑、删除、详情、列表、搜索等功能。
支持文章的点赞、阅读计数，采用异步链路（NSQ）+Redis热点缓存+MySQL批量落库，提升性能与一致性。
3. 评论与私信
文章评论的增删查，评论数异步统计。
私信（DM）功能，支持消息的发送、接收、未读数统计等。
4. 通知系统
关注、评论、点赞等行为触发通知，Kafka异步推送，WebSocket实时下发，离线消息补偿。
通知的已读/未读状态管理。
5. 搜索与推荐
基于Elasticsearch实现文章/用户等内容的全文检索。
热门文章、话题等推荐功能，利用Redis ZSet实现高效排序。
6. 话题与标签
文章关联话题，话题的创建、浏览、讨论等。
话题下文章聚合、讨论区等功能。
7. 缓存与防护
文章详情、热门数据等采用Redis缓存，热点key防击穿，空值缓存+布隆过滤器防穿透。
读写分离，降低数据库压力。
8. 幂等与去重
异步任务/消息消费引入唯一键，防止重复计数、重复推送等问题。
9. 运维与部署
配置集中管理（Viper），结构化日志（Zap）。
Docker Compose一键启动依赖服务，Nginx反向代理，便于本地和生产环境一致部署。
10. 其他
健康检查接口，便于K8s等平台探针监控。
统一错误处理与日志追踪，便于排查问题。


一、通知系统
1. 触发场景
用户关注、被评论、被点赞、收到私信等事件，都会触发通知。
2. 技术实现
消息队列解耦：业务操作（如评论、点赞）后，将通知事件写入Kafka队列，主流程快速返回，提升响应速度。
异步消费：专门的通知消费者服务从Kafka拉取消息，进行后续处理（如推送、落库）。
WebSocket实时推送：在线用户通过WebSocket（Melody）建立长连接，消费者检测到目标用户在线时，直接推送通知消息，实现秒级实时性。
离线补偿：若用户不在线，通知消息先存入Redis或MySQL，用户下次上线时自动拉取未读通知，保证消息不丢失。
失败重试与补偿：推送失败的消息会进入重试队列，或通过定时任务补偿，提升链路可靠性。
已读/未读管理：支持通知的已读、未读状态切换，便于前端展示和用户体验。
3. 典型流程
用户A评论了用户B的文章。
业务服务将“评论通知”事件写入Kafka。
通知消费者监听Kafka，处理该事件。
检查用户B是否在线，在线则WebSocket推送，不在线则存储离线消息。
用户B上线后拉取未读通知，前端展示。

二、异步统计
1. 适用场景
点赞、阅读、评论数等高频写操作，直接落库会导致数据库压力大、写放大严重。
2. 技术实现
NSQ异步解耦：前端触发点赞/阅读等操作时，主流程只写入NSQ队列，快速返回，极大提升接口QPS。
Redis热点缓存：消费者服务从NSQ消费消息，实时更新Redis中的统计数据（如Hash/ZSet），用于热点数据的快速展示。
批量回写MySQL：通过定时任务或阈值触发，将Redis中的增量数据批量同步到MySQL，降低写放大，保证最终一致性。
最终一致性保障：即使部分异步任务失败，也可通过补偿任务（如定时全量同步）确保数据最终一致。
去重与幂等：每条异步消息带唯一ID，消费端做幂等处理，防止重复计数。
3. 典型流程
用户点赞/阅读文章，接口直接写入NSQ队列，主流程返回。
消费者服务监听NSQ，实时更新Redis中的点赞/阅读数。
Redis数据用于前端实时展示，保证高并发下的性能。
定时/批量将Redis中的数据同步到MySQL，保证数据持久化和一致性。



> 说明：当前代码里异步链路主要使用 **NSQ**（点赞/阅读统计）与 **Kafka**（通知/搜索同步）。README 中的链路描述以仓库现有实现为准。

## 架构分层

- `bootstrap`: 应用启动装配层，负责配置、日志、数据库、缓存、队列与路由初始化。
- `router`: HTTP 路由注册与中间件挂载。
- `handler`: API 入参校验、响应与编排。
- `service`: 业务逻辑层。
- `database`: MySQL/Redis 访问层。
- `mq`: 异步消息队列封装（NSQ/Kafka）。
- `middleware`: 认证、安全、限流、可观测性中间件。
- `setting`: 配置加载、默认值和校验。

## 快速启动

1. 启动依赖（至少需要 MySQL、Redis，可选 NSQ/Kafka/Elasticsearch）：

```bash
docker compose up -d
```

2. 运行服务：

```bash
go run main.go
```

3. 健康检查与指标：

- `GET /healthz`
- `GET /readyz`
- `GET /metrics`（启用观测配置后）

## 配置说明

核心配置位于 `setting/common.yaml`。

- `mq.asynq.enabled`: 是否启用 Asynq。
- `mq.nsq.enabled`: 是否启用 NSQ。
- `auth.jwt_secret`: JWT 密钥（生产环境必须覆盖）。
- `security.rate_limit_per_minute`: 每分钟限流阈值。
- `observability.enable_metrics`: 是否启用指标采集。

你可以通过环境变量覆盖配置项，例如：

```bash
set SEARCH_API_KEY=your-key
```

## API 约定

- 基础前缀：`/api/v1`
- 统一响应结构：`code`, `message`, `data`, `request_id`
- 鉴权方式：`Authorization: Bearer <token>`

## 核心链路实现

本节按“入口 API → Service → Redis/MySQL → MQ → Consumer”把四条关键链路写清楚，便于面试/排障时快速定位。

### 1) 点赞/阅读量统计链路（Redis 实时 + NSQ 批量落库）

**目标**：接口响应要快；统计数字尽量实时；最终写回 MySQL 保证可持久化。

**读文章并累计阅读量（阅读量）**

- 入口：`GET /api/v1/articles/:id` → `handler.ArticleHandler.ReadArticleHandle()`
- 主流程：`service.ArticleService.ReadArticle(id, actorKey)`
	- 先通过 `GetArticle()` 读取文章详情（缓存优先，必要时回源 DB），并把 Redis 中的实时统计“套”到返回对象上。
	- 阅读幂等：用 Redis `SETNX` 写入一次性 key，避免同一 actor 在 TTL 内刷量。
		- key 形如：`article:read:limit:<aid>:<actorKey>`，TTL：1h（`articleReadLimitTTL`）
		- actorKey 取值策略：优先 `u:<userID>`（如果带了 Bearer Token 且可解析），否则 `s:<sessionID>`（匿名会话 cookie `gb_sid`），避免 NAT/IP 误伤
	- 计数实时写 Redis：
		- Hash：`article:stats:<aid>`，字段：`view`/`like`（`HINCRBY`）
	- 同步更新排行榜：
		- ZSET：`article:leaderboard:view`（`ZINCRBY +1`）
	- 异步落库：向 NSQ 投递消息（topic：`article_stats`），消息类型为 `view`。

**点赞/取消点赞（点赞量）**

- 入口：`POST /api/v1/articles/like` → `handler.ArticleHandler.LikeArticleHandle()`
- 主流程：`service.ArticleService.LikeArticle(aid, userID, isCancel)`
	- 点赞/取消点赞幂等：通过 MySQL 的唯一行为记录表 `article_likes(user_id, article_id)` 保证（重复点赞不会重复计数；取消点赞仅在记录存在时生效）
	- 计数实时写 Redis：
		- like：`HINCRBY article:stats:<aid> like +1`
		- unlike：`HINCRBY ... like -1`
	- 同步更新排行榜：
		- ZSET：`article:leaderboard:like`（`ZINCRBY ±1`）
	- 异步落库：向 NSQ 投递 `like/unlike` 消息（topic：`article_stats`）。

**NSQ 统计消费者如何批量落库**

- 初始化：`bootstrap.Init()` 在 `setting.Conf.NSQConfig.Enabled` 为 true 时调用 `mq.InitNSQ()`
- 消费者：`mq.StatsWorker`（注册 topic：`article_stats`，channel：`stats_sync_group`）
	- `HandleMessage()` 将 view/like delta 先写入内存 buffer（map 聚合）
	- 定时 flush（默认 1 分钟）：`StartFlushTicks()` → `flush()`
	- 批量事务落库：`database.ArticleRepository.BatchIncrementStats()`
		- 通过 `UPDATE ... SET view_count = view_count + ?` / `like_count = like_count + ?` 实现增量更新
	- flush 成功后才从 buffer 扣减；失败则保留 buffer，下次重试（至少一次投递语义）。

**一致性与失败回滚**（实现里做了“先 Redis 后 MQ”的补偿）

- 若 Redis 计数/排行榜更新成功但 MQ 投递失败：Service 会尝试把 Redis 增量回滚（`DecrStats` / `UpdateLeaderboardDecr`）并删除幂等 key。
- MySQL 最终一致：Redis 的数字可能短暂领先；NSQ consumer flush 后落库与 Redis 对齐。

### 2) 搜索链路（写入触发同步 + 查询走 Elasticsearch）

**目标**：写入链路不阻塞；搜索服务可开关；索引结构最小可用。

**写入/更新触发搜索同步**

- 入口（创建文章）：`POST /api/v1/articles` → `service.ArticleService.CreateArticle()`
- 在 `CreateArticle()` 成功写 MySQL 后：调用 `syncArticleToSearch(article)`
	- 组装 `models.SearchSyncPayload`（`id/title/author_name/tags/summary`）
	- 通过 Kafka topic：`search.push` 异步发送（`mq.PublishSearchSync()`）

**Kafka 消费者：落地到 Elasticsearch**

- 初始化：`bootstrap.Init()` 在 `setting.Conf.KafkaConfig.Enabled` 为 true 时调用 `mq.InitKafka()`，并在 `app.StartWorkers()` 启动 consumers。
- Consumer：`handler.SearchHandler.ProcessSearchSyncMessage()`
	- 若搜索未启用（`search.Enabled()==false`）则直接跳过
	- `search.UpsertArticle()` 用 `IndexRequest` 以 `DocumentID=<id>` upsert 文档

**查询链路**

- 入口：`GET /api/v1/search?q=关键词` → `handler.SearchHandler.GlobalSearch()`
- 实现：`search.SearchArticles()`
	- 使用 `multi_match` 检索字段：`title^3`、`author_name`、`tags`、`summary`
	- 返回 `_source` 的数组（目前以 `[]map[string]any` 形式返回）
	- 若 ES 未启用或查询失败：自动降级为 MySQL `LIKE` 简单查询（用于兜底）

**索引初始化与开关**

- `search.Init(host, apiKey, index)` 会做一次连通性校验，并在索引不存在时创建最小 mapping。
- 通过配置 `search.enabled` 控制是否启用；启用后还需保证 Kafka worker 已启动，否则只会“能搜但不自动同步新增/更新”。

### 3) 消息通知链路（WebSocket 实时推送 + 离线落库）

**目标**：在线实时；离线不丢；重试不会重复插入；客户端重连能补齐。

**WebSocket 建连与连接管理**

- 入口：`GET /api/v1/ws?token=...` → `handler.NotificationHandler.HandleWS()`
	- token 校验：`jwt_module.ParseToken()`
	- 使用 Melody 升级连接，并在 session keys 中写入 `userID`
- 连接生命周期：`NewNotificationHandler()` 内注册 Melody hook
	- `HandleConnect`：把 `userID -> session` 存入 `Conns`，并异步触发离线未读同步 `syncUnreadNotifications()`
	- `HandleDisconnect`：从 `Conns` 删除该用户

**通知推送（Kafka → WS）**

- Topic：`notification.push`
- Consumer：`NotificationHandler.ProcessNotificationMessage()`
	- 若用户在线：直接 `session.Write()` 推送 JSON
	- 若用户不在线 / 写入失败：进入离线分支 `persistOfflineNotification()`

**离线通知：红点 + Kafka 落库**

- Redis 红点：`notify:has_unread:<uid>`
	- 不保存通知明细，仅标记“有未读”（7 天 TTL）
- 离线落库：发送 Kafka topic：`notification.store`（`mq.PublishNotificationStore()`）
- Consumer：`NotificationStoreHandler.ProcessNotificationStoreMessage()`
	- 解析 payload → 转 `models.Notification`（`EventID` 作为业务唯一键）
	- 先进入内存 buffer，达到批大小或定时（10s）批量写 MySQL
	- MySQL 批量写：`NotificationRepository.BatchCreateNotifications()` 使用 `OnConflict DoNothing`，保证 Kafka 重投不重复插入

**重连补齐未读**

- `syncUnreadNotifications()`：
	- 先检查 Redis 红点 `HasUnread()`，没有就直接返回
	- 再从 MySQL 拉取未读列表 `GetUnreadByUserID()`，逐条写回 WS
	- 推送完后：清理红点 `ClearHasUnread()`，并异步把 MySQL 未读全部标记为已读 `MarkReadByUserID()`

### 4) 排行榜链路（Redis ZSET + 热点文章集合）

**目标**：排行榜读写都在 Redis；热门文章走逻辑过期与异步重建，降低缓存击穿风险。

**排行榜写入（与统计同链路）**

- 阅读/点赞时，Service 会同步更新 Redis ZSET：
	- 阅读榜：`article:leaderboard:view`
	- 点赞榜：`article:leaderboard:like`
- 更新方式：`ZINCRBY`（点赞取消会 `ZINCRBY -1`）

**排行榜读取（当前为 Service 能力）**

- `service.ArticleService.GetLeaderboard(actionType)`：
	- 先从 Redis 取前 N 个文章 ID：`RedisArticleRepository.GetTopArticleIDs()`（`ZREVRANGE`）
	- 再批量查 MySQL：`ArticleRepository.GetArticlesByIDs()`
	- 最后按 Redis 返回顺序重新排序（避免 MySQL 返回顺序打乱）

> 备注：目前路由层还未暴露对应的排行榜 HTTP API，你可以在 handler/router 中补一个只读接口（例如 `GET /api/v1/leaderboard?type=view|like`）直接调用该 Service 方法。

**热门文章 HotKey 判定**

- Redis Set：`article:hotkeys` 作为“热点文章 ID 集合”
- 刷新机制：`ArticleService` 启动时会起一个 goroutine，每 30s 从阅读榜取 TopN，筛掉分数 < 20 的成员，写入 `article:hotkeys` 并设置 TTL（2 分钟）。
- 读取详情时：`GetArticle()` 先 `SISMEMBER article:hotkeys <aid>` 判断是否热点
	- 热点：缓存采用“逻辑过期”（value 内含 `expire_at`），过期后异步重建，降低并发击穿
	- 非热点：普通 TTL 缓存

**缓存防穿透/击穿策略（与排行榜配合）**

- 防穿透：Bloom Filter 预热（启动时从 MySQL 拉取一批文章 ID），不存在则直接写空值缓存 `__NULL__`。
- 防击穿：singleflight 合并回源 + Redis 重建锁（`SETNX article:detail:lock:<aid>`）避免多协程同时回源。

## 测试与质量

```bash
go test ./...
go vet ./...
```

项目已集成 GitHub Actions CI，自动执行 vet、test、build。

## 数据迁移

迁移脚本目录：`database/migrations`。

推荐使用 `goose` 管理 SQL 迁移，详情见 `database/migrations/README.md`。