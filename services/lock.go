package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrLockBusy 表示锁已被其他持有者占用。
var ErrLockBusy = errors.New("锁已被占用")

// 锁的过期时间与续期间隔。
//
// 续期间隔取过期时间的三分之一：即使一次续期因网络抖动失败，
// 仍有一次重试机会，不会因单次失败就让锁提前失效。
// 过期时间本身不是任务耗时的预估值，而是「进程异常时最多卡多久」的上限——
// 设得太长，崩溃后别的节点要等很久；设得太短，续期压力大。
const (
	DefaultLockTTL    = 30 * time.Second
	lockRenewInterval = DefaultLockTTL / 3
)

// 续期与释放都要「先比较再操作」，必须原子完成。
//
// 比较和操作分成两条命令的话，中间存在窗口：
// 比较通过后锁刚好过期并被他人获取，随后的删除就会删掉别人的锁。
var (
	extendScript = redis.NewScript(`
if redis.call('get', KEYS[1]) == ARGV[1] then
    return redis.call('pexpire', KEYS[1], ARGV[2])
end
return 0
`)
	releaseScript = redis.NewScript(`
if redis.call('get', KEYS[1]) == ARGV[1] then
    return redis.call('del', KEYS[1])
end
return 0
`)
)

// Lock 是一把带自动续期的分布式锁。
type Lock struct {
	key    string
	token  string
	cancel context.CancelFunc
	done   chan struct{}
	lost   bool
}

// AcquireLock 尝试获取锁，拿不到返回 ErrLockBusy。
//
// 锁的值是一个随机串而不是固定值，用来标识持有者。
// 假设 A 拿到锁后业务阻塞超过过期时间，锁自动释放，B 拿到同一把锁；
// 此时 A 恢复执行并尝试释放，如果直接删除，删掉的是 B 的锁。
// 带上持有者标识后，释放操作能判断这把锁是否还属于自己。
func AcquireLock(ctx context.Context, key string, ttl time.Duration) (*Lock, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}

	ok, err := rdb.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return nil, fmt.Errorf("获取锁: %w", err)
	}
	if !ok {
		return nil, ErrLockBusy
	}

	renewCtx, cancel := context.WithCancel(context.Background())
	l := &Lock{
		key:    key,
		token:  token,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go l.renewLoop(renewCtx, ttl)
	return l, nil
}

// renewLoop 周期性延长锁的有效期。
//
// goroutine 必须有退出条件，否则每次加锁都会泄漏一个，
// 服务长期运行后数量持续累积。
func (l *Lock) renewLoop(ctx context.Context, ttl time.Duration) {
	defer close(l.done)

	ticker := time.NewTicker(lockRenewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !l.extend(ctx, ttl) {
				// 续期失败说明锁已不属于自己，停止续期。
				// 持有者需要在下一次操作前检查 Lost，避免在无锁状态下继续写。
				l.lost = true
				return
			}
		}
	}
}

// extend 延长锁的有效期，返回是否成功。
func (l *Lock) extend(ctx context.Context, ttl time.Duration) bool {
	res, err := extendScript.Run(ctx, rdb, []string{l.key}, l.token, ttl.Milliseconds()).Int()
	if err != nil {
		// 网络抖动导致的失败不立刻判定为丢失，交给下一次 tick 重试。
		return true
	}
	return res == 1
}

// Lost 返回锁是否已经丢失。业务在执行关键操作前应当检查。
func (l *Lock) Lost() bool {
	return l.lost
}

// Release 停止续期并释放锁。
//
// 即使锁已丢失也会停止续期 goroutine，释放操作本身是幂等的：
// 锁不属于自己时脚本什么都不做。
func (l *Lock) Release(ctx context.Context) error {
	l.cancel()
	<-l.done

	return releaseScript.Run(ctx, rdb, []string{l.key}, l.token).Err()
}

// randomToken 生成锁的持有者标识。
func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成锁标识: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// VideoLockKey 返回某个视频的处理锁键。
// 以内容摘要为键，同一视频无论被投递多少次，同一时刻只有一条链路在处理。
func VideoLockKey(md5 string) string {
	return "lock:video:" + md5
}

// ErrVideoBusy 表示该视频正被其他实例处理。
var ErrVideoBusy = errors.New("该视频正在处理中")
