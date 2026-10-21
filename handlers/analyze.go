package handlers

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"videoscope/models"
	"videoscope/services"
)

// SubmitAnalyze 提交视频分析任务。
//
// 请求体：{"md5": "..."}
//
// 这是整个链路的入口，做四件事：限流、查任务、推进状态、投递消息。
// 顺序有讲究：限流放在最前面，避免被拒绝的请求还去查库；
// 状态推进在投递之前，投递失败再回滚，保证不会出现
// 「消息发出去了但状态还停在待分析」这种对不上的情况。
func SubmitAnalyze(c *gin.Context) {
	var req struct {
		MD5 string `json:"md5" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}

	fileMD5 := strings.ToLower(strings.TrimSpace(req.MD5))
	if !isMD5(fileMD5) {
		fail(c, http.StatusBadRequest, "md5 格式错误")
		return
	}

	ctx := c.Request.Context()
	userID := CurrentUserID(c)

	// 限流。按用户维度控制模型调用成本。
	allowed, err := services.Allow(ctx, services.AnalyzeBucketKey(userID),
		services.AnalyzeRate, services.AnalyzeCapacity)
	if err != nil {
		// 限流器故障时降级放行。
		// 拒绝会让分析功能完全不可用，而 Redis 抖动是暂时的；
		// 放行的代价只是短暂失去成本保护，比整条链路瘫痪轻。
		log.Printf("限流器异常，降级放行: %v", err)
	} else if !allowed {
		fail(c, http.StatusTooManyRequests, "操作过于频繁，请稍后再试")
		return
	}

	task, err := services.FindTaskByMD5(fileMD5)
	if err != nil {
		fail(c, http.StatusNotFound, "视频不存在，请先完成上传")
		return
	}

	// 已完成直接返回，不重复消耗算力。
	if task.Status == models.TaskDone {
		ok(c, gin.H{
			"task_id": task.ID,
			"status":  task.Status,
			"message": "该视频已完成分析",
		})
		return
	}

	// 正在处理或已排队：返回当前状态，不重复入队。
	if task.Status == models.TaskQueued || task.Status == models.TaskProcessing {
		ok(c, gin.H{
			"task_id": task.ID,
			"status":  task.Status,
			"message": "任务已在处理中",
		})
		return
	}

	// 只接受从「待分析」或「失败」推进到排队。
	from := task.Status
	if from != models.TaskPending && from != models.TaskFailed {
		fail(c, http.StatusConflict, "当前状态不允许提交分析")
		return
	}

	// 先推进状态。条件更新保证同一任务只有一个请求能推进成功，
	// 并发点击时其余请求会拿到状态冲突，不会重复入队。
	if err := services.Transition(task.ID, from, models.TaskQueued, ""); err != nil {
		if errors.Is(err, services.ErrStatusConflict) {
			fail(c, http.StatusConflict, "任务状态已变化，请刷新后重试")
			return
		}
		fail(c, http.StatusInternalServerError, "更新任务状态失败")
		return
	}

	msg := services.AnalyzeMessage{
		TaskID:    task.ID,
		UserID:    task.UserID,
		VideoMD5:  task.VideoMD5,
		ObjectKey: task.ObjectKey,
	}
	if err := services.PublishAnalyze(ctx, msg); err != nil {
		// 投递失败要把状态退回去，否则任务会永远卡在排队中，
		// 而且用户重试时会因为「已在处理中」被直接返回。
		if rbErr := services.Transition(task.ID, models.TaskQueued, from, ""); rbErr != nil {
			log.Printf("回滚任务状态失败 task=%d: %v", task.ID, rbErr)
		}
		fail(c, http.StatusInternalServerError, "提交分析失败，请重试")
		return
	}

	ok(c, gin.H{
		"task_id": task.ID,
		"status":  models.TaskQueued,
		"message": "已提交，正在排队",
	})
}
