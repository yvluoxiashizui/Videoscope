package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"videoscope/services"
)

type credentialsReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Register 注册。
func Register(c *gin.Context) {
	var req credentialsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}

	u, err := services.Register(req.Username, req.Password)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, gin.H{"id": u.ID, "username": u.Username})
}

// Login 登录，返回 token。
func Login(c *gin.Context) {
	var req credentialsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}

	token, u, err := services.Login(req.Username, req.Password)
	if err != nil {
		fail(c, http.StatusUnauthorized, err.Error())
		return
	}
	ok(c, gin.H{
		"token":        token,
		"user_id":      u.ID,
		"username":     u.Username,
		"account_type": u.AccountType,
	})
}

// GetUser 按 ID 查询用户。
func GetUser(c *gin.Context) {
	id, err := parseUint(c.Query("id"))
	if err != nil {
		fail(c, http.StatusBadRequest, "id 参数错误")
		return
	}

	u, err := services.GetUser(id)
	if err != nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	ok(c, u)
}
