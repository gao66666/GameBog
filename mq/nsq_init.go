package mq

import (
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/nsqio/go-nsq"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// 全局生产者变量
var producer *nsq.Producer

// WorkerFlushOptions 统计类 NSQ 消费者的 flush：定时与定量 OR；MaxKeys<=0 时仅定时触发。
type WorkerFlushOptions struct {
	StatsInterval   time.Duration
	StatsMaxKeys    int
	StatsShardCount int
	CommentInterval time.Duration
	CommentMaxKeys  int
}

func InitNSQ(addr string, db *gorm.DB, rdb *redis.Client, flush WorkerFlushOptions) {
	if flush.StatsInterval <= 0 {
		flush.StatsInterval = time.Minute
	}
	if flush.CommentInterval <= 0 {
		flush.CommentInterval = flush.StatsInterval
	}

	// 1. 初始化生产者
	initProducer(addr)

	// 2. 注册：文章统计消费者 (处理点赞、阅读量)
	articleRepo := database.NewArticleRepository(db)
	worker := NewStatsWorker(articleRepo, flush.StatsInterval, flush.StatsMaxKeys, flush.StatsShardCount)
	worker.StartFlushTicks()
	registerConsumer(addr, "article_stats", "stats_sync_group", worker)

	// 3. 注册：评论点赞消费者
	commentRepo := database.NewCommentRepository(db)
	commentWorker := NewCommentStatsWorker(commentRepo, flush.CommentInterval, flush.CommentMaxKeys)
	commentWorker.StartFlushTicks()
	registerConsumer(addr, "comment_stats", "comment_stats_group", commentWorker)

	zap.L().Info("所有 NSQ 服务初始化完成",
		zap.Duration("stats_flush_interval", flush.StatsInterval),
		zap.Int("stats_flush_max_keys", flush.StatsMaxKeys),
		zap.Int("stats_shard_count", worker.shardCount),
		zap.Duration("comment_flush_interval", flush.CommentInterval),
		zap.Int("comment_flush_max_keys", flush.CommentMaxKeys))
}

// initProducer 封装生产者的初始化逻辑
func initProducer(addr string) {
	config := nsq.NewConfig()
	config.HeartbeatInterval = 10 * time.Second
	config.DialTimeout = 5 * time.Second

	var err error
	producer, err = nsq.NewProducer(addr, config)
	if err != nil {
		zap.L().Error("无法创建 NSQ 生产者", zap.Error(err))
		return
	}

	if err := producer.Ping(); err != nil {
		zap.L().Error("NSQ 生产者连通性测试失败", zap.Error(err))
		return
	}
	zap.L().Info("NSQ 生产者已就位", zap.String("addr", addr))
}

// registerConsumer 封装消费者的初始化逻辑（核心封装）
// addr: nsqd地址, topic: 话题, channel: 频道, handler: 处理逻辑的Worker
func registerConsumer(addr, topic, channel string, handler nsq.Handler) {
	config := nsq.NewConfig()
	// 注意：这里的 LookupdPollInterval 等参数可以根据需要在这里统一设置

	c, err := nsq.NewConsumer(topic, channel, config)
	if err != nil {
		zap.L().Error("无法创建 NSQ 消费者",
			zap.String("topic", topic),
			zap.String("channel", channel),
			zap.Error(err))
		return
	}

	// 绑定处理逻辑
	c.AddHandler(handler)

	// 连接到 nsqd
	if err := c.ConnectToNSQD(addr); err != nil {
		zap.L().Error("消费者连接 nsqd 失败",
			zap.String("topic", topic),
			zap.Error(err))
		return
	}

	zap.L().Info("NSQ 消费者已启动并开始监听",
		zap.String("topic", topic),
		zap.String("channel", channel))
}

// Publish 发送通用消息 (会自动转为 JSON)
func Publish(topic string, data interface{}) error {
	if producer == nil {
		log.Println(" NSQ 生产者未初始化")
		return errors.New("nsq producer is not initialized")
	}

	// 序列化消息体
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// 发送消息
	return producer.Publish(topic, body)
}

func PublishAction(msg ArticleActionMsg) error {
	// 设置默认时间戳
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().Unix()
	}

	// 调用之前定义的通用 Publish 函数
	return Publish("article_stats", msg)
}

// PublishCommentAction 发布评论点赞消息
func PublishCommentAction(msg CommentActionMsg) error {
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().Unix()
	}
	return Publish("comment_stats", msg)
}

// Close 优雅关闭连接
func CloseNsq() {
	if producer != nil {
		log.Println(" 正在关闭 NSQ 生产者...")
		producer.Stop()
	}
}
