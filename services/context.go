package services

import (
	"math"
	"strings"
)

// 上下文切分参数。
//
// 窗口大小与抽帧间隔取同一个值：两者对齐之后，
// 每个窗口最多对应一张关键帧，语音和画面的归属关系是明确的，
// 不会出现「这张图到底算哪一段」的歧义。
const (
	KeyframeInterval = 30 // 每 30 秒抽一帧
	ContextWindow    = 30 // 上下文按 30 秒切分
)

// FrameText 是一张关键帧的识别结果，Time 是它在视频中的时间点（秒）。
type FrameText struct {
	Time float64
	Text string
}

// ContextSegment 是多模态上下文中的一个时间片。
//
// 同一个时间窗口内的语音和画面被放在一起，
// 下游模型看到的是「这一段时间里说了什么、画面上写了什么」，
// 而不是两份各自独立的文本。
type ContextSegment struct {
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	Speech string  `json:"speech,omitempty"`
	Visual string  `json:"visual,omitempty"`
}

// Empty 判断这个时间片是否没有任何内容。
func (s ContextSegment) Empty() bool {
	return strings.TrimSpace(s.Speech) == "" && strings.TrimSpace(s.Visual) == ""
}

// VideoContext 是融合后的多模态上下文。
type VideoContext struct {
	TaskID   uint             `json:"task_id"`
	Duration float64          `json:"duration"`
	Segments []ContextSegment `json:"segments"`
}

// BuildVideoContext 把带时间戳的转写与关键帧识别结果按时间轴对齐。
//
// 对齐粒度取 window 秒。转写与画面各自落在哪个窗口，由它们的时间戳决定——
// 这里不做插值也不做推测，时间戳落在哪个区间就归到哪一段。
//
// 空窗口会被跳过：既没有说话、画面也没有文字的时间段送进模型只是浪费 Token，
// 还可能让模型在无关内容上编出结论。
func BuildVideoContext(
	taskID uint,
	duration float64,
	transcripts []TranscriptSegment,
	visuals []FrameText,
	windowSeconds float64,
) *VideoContext {
	if windowSeconds <= 0 {
		windowSeconds = ContextWindow
	}

	ctx := &VideoContext{
		TaskID:   taskID,
		Duration: duration,
	}

	if duration <= 0 {
		return ctx
	}

	windows := int(math.Ceil(duration / windowSeconds))
	segments := make([]ContextSegment, 0, windows)

	for i := 0; i < windows; i++ {
		start := float64(i) * windowSeconds
		end := start + windowSeconds
		if end > duration {
			end = duration
		}

		speech := collectSpeech(transcripts, start, end)
		visual := collectVisual(visuals, start, end)

		seg := ContextSegment{
			Start:  start,
			End:    end,
			Speech: speech,
			Visual: visual,
		}
		if seg.Empty() {
			continue
		}
		segments = append(segments, seg)
	}

	ctx.Segments = segments
	return ctx
}

// collectSpeech 收集起点落在区间内的转写文本。
func collectSpeech(transcripts []TranscriptSegment, start, end float64) string {
	parts := make([]string, 0, 4)
	for _, t := range transcripts {
		if t.Start >= start && t.Start < end {
			if text := strings.TrimSpace(t.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, " ")
}

// collectVisual 收集时间点落在区间内的画面文字。
func collectVisual(visuals []FrameText, start, end float64) string {
	parts := make([]string, 0, 2)
	for _, v := range visuals {
		if v.Time >= start && v.Time < end {
			if text := strings.TrimSpace(v.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, " ")
}

// SpeechText 把上下文中的语音部分拼成一整段，用于只需要文字的场合。
func (v *VideoContext) SpeechText() string {
	parts := make([]string, 0, len(v.Segments))
	for _, s := range v.Segments {
		if s.Speech != "" {
			parts = append(parts, s.Speech)
		}
	}
	return strings.Join(parts, "\n")
}
