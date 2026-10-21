package main

import (
	"context"
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

	// 表随对应阶段加入，AutoMigrate 只做增量的加表加列，不会删字段。
	if err := db.AutoMigrate(
		&models.User{},
		&models.VideoTask{},
		&models.VideoTranscription{},
		&models.AISummary{},
	); err != nil {
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

	if err := services.InitMQ(config.MQURL()); err != nil {
		log.Fatal("初始化消息队列失败: ", err)
	}
	defer services.CloseMQ()

	// 分片与成品的过期清理交给存储层执行。
	// 配置失败不阻止启动：清理规则是长期维护动作，没有它系统照样能用。
	if err := services.EnsureLifecycle(context.Background()); err != nil {
		log.Printf("配置对象存储生命周期失败: %v", err)
	}

	// 转写器：配了地址和密钥就走真实服务，否则退化为模拟实现。
	// 这样没有外部账号也能把整条处理链路跑通。
	services.SetTranscriber(services.NewTranscriber(
		config.ASRBaseURL(), config.ASRAPIKey(), config.ASRModel()))

	// 画面文字识别：本机装了 tesseract 就用它，没有则退化为模拟实现。
	services.SetOCRExtractor(services.NewOCRExtractor())

	// 大模型客户端：配了地址和密钥就走真实服务，否则退化为模拟实现。
	services.SetLLMClient(services.NewLLMClient(
		config.LLMBaseURL(), config.LLMAPIKey(), config.LLMModel()))

	// 启动消费者。处理逻辑是完整的视频处理流水线。
	if err := services.StartConsumer(context.Background(), services.ProcessVideo); err != nil {
		log.Fatal("启动消费者失败: ", err)
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
		auth.POST("/analyze", handlers.SubmitAnalyze)
		auth.GET("/task/:id/download", handlers.TaskDownloadURL)
	}

	if err := r.Run(config.ListenAddr()); err != nil {
		log.Fatal("启动失败: ", err)
	}
}
