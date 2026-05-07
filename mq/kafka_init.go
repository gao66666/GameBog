package mq

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"strconv"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

const (
	TopicNotificationPush  = "notification.push"
	TopicNotificationStore = "notification.store"
	TopicSearchPush        = "search.push"
	TopicPointsSettle      = "points.settle"
)

type NotificationPayload struct {
	EventID     uint64 `json:"event_id"`
	UserID      uint64 `json:"user_id"`
	SenderID    uint64 `json:"sender_id"`
	SenderName  string `json:"sender_name"`
	Content     string `json:"content"`
	Type        string `json:"type"` // follow, like, system, comment, reply
	CreatedAt   int64  `json:"created_at"`
}

type NotificationProcessor interface {
	ProcessNotificationMessage(ctx context.Context, payload []byte) error
}

type NotificationStoreProcessor interface {
	ProcessNotificationStoreMessage(ctx context.Context, payload []byte) error
}

type SearchSyncProcessor interface {
	ProcessSearchSyncMessage(ctx context.Context, payload []byte) error
}

// PointsSettleProcessor 积分结算消费者接口
type PointsSettleProcessor interface {
	ProcessPointsSettleMessage(ctx context.Context, payload []byte) error
}

var (
	kafkaBrokers []string
	kafkaGroupID string

	kafkaCtx    context.Context
	kafkaCancel context.CancelFunc
	kafkaWG     sync.WaitGroup

	notificationWriter      *kafka.Writer
	notificationStoreWriter *kafka.Writer
	searchWriter            *kafka.Writer
	pointsSettleWriter      *kafka.Writer
)

func resolveBrokers(brokers []string) []string {
	resolved := make([]string, 0, len(brokers))
	for _, b := range brokers {
		host, port, err := net.SplitHostPort(b)
		if err != nil {
			resolved = append(resolved, b)
			continue
		}
		// 已经是 IP 地址，不需要解析
		if net.ParseIP(host) != nil {
			resolved = append(resolved, b)
			continue
		}
		// 尝试 DNS 解析，失败则降级到 127.0.0.1
		_, err = net.LookupHost(host)
		if err != nil {
			zap.L().Info("Kafka broker host 解析失败，降级到 127.0.0.1",
				zap.String("original", b), zap.Error(err))
			resolved = append(resolved, "127.0.0.1:"+port)
			continue
		}
		resolved = append(resolved, b)
	}
	zap.L().Info("Kafka brokers 解析结果",
		zap.Strings("original", brokers),
		zap.Strings("resolved", resolved))
	return resolved
}

func InitKafka(brokers []string, groupID string) error {
	if len(brokers) == 0 {
		return errors.New("kafka brokers is empty")
	}
	if groupID == "" {
		return errors.New("kafka groupID is empty")
	}

	brokers = resolveBrokers(brokers)

	kafkaBrokers = append([]string(nil), brokers...)
	kafkaGroupID = groupID

	if kafkaCancel != nil {
		kafkaCancel()
	}
	kafkaCtx, kafkaCancel = context.WithCancel(context.Background())

	notificationWriter = &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        TopicNotificationPush,
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireOne,
		BatchTimeout: 50 * time.Millisecond,
	}
	notificationStoreWriter = &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        TopicNotificationStore,
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireOne,
		BatchTimeout: 100 * time.Millisecond,
	}
	searchWriter = &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        TopicSearchPush,
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireOne,
		BatchTimeout: 100 * time.Millisecond,
	}

	pointsSettleWriter = &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        TopicPointsSettle,
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireAll, // acks=all，确保 leader 和副本都收到
		BatchTimeout: 100 * time.Millisecond,
	}

	return nil
}

func CloseKafka() {
	if kafkaCancel != nil {
		kafkaCancel()
		kafkaCancel = nil
	}
	kafkaWG.Wait()

	if notificationWriter != nil {
		_ = notificationWriter.Close()
		notificationWriter = nil
	}
	if notificationStoreWriter != nil {
		_ = notificationStoreWriter.Close()
		notificationStoreWriter = nil
	}
	if searchWriter != nil {
		_ = searchWriter.Close()
		searchWriter = nil
	}
	if pointsSettleWriter != nil {
		_ = pointsSettleWriter.Close()
		pointsSettleWriter = nil
	}
}

