package mq

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/nsqio/go-nsq"
	"go.uber.org/zap"
)

// StatsWorker 统计数据消费者
type StatsWorker struct {
	mu            sync.Mutex
	viewBuffer    map[uint64]int
	likeBuffer    map[uint64]int
	flushInterval time.Duration

	// 注入两个 Repo，一个负责实时，一个负责持久化
	redisRepo *database.RedisArticleRepository // 处理 Redis 实时计数
	articleDB *database.ArticleRepository      // 处理 MySQL 批量落库
}

// 传入两个依赖：mysqlRepo 负责数据库，redisRepo 负责缓存
func NewStatsWorker(mysqlRepo *database.ArticleRepository, redisRepo *database.RedisArticleRepository) *StatsWorker {
	return &StatsWorker{
		viewBuffer:    make(map[uint64]int),
		likeBuffer:    make(map[uint64]int),
		flushInterval: 1 * time.Minute,
		articleDB:     mysqlRepo, // 对应 MySQL
		redisRepo:     redisRepo, // 对应 Redis
	}
}

func (w *StatsWorker) HandleMessage(m *nsq.Message) error {
	if len(m.Body) == 0 {
		return nil
	}

	var msg ArticleActionMsg
	if err := json.Unmarshal(m.Body, &msg); err != nil {
		zap.L().Error("解析NSQ消息失败", zap.Error(err))
		return nil
	}

	// --- 第一步：实时更新 Redis (不在锁内) ---
	// 理由：Redis 操作涉及网络 IO，如果放在锁里，并发高时会拖慢整个 Worker 的速度
	field := "view"
	if msg.Type == "like" {
		field = "like"
	}
	// 调用你刚写的 RedisRepo 方法
	_ = w.redisRepo.IncrStats(msg.ArticleID, field)
	_ = w.redisRepo.UpdateLeaderboard(msg.ArticleID, msg.Type)
	// --- 第二步：内存 Map 聚合 (加锁) ---
	w.mu.Lock()
	switch msg.Type {
	case "view":
		w.viewBuffer[msg.ArticleID]++
	case "like":
		w.likeBuffer[msg.ArticleID]++
	}
	w.mu.Unlock()

	return nil
}

// StartFlushTicks 启动定时任务
func (w *StatsWorker) StartFlushTicks() {
	ticker := time.NewTicker(w.flushInterval)
	go func() {
		for range ticker.C {
			w.flush()
		}
	}()
}

func (w *StatsWorker) flush() {
	// 1. 锁操作 (保持不变，很赞)
	w.mu.Lock()
	views := w.viewBuffer
	likes := w.likeBuffer
	w.viewBuffer = make(map[uint64]int)
	w.likeBuffer = make(map[uint64]int)
	w.mu.Unlock()

	if len(views) == 0 && len(likes) == 0 {
		return
	}

	// 3. 处理阅读量落库
	for aid, count := range views {
		if count <= 0 {
			continue
		}
		_ = w.articleDB.IncrementViewCount(aid, int64(count))
	}

	// 4. 处理点赞数落库
	for aid, count := range likes {
		if count <= 0 {
			continue
		}
		_ = w.articleDB.IncrementLikeCount(aid, int64(count))
	}

	// 5. 修剪排行榜 (必须和 UpdateLeaderboard 的 Key 保持一致)
	// 假设你的 Key 格式是 "article:leaderboard:view"
	_ = w.redisRepo.TrimLeaderboard("view", 100)
	_ = w.redisRepo.TrimLeaderboard("like", 100)

	zap.L().Info("统计数据落库及排行榜修剪完成")
}
