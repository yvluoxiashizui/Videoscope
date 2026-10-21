package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"videoscope/services"
)

// downloadURLTTL 是预签名地址的有效期。
// 足够用户点开一段视频，又不至于长到链接被转发后仍长期可用。
const downloadURLTTL = 15 * time.Minute

// TaskDownloadURL 为任务对应的视频生成临时下载地址。
//
// 这个接口同时承担鉴权与授权：先确认登录，再确认任务属于当前用户。
// 两者缺一不可——只校验登录的话，任何人猜到任务 ID 就能下载别人的视频。
func TaskDownloadURL(c *gin.Context) {
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
		// 403 而不是 404：目标存在但无权访问，语义上更准确。
		// 如果连「存在与否」都不想暴露，也可以统一返回 404。
		fail(c, http.StatusForbidden, "无权访问该视频")
		return
	}

	url, err := services.PresignedGetURL(c.Request.Context(), task.ObjectKey, downloadURLTTL)
	if err != nil {
		fail(c, http.StatusInternalServerError, "生成下载地址失败")
		return
	}

	ok(c, gin.H{
		"url":        url,
		"expires_in": int(downloadURLTTL.Seconds()),
	})
}
