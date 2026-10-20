package services

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// devSecret 本地开发兜底密钥，生产环境必须通过环境变量注入。
const devSecret = "videoscope-dev-only-secret"

// secret 只解析一次。
// 密钥在进程生命周期内固定，而 Parse 会被每个鉴权请求调用，
// 每次都读环境变量属于无意义的重复开销。
var (
	secretOnce sync.Once
	secret     []byte
)

// Secret 返回 JWT 签名密钥。
func Secret() []byte {
	secretOnce.Do(func() {
		if s := os.Getenv("VIDEOSCOPE_JWT_SECRET"); s != "" {
			secret = []byte(s)
			return
		}
		log.Println("[warn] VIDEOSCOPE_JWT_SECRET 未设置，正在使用本地开发密钥")
		secret = []byte(devSecret)
	})
	return secret
}

// Sign 为指定用户签发 token，有效期 72 小时。
func Sign(userID uint) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(72 * time.Hour).Unix(),
	})
	return token.SignedString(Secret())
}

// Parse 校验 token 并取出用户 ID。
//
// 这里的两个显式判断是必要的：
// 一是签名算法只接受 HMAC 系列，避免算法混淆；
// 二是 claims 里的 user_id 必须存在且为数字，否则直接返回错误。
// 缺少第二个判断时，一个签名合法但不含 user_id 的 token 会让类型断言 panic。
func Parse(tokenString string) (uint, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return Secret(), nil
	})
	if err != nil || !token.Valid {
		return 0, errors.New("token 无效")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, errors.New("token 载荷格式错误")
	}

	// JSON 数字统一解成 float64，这里必须用带 ok 的断言。
	v, ok := claims["user_id"].(float64)
	if !ok || v <= 0 {
		return 0, errors.New("token 缺少用户标识")
	}
	return uint(v), nil
}
