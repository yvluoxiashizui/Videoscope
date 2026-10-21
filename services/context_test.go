package services

import "testing"

func TestBuildVideoContextAlignment(t *testing.T) {
	transcripts := []TranscriptSegment{
		{Start: 0, End: 5, Text: "开场介绍"},
		{Start: 35, End: 40, Text: "第二段的语音"},
	}
	visuals := []FrameText{
		{Time: 30, Text: "PPT 标题"},
		{Time: 55, Text: "图表说明"},
	}

	ctx := BuildVideoContext(1, 60, transcripts, visuals, 30)

	if len(ctx.Segments) != 2 {
		t.Fatalf("期望 2 个窗口，实际 %d", len(ctx.Segments))
	}

	// 第一个窗口 0-30：只有语音，不该混入画面。
	first := ctx.Segments[0]
	if first.Start != 0 || first.End != 30 {
		t.Errorf("首段区间错误: %v-%v", first.Start, first.End)
	}
	if first.Speech != "开场介绍" {
		t.Errorf("首段语音错误: %q", first.Speech)
	}
	if first.Visual != "" {
		t.Errorf("首段不该有画面内容: %q", first.Visual)
	}

	// 第二个窗口 30-60：语音来自 35 秒，画面来自 30 和 55 秒。
	second := ctx.Segments[1]
	if second.Speech != "第二段的语音" {
		t.Errorf("次段语音错误: %q", second.Speech)
	}
	if second.Visual != "PPT 标题 图表说明" {
		t.Errorf("次段画面错误: %q", second.Visual)
	}
}

func TestBuildVideoContextSkipsEmptyWindow(t *testing.T) {
	transcripts := []TranscriptSegment{
		{Start: 0, End: 5, Text: "只在开头有声音"},
	}

	// 视频 90 秒，但只有开头五秒有内容，后面两个窗口应该是空的。
	ctx := BuildVideoContext(2, 90, transcripts, nil, 30)

	if len(ctx.Segments) != 1 {
		t.Fatalf("空窗口应被跳过，实际保留了 %d 段", len(ctx.Segments))
	}
	if ctx.Segments[0].Start != 0 {
		t.Errorf("保留的应该是首段，实际起点 %v", ctx.Segments[0].Start)
	}
}

func TestBuildVideoContextBoundary(t *testing.T) {
	// 时间戳正好落在窗口边界时，应归属于后一个窗口。
	// 规则是左闭右开 [start, end)，与切片的惯例一致。
	transcripts := []TranscriptSegment{
		{Start: 30, End: 35, Text: "边界上的语音"},
	}

	ctx := BuildVideoContext(3, 60, transcripts, nil, 30)

	if len(ctx.Segments) != 1 {
		t.Fatalf("期望 1 段，实际 %d", len(ctx.Segments))
	}
	if ctx.Segments[0].Start != 30 {
		t.Errorf("边界值应归入第二段，实际起点 %v", ctx.Segments[0].Start)
	}
}

func TestBuildVideoContextZeroDuration(t *testing.T) {
	ctx := BuildVideoContext(4, 0, nil, nil, 30)
	if len(ctx.Segments) != 0 {
		t.Errorf("零时长不该产生窗口，实际 %d", len(ctx.Segments))
	}
}
