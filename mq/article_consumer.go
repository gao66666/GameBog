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

	articleDB *database.ArticleRepository // 仅处理 MySQL 批量落库
}

// 传入依赖：mysqlRepo 负责数据库
func NewStatsWorker(mysqlRepo *database.ArticleRepository) *StatsWorker {
	return &StatsWorker{
		viewBuffer:    make(map[uint64]int),
		likeBuffer:    make(map[uint64]int),
		flushInterval: 1 * time.Minute,
		articleDB:     mysqlRepo, // 对应 MySQL
	}
}

func (w *StatsWorker) HandleMessage(m *nsq.Message) error {
	if len(m.Body) == 0 {
		return nil
	}

	var msg ArticleActionMsg
	if err := json.Unmarshal(m.Body, &msg); err != nil {
		zap.L().Error("解析NSQ消息失败，视为脏消息直接确认丢弃", zap.Error(err))
		return nil
	}

	if msg.ArticleID == 0 {
		zap.L().Warn("NSQ消息缺少article_id，直接丢弃")
		return nil
	}

	var likeDelta int
	switch msg.Type {
	case "view":
		w.mu.Lock()
		w.viewBuffer[msg.ArticleID]++
		w.mu.Unlock()
	case "like":
		likeDelta = 1
	case "unlike":
		likeDelta = -1
	default:
		zap.L().Warn("未知的文章事件类型，直接确认丢弃", zap.String("type", msg.Type))
		return nil
	}

	if msg.Type != "view" {
		w.mu.Lock()
		w.likeBuffer[msg.ArticleID] += likeDelta
		w.mu.Unlock()
	}

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
	// 1. 锁操作：先拷贝快照，但不立刻清空，只有事务成功后再清空
	w.mu.Lock()
	views := make(map[uint64]int, len(w.viewBuffer))
	likes := make(map[uint64]int, len(w.likeBuffer))
	for id, count := range w.viewBuffer {
		views[id] = count
	}
	for id, count := range w.likeBuffer {
		likes[id] = count
	}
	w.mu.Unlock()

	if len(views) == 0 && len(likes) == 0 {
		return
	}

	if err := w.articleDB.BatchIncrementStats(views, likes); err != nil {
		zap.L().Warn("统计数据批量落库失败，将保留缓冲区等待下次重试", zap.Error(err))
		return
	}

	w.mu.Lock()
	for aid, count := range views {
		w.viewBuffer[aid] -= count
		if w.viewBuffer[aid] <= 0 {
			delete(w.viewBuffer, aid)
		}
	}
	for aid, count := range likes {
		w.likeBuffer[aid] -= count
		if w.likeBuffer[aid] <= 0 {
			delete(w.likeBuffer, aid)
		}
	}
	w.mu.Unlock()

	zap.L().Info("统计数据批量落库完成")
}

// CommentStatsWorker 评论点赞统计消费者
type CommentStatsWorker struct {
	mu            sync.Mutex
	likeBuffer    map[uint64]int
	flushInterval time.Duration

	commentDB *database.CommentRepository
}

func NewCommentStatsWorker(commentDB *database.CommentRepository) *CommentStatsWorker {
	return &CommentStatsWorker{
		likeBuffer:    make(map[uint64]int),
		flushInterval: 1 * time.Minute,
		commentDB:     commentDB,
	}
}

func (w *CommentStatsWorker) HandleMessage(m *nsq.Message) error {
	if len(m.Body) == 0 {
		return nil
	}

	var msg CommentActionMsg
	if err := json.Unmarshal(m.Body, &msg); err != nil {
		zap.L().Error("解析评论NSQ消息失败，视为脏消息直接确认丢弃", zap.Error(err))
		return nil
	}

	if msg.CommentID == 0 {
		zap.L().Warn("NSQ消息缺少comment_id，直接丢弃")
		return nil
	}

	var delta int
	switch msg.Type {
	case "like":
		delta = 1
	case "unlike":
		delta = -1
	default:
		zap.L().Warn("未知的评论事件类型，直接确认丢弃", zap.String("type", msg.Type))
		return nil
	}

	w.mu.Lock()
	w.likeBuffer[msg.CommentID] += delta
	w.mu.Unlock()
	return nil
}

func (w *CommentStatsWorker) StartFlushTicks() {
	ticker := time.NewTicker(w.flushInterval)
	go func() {
		for range ticker.C {
			w.flush()
		}
	}()
}

func (w *CommentStatsWorker) flush() {
	w.mu.Lock()
	buf := make(map[uint64]int, len(w.likeBuffer))
	for id, delta := range w.likeBuffer {
		buf[id] = delta
	}
	w.mu.Unlock()

	if len(buf) == 0 {
		return
	}

	if err := w.commentDB.BatchIncrementCommentStats(buf); err != nil {
		zap.L().Warn("评论点赞批量落库失败，将保留缓冲区等待下次重试", zap.Error(err))
		return
	}

	w.mu.Lock()
	for id, delta := range buf {
		w.likeBuffer[id] -= delta
		if w.likeBuffer[id] == 0 {
			delete(w.likeBuffer, id)
		}
	}
	w.mu.Unlock()

	zap.L().Info("评论点赞批量落库完成")
}
