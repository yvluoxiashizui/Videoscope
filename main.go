package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"videoscope/config"
	"videoscope/models"
	"videoscope/repos"
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

	r := gin.Default()
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	if err := r.Run(config.ListenAddr()); err != nil {
		log.Fatal("启动失败: ", err)
	}
}
