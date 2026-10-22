package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"videoscope/models"
	"videoscope/services"
)

// 心跳间隔。
//
// SSE 是长连接，中间可能经过代理或负载均衡，空闲时间过长会被判定超时断开。
// 定期发一行注释（以冒号开头，客户端会忽略）就能维持连接。
const sseHeartbeat = 20 * time.Second

// TaskEvents 用 SSE 推送某个任务的状态变化。
//
// 选择 SSE 而不是轮询或 WebSocket：轮询在任务处理期间绝大多数请求
// 查到的状态都没变，是纯浪费；WebSocket 是双向协议，而这个场景只有
// 服务端往客户端推，用不上双向能力，反而要多维护一套握手与心跳逻辑。
func TaskEvents(c *gin.Context) {
	taskID, err := parseUint(c.Param("id"))
	if err != nil {
		fail(c, http.StatusBadRequest, "任务 ID 错误")
		return
	}

	task, err := services.GetTask(taskID)
	if err != nil {
		fail(c, http.StatusNotFound, "任务不存在")
		return
	}
	if task.UserID != CurrentUserID(c) {
		fail(c, http.StatusForbidden, "无权访问该任务")
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	// 关闭反向代理的响应缓冲，否则事件会被攒起来一起发，失去实时性。
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		fail(c, http.StatusInternalServerError, "当前环境不支持流式响应")
		return
	}

	ctx := c.Request.Context()

	// 先推一次当前状态。
	//
	// 这一步不能省：用户可能在任务已经完成之后才建立连接，
	// 此时后续不会再有事件，只订阅就会一直挂着。
	current := services.TaskEvent{
		TaskID: task.ID,
		Status: task.Status,
		Text:   models.StatusText(task.Status),
		ErrMsg: task.ErrMsg,
	}
	sendTaskEvent(c, flusher, current)

	// 已经是终态就不必订阅。
	if current.Terminal() {
		return
	}

	sub := services.SubscribeTaskEvents(ctx, task.ID)
	defer sub.Close()

	messages := sub.Channel()
	ticker := time.NewTicker(sseHeartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// 客户端主动断开。
			return

		case <-ticker.C:
			if _, err := c.Writer.WriteString(": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()

		case msg, ok := <-messages:
			if !ok {
				return
			}
			var event services.TaskEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				// 单条事件解析失败不影响后续，跳过即可。
				continue
			}
			sendTaskEvent(c, flusher, event)
			if event.Terminal() {
				return
			}
		}
	}
}

// sendTaskEvent 写一条 SSE 事件并立即刷出。
//
// 手动拼 SSE 格式而不是用框架封装，是为了控制刷出的时机：
// 缓冲没冲掉的话客户端收不到，实时推送就变成了批量延迟。
func sendTaskEvent(c *gin.Context, flusher http.Flusher, event services.TaskEvent) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}

	if _, err := c.Writer.WriteString("event: status\ndata: "); err != nil {
		return
	}
	if _, err := c.Writer.Write(payload); err != nil {
		return
	}
	if _, err := c.Writer.WriteString("\n\n"); err != nil {
		return
	}
	flusher.Flush()
}
