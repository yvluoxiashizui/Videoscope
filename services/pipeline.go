package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"videoscope/models"
	"videoscope/repos"
)

// asrConcurrency 是转写时同时进行的请求数。
//
// 实测十二段三十秒音频：串行约十四秒，五路并发降到约五秒，
// 继续加到八路没有明显收益，单次请求的耗时波动反而变大。
//
// 所以这个值不是被对方限流卡住的——八路也没有出现请求被拒绝。
// 它停在收益开始变平的地方：再增加并发只会占用更多配额，
// 换不回等比例的提速。
const asrConcurrency = 5

// ProcessVideo 是一个任务的完整处理链路：下载 → 抽音轨 → 分段 → 转写 → 落库。
//
// 每一步都会在失败时立刻返回，由上层消费者决定重试还是转死信。
// 中间产物全部放在任务独立的临时目录里，函数返回时整个目录删除。
//
// 重试前会先查一次上下文快照：如果上一次已经跑完了转写和画面识别、
// 只是后面的模型调用或建索引失败，那次快照还在，
// 直接从快照恢复即可，不必把最耗时的那几段重新算一遍。
func ProcessVideo(ctx context.Context, task *models.VideoTask) error {
	start := time.Now()

	// 0. 先看有没有可恢复的断点。
	if vctx := LoadCheckpoint(ctx, task.ID); vctx != nil {
		log.Printf("跳过重复处理 task=%d 从上下文快照继续", task.ID)
		// 恢复路径拿不到原始的分段数与帧数，用快照里的规模代替展示。
		return finishAnalysis(ctx, task, vctx, start, 0, 0, 0)
	}

	dir, err := WorkDir(task.ID)
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("清理临时目录失败 task=%d dir=%s: %v", task.ID, dir, err)
		}
	}()

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

	// 4. 分段转写。
	//
	// 并发跑，用固定大小的 worker 池限制同时进行的请求数。
	//
	// 早期是串行：一段转完再转下一段。小视频看不出问题，
	// 但五分钟的音频切出十段，每段一次网络往返，串行下来
	// 光转写就要四五分钟。分段之间彼此独立，没有先后依赖，
	// 串行等于把可以重叠的等待时间全部排成了队列。
	//
	// 并发数也不宜无限大：实测加到八路之后总耗时不再下降，
	// 只是让单次请求的延迟波动变大。取五路是收益开始变平的位置。
	transcripts := make([]TranscriptSegment, 0)
	texts := make([]string, 0)

	results := make([][]TranscriptSegment, len(segments))
	errs := make([]error, len(segments))

	sem := make(chan struct{}, asrConcurrency)
	var wg sync.WaitGroup

	for i := range segments {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				errs[i] = ctx.Err()
				return
			}

			// 偏移量按分段序号推算：除最后一段外每段长度都等于 SegmentSeconds，
			// 所以第 i 段的起点就是 i * SegmentSeconds。
			// 过短的尾段已在切分阶段被过滤，不会打乱这个规律。
			offset := float64(i) * SegmentSeconds
			results[i], errs[i] = ASR().Transcribe(ctx, segments[i], offset)
		}(i)
	}
	wg.Wait()

	// 并发执行后按序号检查错误，报错时能明确指出是哪一段出了问题。
	for i, err := range errs {
		if err != nil {
			return fmt.Errorf("转写第 %d/%d 段: %w", i+1, len(segments), err)
		}
	}

	// 按分段顺序拼接结果。并发完成顺序是乱的，
	// 但内容必须按时间先后排列，所以这里按索引取而不是按完成顺序。
	for _, segs := range results {
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
	// 抽帧失败不中断整个任务：语音转写已经有结果，
	// 少了画面信息只是让上下文缺一个维度，比整条链路作废好。
	// 有些视频本身就没有可用的视频流，或者编码格式特殊抽不出帧，
	// 这类情况不该让用户拿不到任何结果。
	frames, frameErr := ExtractKeyframes(ctx, videoPath, dir, KeyframeInterval)
	if frameErr != nil {
		log.Printf("抽取关键帧失败，跳过画面识别 task=%d: %v", task.ID, frameErr)
		frames = nil
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

	// 7. 转录稿与上下文快照一起落库。
	//
	// 快照在这里写，是因为前面的下载、抽音轨、转写、画面识别
	// 是整条链路里最耗时的部分。后续模型调用或索引失败、消息被重投时，
	// 从快照恢复就能跳过这几步，不必重新算一遍。
	snapshot, snapErr := PrepareCheckpoint(vctx)
	if snapErr != nil {
		log.Printf("生成上下文快照失败 task=%d: %v", task.ID, snapErr)
	}

	rec := &models.VideoTranscription{
		TaskID:      task.ID,
		Content:     content,
		WordCount:   len([]rune(content)),
		ContextJSON: snapshot,
	}
	if err := repos.SaveTranscription(rec); err != nil {
		return fmt.Errorf("保存转录稿: %w", err)
	}

	// 8. 后续分析与建索引。
	return finishAnalysis(ctx, task, vctx, start,
		len(transcripts), len(frames), rec.WordCount)
}

// finishAnalysis 是断点恢复与完整处理共用的后半段。
//
// 从快照恢复的任务跳过前面所有步骤，直接进这里；
// 完整处理的任务走完全程后也落到这里，两条路径只有一份实现。
func finishAnalysis(ctx context.Context, task *models.VideoTask, vctx *VideoContext, start time.Time,
	transcriptCount, frameCount, wordCount int) error {

	// 从快照恢复时拿不到原始的转录稿字数，用上下文里的语音文本重新统计，
	// 避免日志里出现「字数=0」这种误导性的值。
	if wordCount == 0 {
		wordCount = len([]rune(vctx.SpeechText()))
	}

	result, err := Analyze(ctx, vctx)
	if err != nil {
		return fmt.Errorf("生成分析结果: %w", err)
	}

	structured, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("序列化分析结果: %w", err)
	}

	summary := &models.AISummary{
		TaskID:     task.ID,
		Structured: string(structured),
		ModelName:  LLMModelName(),
	}
	if err := repos.SaveSummary(summary); err != nil {
		return fmt.Errorf("保存分析结果: %w", err)
	}

	// 建立检索索引。
	//
	// 失败不阻断主流程：分析结果已经落库，用户可以正常看总结，
	// 只是追问功能暂时不可用。把它当成任务失败会让前面的成果白费，
	// 而索引随时可以重建。
	chunkCount, idxErr := IndexTask(ctx, task.ID, vctx)
	if idxErr != nil {
		log.Printf("建立检索索引失败 task=%d: %v", task.ID, idxErr)
	}

	log.Printf("任务处理完成 task=%d 时长=%.1fs 转写段=%d 关键帧=%d 上下文段=%d 字数=%d 章节=%d 索引块=%d 模型=%s 总耗时=%s",
		task.ID, vctx.Duration, transcriptCount, frameCount, len(vctx.Segments), wordCount,
		len(result.Chapters), chunkCount, LLMModelName(), time.Since(start).Round(time.Millisecond))

	return nil
}
