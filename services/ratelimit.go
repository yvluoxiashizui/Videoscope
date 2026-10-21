package services

import (
	"context"
	_ "embed"
	"fmt"
	"time"
)

//go:embed scripts/token_bucket.lua
var tokenBucketScript string

// 限流参数。
//
// 速率与容量的关系决定了「稳态速率」和「允许多大的突发」：
// 每秒两个、容量十个，意味着持续请求时平均每秒放行两个，
// 但用户手动连点几下不会立刻被拒——桶里攒着令牌。
const (
	AnalyzeRate     = 2  // 每秒补充的令牌数
	AnalyzeCapacity = 10 // 桶容量，即最大突发量
)

// AnalyzeBucketKey 返回某个用户的分析接口限流键。
//
// 按用户维度而不是接口维度：目的是控制单个用户对模型调用的消耗，
// 不是控制接口总流量。全局限流在网关层做，这里管的是成本。
func AnalyzeBucketKey(userID uint) string {
	return fmt.Sprintf("rate:analyze:%d", userID)
}

// Allow 判断一次请求是否放行。
//
// 返回错误表示限流组件本身不可用。调用方需要区分
// 「被限流」和「限流器故障」——前者拒绝请求，后者应当降级放行，
// 否则 Redis 挂掉会把整个分析功能一起带走。
func Allow(ctx context.Context, key string, rate float64, capacity int) (bool, error) {
	now := time.Now().UnixMilli()

	res, err := rdb.Eval(ctx, tokenBucketScript, []string{key},
		rate, capacity, now,
	).Int()
	if err != nil {
		return false, fmt.Errorf("执行令牌桶脚本: %w", err)
	}
	return res == 1, nil
}
