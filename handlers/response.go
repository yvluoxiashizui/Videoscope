package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response 统一响应结构。
// 用 code 字段表达业务结果，而不是让前端去分辨各处的 HTTP 状态码；
// data 为空时省略，避免响应里出现一堆 null。
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func ok(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{Code: 0, Message: "ok", Data: data})
}

func fail(c *gin.Context, status int, msg string) {
	c.JSON(status, Response{Code: status, Message: msg})
}
