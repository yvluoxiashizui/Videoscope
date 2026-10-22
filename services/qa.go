package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"videoscope/models"
)

// 问答提示词。
//
// 与总结提示词的差别在「知识边界」：总结时材料是整段上下文，
// 模型可以自由组织；回答问题时材料只是被检索出来的几个片段，
// 片段之外的内容它并不知道，所以必须明确要求「找不到就说找不到」，
// 否则模型会用通用知识补一个看起来合理、实际与视频无关的答案。
const qaSystemPrompt = `你是视频内容助手，根据提供的片段回答用户问题。

工作约束：
一、只依据提供的片段作答，片段里没有的信息不要编造。
二、片段不足以回答时，明确说明「视频中没有提到相关内容」。
三、材料来自语音识别，可能存在同音字错误，可按上下文合理修正。

输出格式（必须是合法 JSON 对象，不要加任何解释文字）：
{
  "answer": "回答内容",
  "found": true
}
其中 found 表示片段里是否包含回答问题所需的信息。`

// Answer 是一次追问的结果。
type Answer struct {
	Answer string `json:"answer"`
	// Found 表示检索到的片段是否足以回答问题。
	// 检索总会返回 topK 个块，即使相关度很低——
	// 区分「答得出」和「答不出」的责任落在模型身上。
	Found bool `json:"found"`
	// Sources 是答案依据的片段，带时间范围，
	// 前端可以据此跳转到视频的对应位置。
	Sources []Source `json:"sources"`
}

// Source 是一段被引用的内容。
type Source struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Content string  `json:"content"`
}

// AskVideo 基于检索结果回答关于某个视频的问题。
func AskVideo(ctx context.Context, taskID uint, question string) (*Answer, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("问题不能为空")
	}

	hits, err := Retrieve(ctx, taskID, question, TopK)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return &Answer{
			Answer: "该视频还没有可检索的内容。",
			Found:  false,
		}, nil
	}

	// 把召回的片段拼成材料，带时间标记。
	// 时间标记能让模型在答案里引用「第几分钟讲了什么」，
	// 也方便前端把答案定位回视频。
	var material strings.Builder
	sources := make([]Source, 0, len(hits))
	for i, h := range hits {
		fmt.Fprintf(&material, "[片段%d，%.0f-%.0f秒]\n%s\n\n",
			i+1, h.Chunk.Start, h.Chunk.End, h.Chunk.Content)
		sources = append(sources, Source{
			Start:   h.Chunk.Start,
			End:     h.Chunk.End,
			Content: h.Chunk.Content,
		})
	}

	messages := []ChatMessage{
		{Role: "system", Content: qaSystemPrompt},
		{Role: "user", Content: fmt.Sprintf("片段：\n%s\n问题：%s", material.String(), question)},
	}

	raw, err := LLM().Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("调用模型回答: %w", err)
	}

	var out struct {
		Answer string `json:"answer"`
		Found  bool   `json:"found"`
	}
	if err := json.Unmarshal([]byte(stripCodeFence(raw)), &out); err != nil {
		return nil, fmt.Errorf("解析回答结果: %w（原始内容：%s）", err, head(raw, 200))
	}

	if strings.TrimSpace(out.Answer) == "" {
		return nil, fmt.Errorf("模型返回空答案")
	}

	return &Answer{
		Answer:  out.Answer,
		Found:   out.Found,
		Sources: sources,
	}, nil
}

// EnsureAnswerable 检查任务是否具备追问条件。
//
// 只有处理完成的任务才有转写内容与索引。其他状态直接拒绝，
// 避免用户在任务还在跑的时候提问，拿到一个「内容为空」的误导性结果。
func EnsureAnswerable(task *models.VideoTask) error {
	if task.Status != models.TaskDone {
		return fmt.Errorf("任务尚未处理完成，当前状态：%s", models.StatusText(task.Status))
	}
	return nil
}
