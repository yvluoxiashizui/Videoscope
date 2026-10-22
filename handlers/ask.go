package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"videoscope/services"
)

// 单次提问的长度上限。
// 问题不该比被问的内容还长，超过这个长度基本是误用而非真实提问。
const maxQuestionLen = 500

// AskVideo 基于视频内容回答问题。
//
// 请求体：{"question": "..."}
//
// 返回值除了答案本身，还带上依据的片段及其时间范围——
// 用户看到「第几分钟讲过这件事」，比只给一段文字可信得多。
func AskVideo(c *gin.Context) {
	taskID, err := parseUint(c.Param("id"))
	if err != nil {
		fail(c, http.StatusBadRequest, "任务 ID 错误")
		return
	}

	var req struct {
		Question string `json:"question" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}

	question := strings.TrimSpace(req.Question)
	if question == "" {
		fail(c, http.StatusBadRequest, "问题不能为空")
		return
	}
	if len([]rune(question)) > maxQuestionLen {
		fail(c, http.StatusBadRequest, "问题过长")
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
	if err := services.EnsureAnswerable(task); err != nil {
		fail(c, http.StatusConflict, err.Error())
		return
	}

	answer, err := services.AskVideo(c.Request.Context(), taskID, question)
	if err != nil {
		fail(c, http.StatusInternalServerError, "回答失败，请稍后重试")
		return
	}

	ok(c, answer)
}
