package services

import (
	"fmt"
	"strings"
)

// 切块参数。
//
// 上限按字数控制：转写文本以中文为主，五百字大约是模型一次能
// 完整理解又不会稀释重点的长度。
//
// 块之间保留一段重叠，是因为切分点往往落在一句话中间，
// 不重叠的话那句话会被拆成两半，两块都读不通。
const (
	ChunkMaxChars   = 500
	ChunkOverlapLen = 60
)

// Chunk 是切块后的一个检索单元。
type Chunk struct {
	Index   int
	Start   float64
	End     float64
	Content string
}

// ChunkContext 把多模态上下文切成检索块。
//
// 切分依据是内容的自然段落而不是固定字数：每个上下文窗口本身就是
// 一个完整的时间片，按窗口累积到接近上限再断开，块边界刚好落在
// 语义停顿处，比强行按字数截断可读性更好。
func ChunkContext(vctx *VideoContext) []Chunk {
	if vctx == nil || len(vctx.Segments) == 0 {
		return nil
	}

	chunks := make([]Chunk, 0, len(vctx.Segments)/2+1)

	var (
		buf      strings.Builder
		start    float64
		end      float64
		hasStart bool
	)

	flush := func() {
		content := strings.TrimSpace(buf.String())
		if content == "" {
			return
		}
		chunks = append(chunks, Chunk{
			Index:   len(chunks),
			Start:   start,
			End:     end,
			Content: content,
		})
		buf.Reset()
		hasStart = false
	}

	for _, seg := range vctx.Segments {
		text := segmentText(seg)
		if text == "" {
			continue
		}

		// 加上这一段的文字会超过上限，先把已攒的块落下来。
		if buf.Len() > 0 && buf.Len()+len(text) > ChunkMaxChars {
			overflow := overlapTail(buf.String())
			flush()

			// 用上一块的末尾做重叠，保证跨块的句子两块都能读到。
			if overflow != "" {
				buf.WriteString(overflow)
				buf.WriteString("\n")
			}
		}

		if !hasStart {
			start = seg.Start
			hasStart = true
		}
		end = seg.End

		fmt.Fprintf(&buf, "[%.0f-%.0f秒] %s\n", seg.Start, seg.End, text)
	}

	flush()
	return chunks
}

// segmentText 取一个时间片里用于检索的文字。
//
// 语音和画面拼在一起而不是只取语音：画面上的板书、公式、代码
// 往往不在语音里提，但对内容理解很关键，只索引语音会漏掉这一半信息。
func segmentText(seg ContextSegment) string {
	parts := make([]string, 0, 2)
	if speech := strings.TrimSpace(seg.Speech); speech != "" {
		parts = append(parts, speech)
	}
	if visual := strings.TrimSpace(seg.Visual); visual != "" {
		parts = append(parts, "画面："+visual)
	}
	return strings.Join(parts, " ")
}

// overlapTail 取字符串末尾的一段作为下一块的开头。
//
// 按字符而不是字节截取：中文一个字符占三字节，按字节切会把
// 一个字劈成两半，产生乱码。
func overlapTail(s string) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= ChunkOverlapLen {
		return ""
	}
	return string(runes[len(runes)-ChunkOverlapLen:])
}