func StartKafkaConsumers(notification NotificationProcessor, notificationStore NotificationStoreProcessor, search SearchSyncProcessor, pointsSettle ...PointsSettleProcessor) error {
	if kafkaCancel == nil || kafkaCtx == nil {
		return errors.New("kafka is not initialized")
	}

	if notification != nil {
		kafkaWG.Add(1)
		go func() {
			defer kafkaWG.Done()
			consumeLoop(kafkaCtx, kafkaBrokers, kafkaGroupID, TopicNotificationPush, func(ctx context.Context, value []byte) error {
				return notification.ProcessNotificationMessage(ctx, value)
			})
		}()
	}
	if notificationStore != nil {
		kafkaWG.Add(1)
		go func() {
			defer kafkaWG.Done()
			consumeLoop(kafkaCtx, kafkaBrokers, kafkaGroupID, TopicNotificationStore, func(ctx context.Context, value []byte) error {
				return notificationStore.ProcessNotificationStoreMessage(ctx, value)
			})
		}()
	}
	if search != nil {
		kafkaWG.Add(1)
		go func() {
			defer kafkaWG.Done()
			consumeLoop(kafkaCtx, kafkaBrokers, kafkaGroupID, TopicSearchPush, func(ctx context.Context, value []byte) error {
				return search.ProcessSearchSyncMessage(ctx, value)
			})
		}()
	}
	if len(pointsSettle) > 0 && pointsSettle[0] != nil {
		kafkaWG.Add(1)
		go func() {
			defer kafkaWG.Done()
			consumeLoop(kafkaCtx, kafkaBrokers, kafkaGroupID, TopicPointsSettle, func(ctx context.Context, value []byte) error {
				return pointsSettle[0].ProcessPointsSettleMessage(ctx, value)
			})
		}()
	}
	return nil
}

func consumeLoop(ctx context.Context, brokers []string, groupID string, topic string, processor func(context.Context, []byte) error) {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		GroupID:        groupID,
		Topic:          topic,
		MinBytes:       1e3,
		MaxBytes:       10e6,
		CommitInterval: 0, // 手动提交，确保处理成功后再 commit
	})
	defer r.Close()

	var (
		consecutiveFetchErr int
		lastErrLogAt        time.Time
	)

	for {
		m, err := r.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			consecutiveFetchErr++
			now := time.Now()
			if lastErrLogAt.IsZero() || now.Sub(lastErrLogAt) >= 10*time.Second {
				// 第一次用 WARN，后续用 INFO（避免 Kafka 未启动时刷屏）
				if consecutiveFetchErr <= 1 {
					zap.L().Warn("Kafka fetch message failed", zap.String("topic", topic), zap.Error(err))
				} else {
					zap.L().Info("Kafka fetch retrying", zap.String("topic", topic), zap.Error(err))
				}
				lastErrLogAt = now
			}

			backoff := 500 * time.Millisecond
			// 指数退避，上限 30s
			shift := consecutiveFetchErr - 1
			if shift > 6 {
				shift = 6
			}
			if shift > 0 {
				backoff = backoff * time.Duration(1<<shift)
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
			time.Sleep(backoff)
			continue
		}
		consecutiveFetchErr = 0

		if err := processor(ctx, m.Value); err != nil {
			zap.L().Error("Kafka message process failed", zap.String("topic", topic), zap.Error(err))
			// 不 commit，让其后续重试（可能会导致重复投递处理，业务需幂等）
			time.Sleep(1 * time.Second)
			continue
		}

		if err := r.CommitMessages(ctx, m); err != nil {
			zap.L().Warn("Kafka commit failed", zap.String("topic", topic), zap.Error(err))
		}
	}
}

func PublishNotification(userID uint64, senderID uint64, senderName, content string, msgType string) error {
	if notificationWriter == nil {
		return errors.New("kafka notification writer is not initialized")
	}

	payload, err := json.Marshal(NotificationPayload{
		EventID:   tool.GenerateID(),
		UserID:    userID,
		SenderID:  senderID,
		SenderName: senderName,
		Content:   content,
		Type:      msgType,
		CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		return err
	}

	return notificationWriter.WriteMessages(context.Background(), kafka.Message{Value: payload})
}

func PublishNotificationStore(data NotificationPayload) error {
	if notificationStoreWriter == nil {
		return errors.New("kafka notification store writer is not initialized")
	}
	if data.EventID == 0 {
		data.EventID = tool.GenerateID()
	}
	if data.CreatedAt == 0 {
		data.CreatedAt = time.Now().Unix()
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return notificationStoreWriter.WriteMessages(context.Background(), kafka.Message{Key: []byte(strconv.FormatUint(data.UserID, 10)), Value: payload})
}

func PublishPointsSettle(data *PointsSettleMsg) error {
	if pointsSettleWriter == nil {
		return errors.New("kafka points settle writer is not initialized")
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return pointsSettleWriter.WriteMessages(context.Background(), kafka.Message{
		Key:   []byte(strconv.FormatUint(data.UserID, 10)),
		Value: payload,
	})
}

func PublishSearchSync(data *models.SearchSyncPayload) error {
	if searchWriter == nil {
		return errors.New("kafka search writer is not initialized")
	}
	if data == nil {
		return errors.New("search sync payload is nil")
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}

	key := []byte("")
	if data.ID != 0 {
		key = []byte(strconv.FormatUint(data.ID, 10))
	}
	return searchWriter.WriteMessages(context.Background(), kafka.Message{Key: key, Value: payload})
}
