package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Chapter 是一个章节切分。
type Chapter struct {
	Start   float64 `json:"start"`
	Title   string  `json:"title"`
	Summary string  `json:"summary"`
}

// AnalysisResult 是模型输出的结构化分析结果。
//
// 字段形状由提示词约定，解析后直接落库。
// 用固定结构而不是让模型自由发挥，是为了让下游可以稳定消费——
// 自由文本在展示和检索时都要再解析一遍，等于把问题往后推。
type AnalysisResult struct {
	Summary   string    `json:"summary"`
	KeyPoints []string  `json:"key_points"`
	Chapters  []Chapter `json:"chapters"`
}

// maxContextChars 限制送进模型的上下文字符数。
//
// 超长输入有两重代价：Token 费用线性增长，而且模型在过长的输入里
// 更容易丢失中段信息。这里做的是粗暴截断，后续用检索来精确挑选，
// 但截断本身必须有，否则两小时的视频会直接把额度打穿。
const maxContextChars = 20000

// systemPrompt 是系统提示词。
//
// 三段结构：角色、约束、输出格式。
//
// 第二条约束是实测之后加的。最初只写了「严禁推测」，结果模型把转写里的
// 同音字错误原样抄进了总结——「distributed systems」被识别成「dysdiated systems」，
// 总结里也写「dysdiated systems」，读者完全看不懂。
// 模型不是猜不出来，是约束太严，它不敢基于上下文做任何修正。
//
// 现在的写法把「修正识别错误」和「补充材料外信息」区分开：
// 前者允许，后者禁止。这样模型能在明显笔误上做合理还原，
// 又不会借机编造内容。
const systemPrompt = `你是资深课程助教，负责把长视频的内容提炼成结构化笔记。

工作约束：
一、只依据提供的材料作答，严禁引入材料之外的知识。
二、材料来自语音识别，可能存在同音字或断句错误。如果某个词在上下文中明显是识别错误，可以按上下文修正为合理的词；但不得补充材料中没有的内容。
三、材料不足以得出结论时，在对应字段写「材料未涉及」。

输出格式（必须是合法 JSON 对象，不要加任何解释文字）：
{
  "summary": "整体总结，两百字以内",
  "key_points": ["要点一", "要点二"],
  "chapters": [{"start": 0, "title": "章节标题", "summary": "章节摘要"}]
}`

// BuildPrompt 把多模态上下文组装成对话消息。
func BuildPrompt(vctx *VideoContext) []ChatMessage {
	return []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: renderContext(vctx)},
	}
}

// renderContext 把上下文渲染成带时间标记的文本。
//
// 保留时间标记而不是只给一段连续文字：模型据此判断内容的先后顺序，
// 生成章节切分时才有依据。
func renderContext(vctx *VideoContext) string {
	var b strings.Builder

	fmt.Fprintf(&b, "视频总时长：%.0f 秒。\n\n", vctx.Duration)

	for _, s := range vctx.Segments {
		fmt.Fprintf(&b, "[%.0f-%.0f 秒]\n", s.Start, s.End)
		if s.Speech != "" {
			fmt.Fprintf(&b, "语音：%s\n", s.Speech)
		}
		if s.Visual != "" {
			fmt.Fprintf(&b, "画面：%s\n", s.Visual)
		}
		b.WriteString("\n")
	}

	out := b.String()
	if len([]rune(out)) <= maxContextChars {
		return out
	}

	// 超长时按字符截断，并显式告知模型这是截断后的内容。
	// 不告知的话，模型会以为视频到此结束，总结里可能出现「内容不完整」的判断。
	truncated := string([]rune(out)[:maxContextChars])
	return truncated + "\n（材料过长，后续内容已截断，请在总结中说明本次分析只覆盖前半部分。）"
}

// Analyze 调用模型生成结构化分析结果。
func Analyze(ctx context.Context, vctx *VideoContext) (*AnalysisResult, error) {
	raw, err := LLM().Chat(ctx, BuildPrompt(vctx))
	if err != nil {
		return nil, fmt.Errorf("调用模型: %w", err)
	}

	result, err := ParseAnalysis(raw)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ParseAnalysis 解析模型返回的 JSON。
//
// 即使接口层已经要求了 JSON 输出，模型仍可能加上 Markdown 代码块包裹，
// 所以解析前先把围栏剥掉。这是实践中最高频的格式问题。
func ParseAnalysis(raw string) (*AnalysisResult, error) {
	cleaned := stripCodeFence(raw)

	var result AnalysisResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("解析分析结果: %w（原始内容前 200 字：%s）", err, head(cleaned, 200))
	}

	if strings.TrimSpace(result.Summary) == "" {
		return nil, fmt.Errorf("分析结果缺少 summary")
	}
	return &result, nil
}

// stripCodeFence 去掉 Markdown 代码块围栏。
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}

	// 去掉起始行（可能是 ```json 或 ```）。
	if idx := strings.Index(s, "\n"); idx >= 0 {
		s = s[idx+1:]
	} else {
		return s
	}

	// 去掉结尾的 ```。
	if idx := strings.LastIndex(s, "```"); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

// head 截取字符串开头部分用于错误信息。
// 按 rune 截断，避免把多字节字符切成半个。
func head(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "..."
}
