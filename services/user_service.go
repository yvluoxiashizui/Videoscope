package services

import (
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"videoscope/models"
	"videoscope/repos"
)

// Register 注册新用户。
func Register(username, password string) (*models.User, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, errors.New("用户名和密码不能为空")
	}
	if len(username) > 32 {
		return nil, errors.New("用户名不能超过 32 个字符")
	}
	if !isAlnum(username) {
		return nil, errors.New("用户名只能包含字母和数字")
	}

	// bcrypt 只处理前 72 字节，超长输入会直接报错，这里提前拦掉给出明确提示。
	if len(password) > 72 {
		return nil, errors.New("密码不能超过 72 字节")
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.New("密码加密失败")
	}

	u := &models.User{
		Username:    username,
		Password:    string(hashed),
		AccountType: models.AccountFree,
	}
	if err := repos.CreateUser(u); err != nil {
		// 唯一索引冲突是最常见的失败原因，其余错误也归到这里，
		// 避免把数据库错误原样返回给客户端。
		return nil, errors.New("用户名已存在")
	}
	return u, nil
}

// Login 校验凭证并签发 token。
func Login(username, password string) (string, *models.User, error) {
	u, err := repos.FindUserByName(strings.TrimSpace(username))
	if err != nil {
		// 用户不存在与密码错误返回同一句提示，避免账号枚举。
		return "", nil, errors.New("用户名或密码错误")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password)); err != nil {
		return "", nil, errors.New("用户名或密码错误")
	}

	token, err := Sign(u.ID)
	if err != nil {
		return "", nil, errors.New("签发 token 失败")
	}
	return token, u, nil
}

// GetUser 按 ID 查询用户。
func GetUser(id uint) (*models.User, error) {
	return repos.FindUserByID(id)
}

func isAlnum(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}
