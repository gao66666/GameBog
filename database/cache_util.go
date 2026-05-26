package database

import (
	"context"

	"github.com/redis/go-redis/v9"
)

func deleteKeysByPattern(client *redis.Client, pattern string) error {
	if client == nil || pattern == "" {
		return nil
	}
	ctx := context.Background()
	var cursor uint64
	for {
		keys, next, err := client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return nil
}
