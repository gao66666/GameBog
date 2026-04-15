package bootstrap

import (
	"fmt"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/logger"
	"github.com/gao66666/GoBlog/mq"
	"github.com/gao66666/GoBlog/router"
	"github.com/gao66666/GoBlog/search"
	"github.com/gao66666/GoBlog/setting"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type Runtime struct {
	Engine *gin.Engine
	cfg    *setting.AppConfig
	db     *gorm.DB
}

func Init(configPath string) (*Runtime, error) {
	if err := setting.Init(configPath); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if err := logger.Init(setting.Conf.LogConfig, setting.Conf.Mode); err != nil {
		return nil, fmt.Errorf("init logger: %w", err)
	}

	if err := tool.InitSnowflake(setting.Conf.MachineID); err != nil {
		return nil, fmt.Errorf("init snowflake: %w", err)
	}

	redisClient, err := database.RedisInit(setting.Conf.RedisConfig)
	if err != nil {
		return nil, fmt.Errorf("init redis: %w", err)
	}

	dbClient := database.MysqlInit(setting.Conf.MySQLConfig)
	app := router.SetupApp(dbClient, redisClient)

	if setting.Conf.SearchConfig.Enabled {
		search.Init(setting.Conf.SearchConfig.Host, setting.Conf.SearchConfig.APIKey, setting.Conf.SearchConfig.Index)
	}

	if setting.Conf.KafkaConfig.Enabled {
		if err := mq.InitKafka(setting.Conf.KafkaConfig.Brokers, setting.Conf.KafkaConfig.GroupID); err != nil {
			return nil, fmt.Errorf("init kafka: %w", err)
		}
		app.StartWorkers()
		zap.L().Info("Kafka enabled", zap.Strings("brokers", setting.Conf.KafkaConfig.Brokers), zap.String("group_id", setting.Conf.KafkaConfig.GroupID))
	}

	if setting.Conf.NSQConfig.Enabled {
		mq.InitNSQ(setting.Conf.NSQConfig.Addr, dbClient, redisClient)
		zap.L().Info("NSQ enabled", zap.String("addr", setting.Conf.NSQConfig.Addr))
	}

	engine := router.RouterInit(setting.Conf.Mode, app)
	// 关闭开发环境的自动种子数据，避免默认生成用户和文章
	// 如需再次启用，可手动调用 seedDevData(dbClient)
	return &Runtime{
		Engine: engine,
		cfg:    setting.Conf,
		db:     dbClient,
	}, nil
}

func (r *Runtime) Addr() string {
	return fmt.Sprintf(":%d", r.cfg.Port)
}

func (r *Runtime) Cleanup() {
	mq.CloseKafka()
	mq.CloseNsq()
	if r.db != nil {
		sqlDB, err := r.db.DB()
		if err == nil {
			sqlDB.Close()
		}
	}
	zap.L().Sync()
}
