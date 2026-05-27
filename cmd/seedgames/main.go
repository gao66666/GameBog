// 从文本种子批量写入游戏表（并触发话题、Redis 缓存失效）。
// 用法：在项目根目录执行
//
//	go run ./cmd/seedgames -config ./setting/common.yaml -file ./data/games_seed.txt
//
// 文本格式见 data/games_seed.txt，字段以 | 分隔。
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/logger"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/setting"
	"github.com/gao66666/GoBlog/service"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/datatypes"
)

func main() {
	cfgPath := flag.String("config", "./setting/common.yaml", "配置文件路径")
	seedFile := flag.String("file", "data/games_seed.txt", "游戏种子文件（| 分隔）")
	flag.Parse()

	if err := setting.Init(*cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, "load config:", err)
		os.Exit(1)
	}
	if err := logger.Init(setting.Conf.LogConfig, setting.Conf.Mode); err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		os.Exit(1)
	}
	if err := tool.InitSnowflake(setting.Conf.MachineID); err != nil {
		fmt.Fprintln(os.Stderr, "snowflake:", err)
		os.Exit(1)
	}

	rdb, err := database.RedisInit(setting.Conf.RedisConfig)
	if err != nil {
		zap.L().Fatal("redis", zap.Error(err))
	}
	db := database.MysqlInit(setting.Conf.MySQLConfig)

	gameRepo := database.NewGameRepository(db)
	gameStoreRepo := database.NewGameStoreRepository(db)
	gameRedis := database.NewRedisGameRepository(rdb)
	topicRepo := database.NewTopicRepository(db)
	userRepo := database.NewUserRepository(db)
	svc := service.NewGameService(gameRepo, gameStoreRepo, gameRedis, topicRepo, userRepo)

	if err := gameRepo.InitTable(); err != nil {
		zap.L().Warn("InitTable", zap.Error(err))
	}
	if err := gameStoreRepo.InitTable(); err != nil {
		zap.L().Warn("game store InitTable", zap.Error(err))
	}

	f, err := os.Open(*seedFile)
	if err != nil {
		zap.L().Fatal("open seed file", zap.String("path", *seedFile), zap.Error(err))
	}
	defer f.Close()

	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			zap.L().Warn("skip: need at least 5 fields", zap.String("line", line))
			continue
		}
		name := strings.TrimSpace(parts[0])
		desc := strings.TrimSpace(parts[1])
		dateStr := strings.TrimSpace(parts[2])
		pub := strings.TrimSpace(parts[3])
		dev := strings.TrimSpace(parts[4])
		achJSON := "[]"
		if len(parts) > 5 && strings.TrimSpace(parts[5]) != "" {
			achJSON = strings.TrimSpace(parts[5])
		}
		coverURL := ""
		if len(parts) > 6 {
			coverURL = strings.TrimSpace(parts[6])
		}
		priceCents := int64(-1)
		if len(parts) > 7 {
			pc := strings.TrimSpace(parts[7])
			if pc != "" && pc != "-" {
				if pc == "0" || pc == "免费" {
					priceCents = 0
				} else if yuan, e := strconv.ParseInt(pc, 10, 64); e == nil {
					priceCents = yuan * 100
				}
			}
		}
		var tagsJSON datatypes.JSON
		if len(parts) > 8 && strings.TrimSpace(parts[8]) != "" {
			raw := strings.TrimSpace(parts[8])
			chunks := strings.Split(raw, ",")
			var tags []string
			for _, ch := range chunks {
				t := strings.TrimSpace(ch)
				if t != "" {
					tags = append(tags, t)
				}
			}
			b, _ := json.Marshal(tags)
			tagsJSON = datatypes.JSON(b)
		} else {
			tagsJSON = datatypes.JSON([]byte("[]"))
		}

		var releaseAt time.Time
		if dateStr != "" {
			t, e := time.ParseInLocation("2006-01-02", dateStr, time.Local)
			if e != nil {
				zap.L().Warn("bad date", zap.String("raw", dateStr))
			} else {
				releaseAt = t
			}
		}

		game := &models.Game{
			ID:           tool.GenerateID(),
			Name:         name,
			Description:  desc,
			ReleaseAt:    releaseAt,
			Publisher:    pub,
			Developer:    dev,
			CoverURL:     coverURL,
			PriceCents:   priceCents,
			Tags:         tagsJSON,
			Achievements: datatypes.JSON([]byte(achJSON)),
		}
		if err := svc.CreateGame(game); err != nil {
			zap.L().Warn("create failed", zap.String("name", name), zap.Error(err))
			continue
		}
		n++
		zap.L().Info("seeded", zap.String("name", name), zap.Uint64("id", game.ID))
	}
	if err := sc.Err(); err != nil {
		zap.L().Fatal("read file", zap.Error(err))
	}
	fmt.Printf("完成：写入 %d 条游戏（重复执行会产生重复数据，请按需清库后再灌）\n", n)
}
