package handlers

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"

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
