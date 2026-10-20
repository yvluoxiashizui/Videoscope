package services

import "github.com/redis/go-redis/v9"

// rdb 全局 Redis 客户端，由 main 注入。
var rdb *redis.Client

// SetRedis 注入 Redis 客户端。
func SetRedis(c *redis.Client) {
	rdb = c
}
