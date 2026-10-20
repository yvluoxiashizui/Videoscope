package services

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// 分片上传的临时状态。
//
// 用 Redis Set 存已上传的分片序号，key 是 upload:chunks:{md5}。
// 选 Set 而不是 List 或 Hash 的原因：
// 弱网下前端可能重复发送同一分片，Set 天然保证序号唯一，
// 不需要额外的去重逻辑；查询「哪些已传」也是 O(1) 取全部成员。
const (
	chunkKeyPrefix = "upload:chunks:"
	chunkTTL       = 24 * time.Hour
)

func chunkKey(md5 string) string {
	return chunkKeyPrefix + md5
}

// MarkChunkUploaded 记录一个分片已落盘。
// 必须在上传成功后调用——先落盘后记账，
// 反过来的话会出现「记录了但对象不存在」的假上传，
// 续传时跳过该分片，最终合并出来是残缺文件。
func MarkChunkUploaded(ctx context.Context, md5 string, index int) error {
	key := chunkKey(md5)
	if err := rdb.SAdd(ctx, key, index).Err(); err != nil {
		return fmt.Errorf("记录分片序号: %w", err)
	}
	// 每次写入都刷新过期时间，避免用户在传输过程中状态被提前清掉。
	return rdb.Expire(ctx, key, chunkTTL).Err()
}

// UploadedChunks 返回已上传的分片序号。
// 没有记录时返回空切片而不是错误——对调用方来说「一个都没传」是正常状态。
func UploadedChunks(ctx context.Context, md5 string) ([]int, error) {
	members, err := rdb.SMembers(ctx, chunkKey(md5)).Result()
	if err != nil {
		return nil, fmt.Errorf("读取分片序号: %w", err)
	}

	indexes := make([]int, 0, len(members))
	for _, m := range members {
		n, err := strconv.Atoi(m)
		if err != nil {
			// 理论不会发生，出现了说明有脏数据，跳过即可。
			continue
		}
		indexes = append(indexes, n)
	}
	return indexes, nil
}

// UploadedCount 返回已上传分片数量，用于合并前的完整性校验。
func UploadedCount(ctx context.Context, md5 string) (int64, error) {
	n, err := rdb.SCard(ctx, chunkKey(md5)).Result()
	if err != nil {
		return 0, fmt.Errorf("统计分片数量: %w", err)
	}
	return n, nil
}

// ClearChunks 清除分片记录，合并成功后调用。
func ClearChunks(ctx context.Context, md5 string) error {
	return rdb.Del(ctx, chunkKey(md5)).Err()
}

// 合并锁的 key 前缀。
// 前端在超时后会重试合并请求，没有锁的话两次合并并发执行，
// 对象存储侧会因为目标对象被同时写入而报状态机冲突。
const mergeLockPrefix = "lock:merge:"

// TryLockMerge 尝试获取合并锁，返回是否拿到。
func TryLockMerge(ctx context.Context, md5 string, ttl time.Duration) (bool, error) {
	return rdb.SetNX(ctx, mergeLockPrefix+md5, 1, ttl).Result()
}

// UnlockMerge 释放合并锁。
func UnlockMerge(ctx context.Context, md5 string) error {
	return rdb.Del(ctx, mergeLockPrefix+md5).Err()
}
