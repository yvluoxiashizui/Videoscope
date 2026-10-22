package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// Embedder 把文本转成向量。
//
// 批量接口而不是单条：切块之后一次要处理几十个块，
// 逐条请求会把网络往返时间放大几十倍。
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// NewEmbedder 根据配置返回可用的实现。
func NewEmbedder(baseURL, apiKey, model string) Embedder {
	if baseURL == "" || apiKey == "" {
		return mockEmbedder{}
	}
	return &httpEmbedder{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// httpEmbedder 调用兼容 OpenAI 的向量接口。
type httpEmbedder struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func (e *httpEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	payload := map[string]interface{}{
		"model": e.model,
		"input": texts,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化向量请求: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造向量请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用向量接口: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("向量接口返回 %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}

	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("解析向量结果: %w", err)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("向量数量不匹配：请求 %d 条，返回 %d 条", len(texts), len(out.Data))
	}

	// 按 index 归位。接口不保证返回顺序与请求顺序一致，
	// 直接按数组下标取会把向量错配到其他文本上。
	vectors := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vectors) {
			continue
		}
		vectors[d.Index] = d.Embedding
	}
	for i, v := range vectors {
		if len(v) == 0 {
			return nil, fmt.Errorf("第 %d 条文本没有返回向量", i)
		}
	}
	return vectors, nil
}

// mockEmbedder 是本地开发用的占位实现。
//
// 用文本的哈希值散布成一个固定维度的向量，相同文本得到相同向量，
// 不同文本得到不同向量。它不具备任何语义性质——近义文本不会更接近——
// 但足以验证「切块、向量化、存储、检索」这条链路的连接是否正确。
type mockEmbedder struct{}

const mockEmbedDim = 256

func (mockEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		vectors[i] = hashVector(text)
	}
	return vectors, nil
}

// hashVector 把文本哈希扩展成归一化向量。
func hashVector(text string) []float32 {
	seed := sha256.Sum256([]byte(text))

	vec := make([]float32, mockEmbedDim)
	var norm float64
	for i := range vec {
		// 用哈希字节循环填充，保证长度固定且与文本相关。
		b := seed[i%len(seed)]
		v := float64(b)/255.0 - 0.5
		vec[i] = float32(v)
		norm += v * v
	}

	// 归一化之后余弦相似度直接等于点积，检索时少算一步。
	if norm > 0 {
		f := float32(1 / math.Sqrt(norm))
		for i := range vec {
			vec[i] *= f
		}
	}
	return vec
}
