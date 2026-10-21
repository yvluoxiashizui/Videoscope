package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"videoscope/models"
	"videoscope/repos"
)

// ProcessVideo 是一个任务的完整处理链路：下载 → 抽音轨 → 分段 → 转写 → 落库。
//
// 每一步都会在失败时立刻返回，由上层消费者决定重试还是转死信。
// 中间产物全部放在任务独立的临时目录里，函数返回时整个目录删除——
// 失败也不留垃圾，因为重试会重新走一遍。
func ProcessVideo(ctx context.Context, task *models.VideoTask) error {
	dir, err := WorkDir(task.ID)
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("清理临时目录失败 task=%d dir=%s: %v", task.ID, dir, err)
		}
	}()

	start := time.Now()

	// 1. 把对象存储里的视频拉到本地。ffmpeg 只能读本地文件。
	videoPath := filepath.Join(dir, "source"+filepath.Ext(task.ObjectKey))
	if err := DownloadObject(ctx, task.ObjectKey, videoPath); err != nil {
		return err
	}

	// 2. 抽音轨。
	audioPath := filepath.Join(dir, "audio.wav")
	if err := ExtractAudio(ctx, videoPath, audioPath); err != nil {
		return err
	}

	duration, err := ProbeDuration(ctx, audioPath)
	if err != nil {
		return err
	}
	if duration <= 0 {
		return fmt.Errorf("音轨时长异常: %.2f", duration)
	}

	// 3. 分段。短视频只有一段，长视频按 SegmentSeconds 切。
	segments, err := SplitAudio(ctx, audioPath, dir, SegmentSeconds)
	if err != nil {
		return err
	}

	// 4. 逐段转写。
	//
	// 不用并发：转写接口通常有速率限制，并发反而容易触发限流；
	// 而且分段数量不多（两小时视频约 24 段），串行的墙钟时间可以接受。
	//
	// 偏移量按分段序号推算：除最后一段外，每段的实际长度都等于 SegmentSeconds，
	// 所以第 i 段的起点就是 i * SegmentSeconds。
	// 过短的尾段已经在切分阶段被过滤，不会打乱这个规律。
	transcripts := make([]TranscriptSegment, 0)
	texts := make([]string, 0)
	for i, seg := range segments {
		offset := float64(i) * SegmentSeconds

		segs, err := ASR().Transcribe(ctx, seg, offset)
		if err != nil {
			// 带上段序号，重试时能看出是哪一段的问题。
			return fmt.Errorf("转写第 %d/%d 段: %w", i+1, len(segments), err)
		}
		for _, s := range segs {
			if text := strings.TrimSpace(s.Text); text != "" {
				transcripts = append(transcripts, s)
				texts = append(texts, text)
			}
		}
	}

	content := strings.Join(texts, "\n")
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("转写结果为空")
	}

	// 5. 抽关键帧并识别画面文字。
	//
	// 单帧识别失败不中断整个任务：黑屏、纯色画面本来就抽不出文字，
	// 这不是异常。只有所有帧都失败才值得警惕，用日志暴露出来。
	frames, err := ExtractKeyframes(ctx, videoPath, dir, KeyframeInterval)
	if err != nil {
		return fmt.Errorf("抽取关键帧: %w", err)
	}

	visuals := make([]FrameText, 0, len(frames))
	for _, f := range frames {
		text, ocrErr := OCR().Extract(ctx, f.Path)
		if ocrErr != nil {
			log.Printf("关键帧识别失败 task=%d frame=%s: %v", task.ID, filepath.Base(f.Path), ocrErr)
			continue
		}
		if text = strings.TrimSpace(text); text == "" {
			continue
		}
		visuals = append(visuals, FrameText{Time: f.Time, Text: text})
	}

	// 6. 按时间轴融合成多模态上下文。
	vctx := BuildVideoContext(task.ID, duration, transcripts, visuals, ContextWindow)

	// 7. 落库。task_id 上有唯一索引，重试会覆盖上一次的结果。
	rec := &models.VideoTranscription{
		TaskID:    task.ID,
		Content:   content,
		WordCount: len([]rune(content)),
	}
	if err := repos.SaveTranscription(rec); err != nil {
		return fmt.Errorf("保存转录稿: %w", err)
	}

	log.Printf("任务处理完成 task=%d 时长=%.1fs 转写段=%d 关键帧=%d 上下文段=%d 字数=%d 总耗时=%s",
		task.ID, duration, len(transcripts), len(frames), len(vctx.Segments), rec.WordCount,
		time.Since(start).Round(time.Millisecond))

	return nil
}
