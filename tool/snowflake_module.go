package tool

import (
	"sync"

	"github.com/bwmarrin/snowflake"
	"go.uber.org/zap"
)

var (
	snowflakeNode *snowflake.Node
	once          sync.Once
)

// InitSnowflake 初始化雪花算法节点
func InitSnowflake(nodeID int64) error {
	var err error
	once.Do(func() {
		snowflakeNode, err = snowflake.NewNode(nodeID)
	})
	return err
}

// GenerateID 生成ID
func GenerateID() uint64 {
	if snowflakeNode == nil {
		zap.L().Info("雪花算法未初始化")
	}
	return uint64(snowflakeNode.Generate().Int64())
}
