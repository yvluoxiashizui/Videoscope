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

// TranscriptSegment 是一段带时间戳的转写结果。
//
// 时间戳是后面做多模态对齐的基础：只有知道「这句话出现在第几秒」，
// 才能把它和同一时刻的画面文字对应起来。
// 只返回一整段纯文本的话，语音和画面就只能是两条互不相干的线。
type TranscriptSegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// Transcriber 把一段音频转成带时间戳的文字。
//
// offset 是这段音频在完整音轨中的起始秒数。
// 分段转写时每一段都从 0 开始计时，必须加上偏移量才能还原到全局时间轴，
// 否则所有分段的文字都会挤在开头。
type Transcriber interface {
	Transcribe(ctx context.Context, audioPath string, offset float64) ([]TranscriptSegment, error)
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
// 选这个格式而不是绑死某家厂商的 SDK：只要协议一致，
// 换供应商时改的是 base URL 和模型名，不是代码。
type httpTranscriber struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func (t *httpTranscriber) Transcribe(ctx context.Context, audioPath string, offset float64) ([]TranscriptSegment, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return nil, fmt.Errorf("打开音频文件: %w", err)
	}
	defer f.Close()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("model", t.model); err != nil {
		return nil, fmt.Errorf("构造请求体: %w", err)
	}
	part, err := w.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return nil, fmt.Errorf("构造文件字段: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, fmt.Errorf("写入音频数据: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("结束请求体: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.baseURL+"/v1/audio/transcriptions", &body)
	if err != nil {
		return nil, fmt.Errorf("构造请求: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+t.apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用转写接口: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("转写接口返回 %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}

	var out struct {
		Text     string `json:"text"`
		Segments []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		} `json:"segments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("解析转写结果: %w", err)
	}

	// 有些实现会返回 segments，有些只返回整段 text。
	// 不额外请求 verbose_json 是因为部分平台不接受这个参数，直接报 400；
	// 而且当前方案用分段边界做时间轴，本就不依赖返回值里的时间戳。
	if len(out.Segments) == 0 {
		if out.Text == "" {
			return nil, nil
		}
		return []TranscriptSegment{{Start: offset, End: offset, Text: out.Text}}, nil
	}

	segs := make([]TranscriptSegment, 0, len(out.Segments))
	for _, s := range out.Segments {
		segs = append(segs, TranscriptSegment{
			Start: s.Start + offset,
			End:   s.End + offset,
			Text:  s.Text,
		})
	}
	return segs, nil
}

// mockTranscriber 是本地开发用的占位实现。
//
// 不做真实识别，但会按音频时长切出若干带时间戳的假条目，
// 这样依赖时间戳的对齐逻辑在本地也能跑出真实形状。
type mockTranscriber struct{}

const (
	// wavBytesPerSecond 是 16kHz / 16bit / 单声道 PCM 每秒的字节数。
	wavBytesPerSecond = 16000 * 2
	// mockSegmentSeconds 是假条目的粒度，与真实 ASR 常见的分句长度接近。
	mockSegmentSeconds = 15.0
)

func (mockTranscriber) Transcribe(ctx context.Context, audioPath string, offset float64) ([]TranscriptSegment, error) {
	info, err := os.Stat(audioPath)
	if err != nil {
		return nil, fmt.Errorf("读取音频文件: %w", err)
	}

	// 扣除 44 字节 WAV 头，剩下按每秒字节数换算时长。
	duration := float64(info.Size()-44) / wavBytesPerSecond
	if duration <= 0 {
		return nil, nil
	}

	segs := make([]TranscriptSegment, 0, int(duration/mockSegmentSeconds)+1)
	for start := 0.0; start < duration; start += mockSegmentSeconds {
		end := start + mockSegmentSeconds
		if end > duration {
			end = duration
		}
		// 跳过极短的尾条。
		// 音频时长带小数（30 秒的段实际是 30.001 秒），循环会多跑一次，
		// 产出一个毫秒级的条目。这种条目送进模型只是噪声。
		if end-start < 0.5 {
			continue
		}
		segs = append(segs, TranscriptSegment{
			Start: start + offset,
			End:   end + offset,
			Text:  fmt.Sprintf("[模拟转写 %.0f-%.0f秒]", start+offset, end+offset),
		})
	}
	return segs, nil
}
