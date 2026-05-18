package mq

import (
	"encoding/json"
	"fmt"
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
	// likeInsert 待异步插入的 article_likes（key=uid:aid，由带 user_id 的 like 消息产生）
	likeInsert map[string]struct {
		UserID    uint64
		ArticleID uint64
	}
	// unlikeDelete 待异步删除的 article_likes 行（key=uid:aid）
	unlikeDelete map[string]struct {
		UserID    uint64
		ArticleID uint64
	}
	flushInterval time.Duration
	flushMaxKeys  int
	flushSig      chan struct{}

	articleDB *database.ArticleRepository // 仅处理 MySQL 批量落库
}

// NewStatsWorker mysqlRepo 负责数据库；interval<=0 时使用 1 分钟；maxKeys<=0 时不按 buffer 规模触发 flush（仅定时）。
func NewStatsWorker(mysqlRepo *database.ArticleRepository, interval time.Duration, maxKeys int) *StatsWorker {
	if interval <= 0 {
		interval = 1 * time.Minute
	}
	return &StatsWorker{
		viewBuffer:    make(map[uint64]int),
		likeBuffer:    make(map[uint64]int),
		unlikeDelete: make(map[string]struct {
			UserID    uint64
			ArticleID uint64
		}),
		likeInsert: make(map[string]struct {
			UserID    uint64
			ArticleID uint64
		}),
		flushInterval: interval,
		flushMaxKeys:  maxKeys,
		flushSig:      make(chan struct{}, 1),
		articleDB:     mysqlRepo,
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

	w.mu.Lock()
	switch msg.Type {
	case "view":
		w.viewBuffer[msg.ArticleID]++
	case "like":
		if msg.UserID != 0 {
			k := fmt.Sprintf("%d:%d", msg.UserID, msg.ArticleID)
			w.likeInsert[k] = struct {
				UserID    uint64
				ArticleID uint64
			}{msg.UserID, msg.ArticleID}
		} else {
			w.likeBuffer[msg.ArticleID]++
		}
	case "unlike":
		delta := -1
		if msg.LikeDelta != nil {
			delta = *msg.LikeDelta
		}
		if delta != 0 {
			w.likeBuffer[msg.ArticleID] += delta
		}
		if msg.UserID != 0 {
			k := fmt.Sprintf("%d:%d", msg.UserID, msg.ArticleID)
			w.unlikeDelete[k] = struct {
				UserID    uint64
				ArticleID uint64
			}{msg.UserID, msg.ArticleID}
		}
	default:
		w.mu.Unlock()
		zap.L().Warn("未知的文章事件类型，直接确认丢弃", zap.String("type", msg.Type))
		return nil
	}
	approxKeys := len(w.viewBuffer) + len(w.likeBuffer) + len(w.unlikeDelete) + len(w.likeInsert)
	w.mu.Unlock()

	if w.flushMaxKeys > 0 && approxKeys >= w.flushMaxKeys {
		select {
		case w.flushSig <- struct{}{}:
		default:
		}
	}

	return nil
}

// StartFlushTicks 定时或与 buffer 规模触发 flush（二者 OR，在单独 goroutine 中串行执行）。
func (w *StatsWorker) StartFlushTicks() {
	ticker := time.NewTicker(w.flushInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				w.flush()
			case <-w.flushSig:
				w.flush()
			}
		}
	}()
}

func (w *StatsWorker) flush() {
	w.mu.Lock()
	views := make(map[uint64]int, len(w.viewBuffer))
	likes := make(map[uint64]int, len(w.likeBuffer))
	for id, count := range w.viewBuffer {
		views[id] = count
	}
	for id, count := range w.likeBuffer {
		likes[id] = count
	}
	unlikeRows := make([]database.ArticleLikePair, 0, len(w.unlikeDelete))
	unlikeKeys := make([]string, 0, len(w.unlikeDelete))
	for k, v := range w.unlikeDelete {
		unlikeKeys = append(unlikeKeys, k)
		unlikeRows = append(unlikeRows, database.ArticleLikePair{UserID: v.UserID, ArticleID: v.ArticleID})
	}
	likeRows := make([]database.ArticleLikePair, 0, len(w.likeInsert))
	likeKeys := make([]string, 0, len(w.likeInsert))
	for k, v := range w.likeInsert {
		likeKeys = append(likeKeys, k)
		likeRows = append(likeRows, database.ArticleLikePair{UserID: v.UserID, ArticleID: v.ArticleID})
	}
	w.mu.Unlock()

	if len(views) == 0 && len(likes) == 0 && len(unlikeRows) == 0 && len(likeRows) == 0 {
		return
	}

	if err := w.articleDB.BatchFlushArticleActions(views, likes, likeRows, unlikeRows); err != nil {
		zap.L().Warn("文章统计/点赞批量落库失败，将保留缓冲区等待下次重试", zap.Error(err))
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
	for _, k := range unlikeKeys {
		delete(w.unlikeDelete, k)
	}
	for _, k := range likeKeys {
		delete(w.likeInsert, k)
	}
	w.mu.Unlock()

	zap.L().Info("统计数据批量落库完成")
}

// CommentStatsWorker 评论点赞统计消费者
type CommentStatsWorker struct {
	mu            sync.Mutex
	likeBuffer    map[uint64]int
	flushInterval time.Duration
	flushMaxKeys  int
	flushSig      chan struct{}

	commentDB *database.CommentRepository
}

func NewCommentStatsWorker(commentDB *database.CommentRepository, interval time.Duration, maxKeys int) *CommentStatsWorker {
	if interval <= 0 {
		interval = 1 * time.Minute
	}
	return &CommentStatsWorker{
		likeBuffer:    make(map[uint64]int),
		flushInterval: interval,
		flushMaxKeys:  maxKeys,
		flushSig:      make(chan struct{}, 1),
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

	w.mu.Lock()
	switch msg.Type {
	case "like":
		w.likeBuffer[msg.CommentID]++
	case "unlike":
		w.likeBuffer[msg.CommentID]--
	default:
		w.mu.Unlock()
		zap.L().Warn("未知的评论事件类型，直接确认丢弃", zap.String("type", msg.Type))
		return nil
	}
	n := len(w.likeBuffer)
	w.mu.Unlock()

	if w.flushMaxKeys > 0 && n >= w.flushMaxKeys {
		select {
		case w.flushSig <- struct{}{}:
		default:
		}
	}

	return nil
}

func (w *CommentStatsWorker) StartFlushTicks() {
	ticker := time.NewTicker(w.flushInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				w.flush()
			case <-w.flushSig:
				w.flush()
			}
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
