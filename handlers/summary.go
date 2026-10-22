package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"videoscope/services"
)

// TaskSummary 返回某个任务的结构化分析结果。
//
// 与追问接口分开：追问是一次新的模型调用，有延迟也有成本；
// 而总结在任务完成时就已经生成好了，这里只是读取。
func TaskSummary(c *gin.Context) {
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

	result, err := services.LoadAnalysis(taskID)
	if err != nil {
		// 任务存在但没有结果，说明还没处理完或者分析失败。
		fail(c, http.StatusNotFound, "该任务还没有分析结果")
		return
	}

	ok(c, result)
}
