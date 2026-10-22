package services

import (
	"context"
	"encoding/json"
	"log"
	"strconv"

	"github.com/redis/go-redis/v9"

	"videoscope/models"
)

// 任务事件通道前缀。
//
// 用 Redis 的发布订阅做状态广播，而不是让前端轮询查库：
// 轮询在任务处理期间是纯粹的无效请求——几十分钟里绝大多数请求
// 查到的状态都没变，既浪费连接也浪费数据库。
//
// 每个任务一个频道，订阅方只会收到自己关心的那条任务的事件。
const taskEventPrefix = "events:task:"

// TaskEvent 是推给客户端的一条状态变更。
//
// 只带展示需要的最小字段：客户端不需要知道内部状态码的含义，
// 所以同时给一个可以直接显示的中文描述。
type TaskEvent struct {
	TaskID uint   `json:"task_id"`
	Status int8   `json:"status"`
	Text   string `json:"text"`
	ErrMsg string `json:"err_msg,omitempty"`
}

// Terminal 判断这条事件是否代表任务的终态。
// 客户端收到终态事件后可以主动断开，不必一直挂着连接。
func (e TaskEvent) Terminal() bool {
	return e.Status == 3 || e.Status == 4
}

func taskEventChannel(taskID uint) string {
	return taskEventPrefix + strconv.FormatUint(uint64(taskID), 10)
}

// PublishTaskEvent 发布一条状态变更事件。
//
// 发布失败不影响主流程：状态已经落库，事件只是通知手段。
// 丢一条通知的后果是客户端多等一会儿，而不是数据出错。
func PublishTaskEvent(ctx context.Context, event TaskEvent) {
	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("序列化任务事件失败 task=%d: %v", event.TaskID, err)
		return
	}
	if err := rdb.Publish(ctx, taskEventChannel(event.TaskID), payload).Err(); err != nil {
		log.Printf("发布任务事件失败 task=%d: %v", event.TaskID, err)
	}
}

// SubscribeTaskEvents 订阅某个任务的状态变更。
// 调用方负责关闭返回的 PubSub。
func SubscribeTaskEvents(ctx context.Context, taskID uint) *redis.PubSub {
	return rdb.Subscribe(ctx, taskEventChannel(taskID))
}

// PublishTransition 在状态推进成功后发布事件。
//
// 单独抽出来是因为 Transition 不接收 context——
// 状态推进是数据库操作，事件发布是通知，两者生命周期不同，
// 用一个独立的短超时 context 比复用请求 context 更稳。
func PublishTransition(taskID uint, to int8, errMsg string) {
	PublishTaskEvent(context.Background(), TaskEvent{
		TaskID: taskID,
		Status: to,
		Text:   models.StatusText(to),
		ErrMsg: errMsg,
	})
}
