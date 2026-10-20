package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"videoscope/config"
	"videoscope/handlers"
	"videoscope/models"
	"videoscope/repos"
	"videoscope/services"
)

func main() {
	db, err := gorm.Open(mysql.Open(config.DSN()), &gorm.Config{})
	if err != nil {
		log.Fatal("连接数据库失败: ", err)
	}

	// 阶段一只建 user 表，其余表随对应阶段加入。
	if err := db.AutoMigrate(&models.User{}); err != nil {
		log.Fatal("建表失败: ", err)
	}
	repos.SetDB(db)

	rdb := redis.NewClient(&redis.Options{Addr: config.RedisAddr()})
	services.SetRedis(rdb)

	if err := services.InitStorage(
		config.S3Endpoint(),
		config.S3AccessKey(),
		config.S3SecretKey(),
		config.S3Bucket(),
		config.S3UseSSL(),
	); err != nil {
		log.Fatal("初始化对象存储失败: ", err)
	}

	r := gin.Default()

	// 单次请求体上限。分片是 multipart 上传，内存阈值设成单片上限的两倍，
	// 保证正常分片直接在内存里解析，不落到系统临时目录多做一次磁盘往返。
	r.MaxMultipartMemory = 64 << 20

	// 公开接口
	r.POST("/api/register", handlers.Register)
	r.POST("/api/login", handlers.Login)
	r.GET("/api/user", handlers.GetUser)
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 需要登录的接口
	auth := r.Group("/api", handlers.Auth)
	{
		auth.GET("/me", func(c *gin.Context) {
			u, err := services.GetUser(handlers.CurrentUserID(c))
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "用户不存在"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": u})
		})

		auth.POST("/upload/chunk", handlers.UploadChunk)
		auth.GET("/upload/status", handlers.UploadStatus)
		auth.POST("/upload/merge", handlers.MergeChunks)
	}

	if err := r.Run(config.ListenAddr()); err != nil {
		log.Fatal("启动失败: ", err)
	}
}
