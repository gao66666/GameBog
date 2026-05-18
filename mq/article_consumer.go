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

type likePair struct {
	UserID    uint64
	ArticleID uint64
}

// statsBuffer 单分片内存聚合缓冲。
type statsBuffer struct {
	viewBuffer   map[uint64]int
	likeBuffer   map[uint64]int
	likeInsert   map[string]likePair
	unlikeDelete map[string]likePair
}

func newStatsBuffer() *statsBuffer {
	return &statsBuffer{
		viewBuffer:   make(map[uint64]int),
		likeBuffer:   make(map[uint64]int),
		likeInsert:   make(map[string]likePair),
		unlikeDelete: make(map[string]likePair),
	}
}

func (b *statsBuffer) approxKeys() int {
	return len(b.viewBuffer) + len(b.likeBuffer) + len(b.likeInsert) + len(b.unlikeDelete)
}

func (b *statsBuffer) isEmpty() bool {
	return b.approxKeys() == 0
}

func (b *statsBuffer) apply(msg ArticleActionMsg) {
	switch msg.Type {
	case "view":
		b.viewBuffer[msg.ArticleID]++
	case "like":
		if msg.UserID != 0 {
			k := fmt.Sprintf("%d:%d", msg.UserID, msg.ArticleID)
			b.likeInsert[k] = likePair{UserID: msg.UserID, ArticleID: msg.ArticleID}
		} else {
			b.likeBuffer[msg.ArticleID]++
		}
	case "unlike":
		delta := -1
		if msg.LikeDelta != nil {
			delta = *msg.LikeDelta
		}
		if delta != 0 {
			b.likeBuffer[msg.ArticleID] += delta
		}
		if msg.UserID != 0 {
			k := fmt.Sprintf("%d:%d", msg.UserID, msg.ArticleID)
			b.unlikeDelete[k] = likePair{UserID: msg.UserID, ArticleID: msg.ArticleID}
		}
	}
}

func (b *statsBuffer) mergeFrom(other *statsBuffer) {
	for id, n := range other.viewBuffer {
		b.viewBuffer[id] += n
	}
	for id, n := range other.likeBuffer {
		b.likeBuffer[id] += n
	}
	for k, v := range other.likeInsert {
		b.likeInsert[k] = v
	}
	for k, v := range other.unlikeDelete {
		b.unlikeDelete[k] = v
	}
}

func (b *statsBuffer) toFlushPayload() (
	views map[uint64]int,
	likes map[uint64]int,
	likeRows []database.ArticleLikePair,
	unlikeRows []database.ArticleLikePair,
) {
	views = make(map[uint64]int, len(b.viewBuffer))
	likes = make(map[uint64]int, len(b.likeBuffer))
	for id, count := range b.viewBuffer {
		views[id] = count
	}
	for id, count := range b.likeBuffer {
		likes[id] = count
	}
	likeRows = make([]database.ArticleLikePair, 0, len(b.likeInsert))
	for _, v := range b.likeInsert {
		likeRows = append(likeRows, database.ArticleLikePair{UserID: v.UserID, ArticleID: v.ArticleID})
	}
	unlikeRows = make([]database.ArticleLikePair, 0, len(b.unlikeDelete))
	for _, v := range b.unlikeDelete {
		unlikeRows = append(unlikeRows, database.ArticleLikePair{UserID: v.UserID, ArticleID: v.ArticleID})
	}
	return views, likes, likeRows, unlikeRows
}

// statsShard 分片：独立锁 + 可置换的 buffer 指针。
type statsShard struct {
	mu       sync.Mutex
	buf      *statsBuffer
	flushSig chan struct{}
}

func (s *statsShard) swapBuffers() *statsBuffer {
	s.mu.Lock()
	old := s.buf
	s.buf = newStatsBuffer()
	s.mu.Unlock()
	return old
}

func (s *statsShard) mergeBack(buf *statsBuffer) {
	if buf == nil || buf.isEmpty() {
		return
	}
	s.mu.Lock()
	s.buf.mergeFrom(buf)
	s.mu.Unlock()
}

// StatsWorker 统计数据消费者（按 article_id 哈希分片，降低单锁竞争）。
type StatsWorker struct {
	shards        []*statsShard
	shardCount    int
	flushInterval time.Duration
	flushMaxKeys  int

	articleDB *database.ArticleRepository
}

func statsShardIndex(articleID uint64, shardCount int) int {
	if shardCount <= 1 {
		return 0
	}
	return int(articleID % uint64(shardCount))
}

// NewStatsWorker mysqlRepo 负责数据库；interval<=0 时使用 1 分钟；maxKeys<=0 时不按 buffer 规模触发 flush（仅定时）；shardCount<=0 时默认 16。
func NewStatsWorker(mysqlRepo *database.ArticleRepository, interval time.Duration, maxKeys, shardCount int) *StatsWorker {
	if interval <= 0 {
		interval = 1 * time.Minute
	}
	if shardCount <= 0 {
		shardCount = 16
	}
	shards := make([]*statsShard, shardCount)
	for i := range shards {
		shards[i] = &statsShard{
			buf:      newStatsBuffer(),
			flushSig: make(chan struct{}, 1),
		}
	}
	return &StatsWorker{
		shards:        shards,
		shardCount:    shardCount,
		flushInterval: interval,
		flushMaxKeys:  maxKeys,
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

	switch msg.Type {
	case "view", "like", "unlike":
	default:
		zap.L().Warn("未知的文章事件类型，直接确认丢弃", zap.String("type", msg.Type))
		return nil
	}

	sh := w.shards[statsShardIndex(msg.ArticleID, w.shardCount)]
	sh.mu.Lock()
	sh.buf.apply(msg)
	approxKeys := sh.buf.approxKeys()
	sh.mu.Unlock()

	if w.flushMaxKeys > 0 && approxKeys >= w.flushMaxKeys {
		select {
		case sh.flushSig <- struct{}{}:
		default:
		}
	}

	return nil
}

// StartFlushTicks 每个分片独立定时/定量触发 flush（二者 OR）。
func (w *StatsWorker) StartFlushTicks() {
	for _, sh := range w.shards {
		shard := sh
		ticker := time.NewTicker(w.flushInterval)
		go func() {
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					w.flushShard(shard)
				case <-shard.flushSig:
					w.flushShard(shard)
				}
			}
		}()
	}
}

func (w *StatsWorker) flushShard(sh *statsShard) {
	drained := sh.swapBuffers()
	if drained.isEmpty() {
		return
	}

	views, likes, likeRows, unlikeRows := drained.toFlushPayload()
	if err := w.articleDB.BatchFlushArticleActions(views, likes, likeRows, unlikeRows); err != nil {
		sh.mergeBack(drained)
		zap.L().Warn("文章统计/点赞批量落库失败，将合并回分片缓冲等待下次重试", zap.Error(err))
		return
	}

	zap.L().Debug("文章统计分片批量落库完成",
		zap.Int("views", len(views)),
		zap.Int("likes", len(likes)),
		zap.Int("like_rows", len(likeRows)),
		zap.Int("unlike_rows", len(unlikeRows)))
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
