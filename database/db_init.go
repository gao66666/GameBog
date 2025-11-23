package database

import (
	"fmt"

	"github.com/gao66666/GoBlog/setting"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var db *gorm.DB

type DataDriver struct {
	mysqlRepo *UserRepository //存储用户的相关数据
	redisRepo *RedisRepository
}

func NewDataDriver(mysqlRepo *UserRepository, redisRepo *RedisRepository) *DataDriver {
	return &DataDriver{mysqlRepo: mysqlRepo, redisRepo: redisRepo}
}

func MysqlInit(cfg *setting.MySQLConfig) *UserRepository {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.DB)

	// 连接数据库
	var err error
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		panic("连接数据库失败: " + err.Error())
	}
	fmt.Println("数据库连接成功")

	// 创建UserRepository
	userRepo := NewUserRepository(db)

	// 初始化表
	err = userRepo.InitTable()
	if err != nil {
		panic("初始化表失败: " + err.Error())
	}
	fmt.Println("数据库初始化测试完成!")
	return userRepo
}
