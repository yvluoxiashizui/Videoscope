package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ChatMessage 是一条对话消息。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LLMClient 调用大模型完成一次对话。
//
// 抽成接口的原因和转写器一样：本地开发用模拟实现跑通链路，
// 上线换成真实服务时上层编排不用改。
type LLMClient interface {
	Chat(ctx context.Context, messages []ChatMessage) (string, error)
}

// NewLLMClient 根据配置返回可用的实现。
func NewLLMClient(baseURL, apiKey, model string) LLMClient {
	if baseURL == "" || apiKey == "" {
		return mockLLM{}
	}
	return &httpLLM{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 2 * time.Minute},
	}
}

// httpLLM 调用兼容 OpenAI 的对话接口。
type httpLLM struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func (l *httpLLM) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	payload := map[string]interface{}{
		"model":       l.model,
		"messages":    messages,
		"temperature": 0.2,
		// 强制模型输出 JSON 对象。
		// 提示词里已经要求了格式，这里再加一层接口级的约束，
		// 因为模型偶尔会忘记格式要求，在前面加一句「好的，以下是总结」，
		// 后面的解析就直接失败了。
		"response_format": map[string]string{"type": "json_object"},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("序列化请求: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		l.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("构造请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+l.apiKey)

	resp, err := l.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用模型接口: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("模型接口返回 %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("解析模型响应: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("模型返回空结果")
	}

	return out.Choices[0].Message.Content, nil
}

// mockLLM 是本地开发用的占位实现。
//
// 返回一段符合约定格式的 JSON，让下游的解析与落库逻辑能跑出真实形状。
// 内容里带「模拟」前缀，一眼能看出不是真实分析结果。
type mockLLM struct{}

func (mockLLM) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	// 从最后一条用户消息里粗略取一段，让输出看起来跟输入有关联。
	inputLen := 0
	if len(messages) > 0 {
		inputLen = len([]rune(messages[len(messages)-1].Content))
	}

	out := map[string]interface{}{
		"summary": fmt.Sprintf("[模拟分析] 基于约 %d 字的上下文生成的整体摘要。", inputLen),
		"key_points": []string{
			"[模拟要点] 视频开篇介绍了主题",
			"[模拟要点] 中间部分展开讲解",
			"[模拟要点] 结尾做了总结",
		},
		"chapters": []map[string]interface{}{
			{"start": 0, "title": "[模拟章节] 开头", "summary": "引入部分"},
			{"start": 60, "title": "[模拟章节] 主体", "summary": "核心讲解"},
		},
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
