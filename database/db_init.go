package database

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gao66666/GoBlog/setting"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type Repository struct {
	db *gorm.DB
}

// NewRepository 创建基础仓库实例
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

var DB *gorm.DB

func MysqlInit(cfg *setting.MySQLConfig) *gorm.DB {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.DB)

	var err error
	newLogger := gormlogger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		gormlogger.Config{
			SlowThreshold:             1 * time.Second,
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)
	// 使用连接池配置来优化性能
	instance, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		// 可以在这里增加一些全局配置，比如禁用外键约束等
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   newLogger,
	})

	if err != nil {
		panic("连接数据库失败: " + err.Error())
	}

	// 设置连接池参数（大厂必备，防止连接数爆炸或失效）
	sqlDB, _ := instance.DB()
	sqlDB.SetMaxIdleConns(10)           // 最大空闲连接
	sqlDB.SetMaxOpenConns(100)          // 最大打开连接
	sqlDB.SetConnMaxLifetime(time.Hour) // 连接最长存活时间

	fmt.Println("✅ 数据库连接成功")

	DB = instance
	return instance
}
