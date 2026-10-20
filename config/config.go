package config

import (
	"fmt"
	"os"
)

// Getenv 读取环境变量，未设置时返回兜底值。
// 所有配置都走环境变量，本地开发用兜底值即可跑起来，部署时通过环境变量覆盖。
func Getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// DSN 返回 MySQL 连接串。
// parseTime=True 让 time.Time 字段能正确解析，charset 固定 utf8mb4 以支持中文与 emoji。
func DSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		Getenv("VIDEOSCOPE_DB_USER", "goapp"),
		Getenv("VIDEOSCOPE_DB_PASSWORD", "goapp123"),
		Getenv("VIDEOSCOPE_DB_ADDR", "127.0.0.1:3306"),
		Getenv("VIDEOSCOPE_DB_NAME", "videoscope"),
	)
}

// RedisAddr 返回 Redis 地址，阶段四开始使用。
func RedisAddr() string {
	return Getenv("VIDEOSCOPE_REDIS_ADDR", "127.0.0.1:6379")
}

// ListenAddr 返回服务监听地址。
func ListenAddr() string {
	return Getenv("VIDEOSCOPE_LISTEN", ":8080")
}
