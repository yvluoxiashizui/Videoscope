package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"videoscope/services"
)

// ContextUserID 是存放当前登录用户 ID 的上下文键。
// 集中定义，避免各处手写字符串导致拼写不一致。
const ContextUserID = "user_id"

// Auth 鉴权中间件。
// 校验 Authorization 头中的 token，通过后把用户 ID 放进上下文，
// 后续 handler 用 CurrentUserID 取出。
func Auth(c *gin.Context) {
	raw := c.GetHeader("Authorization")
	if raw == "" {
		fail(c, http.StatusUnauthorized, "未登录")
		c.Abort()
		return
	}

	// 同时兼容 "Bearer xxx" 与直接传 token 两种写法。
	// 判断前缀时忽略大小写：RFC 7235 规定认证方案名不区分大小写，
	// 部分客户端会用 "bearer"，用 TrimPrefix 精确匹配会导致取不到 token。
	token := strings.TrimSpace(raw)
	if parts := strings.Fields(raw); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		token = parts[1]
	}

	userID, err := services.Parse(token)
	if err != nil {
		fail(c, http.StatusUnauthorized, err.Error())
		c.Abort()
		return
	}

	c.Set(ContextUserID, userID)
	c.Next()
}

// CurrentUserID 取出当前登录用户 ID。
// 只能在 Auth 之后的 handler 中调用，此时上下文里一定存在该值。
func CurrentUserID(c *gin.Context) uint {
	v, _ := c.Get(ContextUserID)
	id, _ := v.(uint)
	return id
}
