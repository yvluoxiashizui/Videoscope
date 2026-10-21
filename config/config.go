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

// S3Endpoint 返回对象存储服务地址，不带协议前缀。
func S3Endpoint() string {
	return Getenv("VIDEOSCOPE_S3_ENDPOINT", "127.0.0.1:9000")
}

// S3AccessKey 与 S3SecretKey 是对象存储的访问凭证。
func S3AccessKey() string {
	return Getenv("VIDEOSCOPE_S3_ACCESS_KEY", "minioadmin")
}

func S3SecretKey() string {
	return Getenv("VIDEOSCOPE_S3_SECRET_KEY", "minioadmin")
}

// S3Bucket 是存放分片与成品的桶。
func S3Bucket() string {
	return Getenv("VIDEOSCOPE_S3_BUCKET", "videos")
}

// S3UseSSL 决定是否用 HTTPS 连接对象存储，本地开发关闭。
func S3UseSSL() bool {
	return Getenv("VIDEOSCOPE_S3_USE_SSL", "false") == "true"
}

// MQURL 返回 RabbitMQ 连接串。
func MQURL() string {
	return Getenv("VIDEOSCOPE_MQ_URL", "amqp://guest:guest@127.0.0.1:5672/")
}

// ASR 相关配置。
// 未配置地址或密钥时，转写会退化为模拟实现，用于本地跑通链路。
func ASRBaseURL() string {
	return Getenv("VIDEOSCOPE_ASR_BASE_URL", "")
}

func ASRAPIKey() string {
	return Getenv("VIDEOSCOPE_ASR_API_KEY", "")
}

func ASRModel() string {
	return Getenv("VIDEOSCOPE_ASR_MODEL", "whisper-1")
}

// ListenAddr 返回服务监听地址。
func ListenAddr() string {
	return Getenv("VIDEOSCOPE_LISTEN", ":8080")
}
