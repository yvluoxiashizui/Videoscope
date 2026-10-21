package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Transcriber 把一段音频转成文字。
//
// 抽成接口是为了让转写服务可替换：本地开发可以用模拟实现跑通链路，
// 上线时换成真正的语音识别服务，上层编排逻辑不用改。
type Transcriber interface {
	Transcribe(ctx context.Context, audioPath string) (string, error)
}

// NewTranscriber 根据配置返回可用的实现。
// 没配密钥时返回模拟实现，保证没有外部依赖也能把整条链路跑起来。
func NewTranscriber(baseURL, apiKey, model string) Transcriber {
	if baseURL == "" || apiKey == "" {
		return mockTranscriber{}
	}
	return &httpTranscriber{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 3 * time.Minute},
	}
}

// httpTranscriber 调用兼容 OpenAI 格式的语音转写接口。
//
// 选这个格式而不是绑死某家厂商的 SDK：只要接口协议一致，
// 换供应商时改的是 base URL 和模型名，不是代码。
type httpTranscriber struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func (t *httpTranscriber) Transcribe(ctx context.Context, audioPath string) (string, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return "", fmt.Errorf("打开音频文件: %w", err)
	}
	defer f.Close()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("model", t.model); err != nil {
		return "", fmt.Errorf("构造请求体: %w", err)
	}
	part, err := w.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", fmt.Errorf("构造文件字段: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", fmt.Errorf("写入音频数据: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("结束请求体: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.baseURL+"/v1/audio/transcriptions", &body)
	if err != nil {
		return "", fmt.Errorf("构造请求: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+t.apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用转写接口: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// 把响应体前一段带出来，定位问题时不用再复现一次。
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("转写接口返回 %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("解析转写结果: %w", err)
	}
	return out.Text, nil
}

// mockTranscriber 是本地开发用的占位实现。
//
// 不做任何真实识别，只返回一段带前缀的说明文本。
// 前缀是故意的：日志和数据库里一眼能看出这是模拟数据，
// 不会和真实转写结果混淆。
type mockTranscriber struct{}

func (mockTranscriber) Transcribe(ctx context.Context, audioPath string) (string, error) {
	info, err := os.Stat(audioPath)
	if err != nil {
		return "", fmt.Errorf("读取音频文件: %w", err)
	}
	return fmt.Sprintf("[模拟转写] %s，音频大小 %d 字节",
		filepath.Base(audioPath), info.Size()), nil
}
