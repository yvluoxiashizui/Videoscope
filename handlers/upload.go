package handlers

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"videoscope/services"
)

// maxChunkSize 是单个分片的大小上限。
// 前端按 1MB / 5MB / 20MB 三档动态调整，取 32MB 留出余量。
const maxChunkSize = 32 << 20

// UploadChunk 接收单个分片。
//
// 请求为 multipart/form-data，字段：md5（整个文件）、index（分片序号）、
// chunk_md5（可选，当前分片的摘要）、file（分片二进制）。
//
// 这里会重新计算一次分片的 MD5 并与前端传值比对。
// 原因是分片经过公网传输后可能发生损坏（比特翻转），
// 而对象存储不会校验内容，坏分片一旦落盘，
// 最终合并出来的视频是损坏的，且错误要到播放时才会暴露。
func UploadChunk(c *gin.Context) {
	fileMD5 := strings.ToLower(strings.TrimSpace(c.PostForm("md5")))
	if !isMD5(fileMD5) {
		fail(c, http.StatusBadRequest, "md5 格式错误")
		return
	}

	index, err := parseInt(c.PostForm("index"))
	if err != nil || index < 0 {
		fail(c, http.StatusBadRequest, "index 参数错误")
		return
	}

	header, err := c.FormFile("file")
	if err != nil {
		fail(c, http.StatusBadRequest, "缺少分片文件")
		return
	}
	if header.Size > maxChunkSize {
		fail(c, http.StatusRequestEntityTooLarge, "分片超过大小上限")
		return
	}

	src, err := header.Open()
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取分片失败")
		return
	}
	defer src.Close()

	data, err := io.ReadAll(io.LimitReader(src, maxChunkSize+1))
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取分片失败")
		return
	}
	if len(data) > maxChunkSize {
		fail(c, http.StatusRequestEntityTooLarge, "分片超过大小上限")
		return
	}

	if want := strings.ToLower(strings.TrimSpace(c.PostForm("chunk_md5"))); want != "" {
		sum := md5.Sum(data)
		if hex.EncodeToString(sum[:]) != want {
			// 返回 400 而不是 500：这是客户端数据问题，前端拿到后应重传该分片。
			fail(c, http.StatusBadRequest, "分片校验失败，请重传")
			return
		}
	}

	ctx := c.Request.Context()
	key := services.ChunkKey(fileMD5, index)

	// 先落盘，后记账。顺序反了会产生「已记录但对象不存在」的假上传。
	if err := services.PutChunk(ctx, key, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
		fail(c, http.StatusInternalServerError, "写入分片失败")
		return
	}
	if err := services.MarkChunkUploaded(ctx, fileMD5, index); err != nil {
		fail(c, http.StatusInternalServerError, "记录分片状态失败")
		return
	}

	ok(c, gin.H{"index": index, "size": len(data)})
}

// UploadStatus 返回某个文件已上传的分片序号，前端据此续传。
//
// 传入 total 时额外返回 can_merge：
// 分片齐全说明可以直接合并，前端不需要再补传任何数据。
func UploadStatus(c *gin.Context) {
	fileMD5 := strings.ToLower(strings.TrimSpace(c.Query("md5")))
	if !isMD5(fileMD5) {
		fail(c, http.StatusBadRequest, "md5 格式错误")
		return
	}

	uploaded, err := services.UploadedChunks(c.Request.Context(), fileMD5)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询上传状态失败")
		return
	}

	resp := gin.H{
		"md5":      fileMD5,
		"uploaded": uploaded,
		"count":    len(uploaded),
	}
	if total, err := parseInt(c.Query("total")); err == nil && total > 0 {
		resp["can_merge"] = len(uploaded) == total
	}
	ok(c, resp)
}

// MergeChunks 把已上传的分片合并成完整文件。
//
// 由前端在确认所有分片都返回成功后主动调用一次，而不是后端在收齐分片时自动触发。
// 后端自动触发的写法在并发下会翻车：最后几片几乎同时到达，
// 多个请求各自发现「分片数刚好够了」，于是同时发起合并，
// 对象存储侧会报状态机冲突。
//
// 由前端统一触发，合并请求天然只有一个；后端的合并锁再兜一层，
// 防止前端因超时重试而重复调用。
func MergeChunks(c *gin.Context) {
	var req struct {
		MD5   string `json:"md5" binding:"required"`
		Total int    `json:"total" binding:"required"`
		Ext   string `json:"ext"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}

	fileMD5 := strings.ToLower(strings.TrimSpace(req.MD5))
	if !isMD5(fileMD5) {
		fail(c, http.StatusBadRequest, "md5 格式错误")
		return
	}
	// 一万片是对象存储合并接口的上限。
	if req.Total <= 0 || req.Total > 10000 {
		fail(c, http.StatusBadRequest, "total 超出范围")
		return
	}

	ctx := c.Request.Context()

	// 先校验分片数量，避免为一个传了一半的任务去抢锁。
	count, err := services.UploadedCount(ctx, fileMD5)
	if err != nil {
		fail(c, http.StatusInternalServerError, "查询分片状态失败")
		return
	}
	if count != int64(req.Total) {
		fail(c, http.StatusBadRequest, "分片未传齐")
		return
	}

	locked, err := services.TryLockMerge(ctx, fileMD5, 30*time.Second)
	if err != nil {
		fail(c, http.StatusInternalServerError, "获取合并锁失败")
		return
	}
	if !locked {
		// 409 而不是 500：另一个请求正在合并，前端稍后重试即可。
		fail(c, http.StatusConflict, "该文件正在合并中")
		return
	}
	defer services.UnlockMerge(context.WithoutCancel(ctx), fileMD5)

	destKey := "videos/" + fileMD5 + normalizeExt(req.Ext)

	if err := services.ComposeChunks(ctx, fileMD5, req.Total, destKey); err != nil {
		fail(c, http.StatusInternalServerError, "合并分片失败")
		return
	}

	// 合并成功后清掉分片记录，避免 24 小时内重复上传被判为「已存在」。
	if err := services.ClearChunks(ctx, fileMD5); err != nil {
		// 清理失败不影响本次结果的正确性，记录即可。
		log.Printf("清除分片状态失败 md5=%s: %v", fileMD5, err)
	}

	ok(c, gin.H{"md5": fileMD5, "object": destKey})
}

// normalizeExt 规范化扩展名，只保留白名单内的值。
// 直接拼用户传入的扩展名会带来路径穿越与类型伪造问题。
func normalizeExt(ext string) string {
	switch strings.ToLower(strings.TrimSpace(strings.TrimPrefix(ext, "."))) {
	case "mp4":
		return ".mp4"
	case "mov":
		return ".mov"
	case "m4a":
		return ".m4a"
	case "mp3":
		return ".mp3"
	default:
		return ".mp4"
	}
}

// isMD5 校验是否为 32 位十六进制字符串。
func isMD5(s string) bool {
	if len(s) != 32 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// parseInt 解析十进制整数，非法输入返回错误而不是静默变成 0。
func parseInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("空值")
	}
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("非数字")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
