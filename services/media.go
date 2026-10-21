package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ffmpegSlots 限制同时运行的转码进程数。
//
// 转码是 CPU 密集型任务，同时开太多只会互相抢核、整体变慢，
// 还可能把内存打满。消费者那边已经用 Qos(1) 控制了一条一条处理，
// 这里再加一层是因为后续可能出现并行抽取（音轨和关键帧同时跑）。
var ffmpegSlots = make(chan struct{}, 2)

// SegmentSeconds 是单个转写分段的时长（秒）。
//
// 分段不是为了并行，是因为语音识别接口对单次请求的音频长度有限制，
// 而且一整条音轨上传失败要整体重来，分成五分钟一段后，
// 某一段失败只需要重试那一段。
const SegmentSeconds = 300

// ExtractAudio 从视频中抽取音轨，转成语音识别需要的格式。
//
// ASR 模型的标准输入是 16kHz 单声道 PCM：
// 采样率高了不会被识别得更准，只会让上传体积变大；
// 单声道是因为语音识别不利用立体声的空间信息。
func ExtractAudio(ctx context.Context, src, dst string) error {
	release, err := acquireFFmpeg(ctx)
	if err != nil {
		return err
	}
	defer release()

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",      // 覆盖已存在的输出，避免重试时因为文件存在失败
		"-i", src, //
		"-vn", // 丢掉视频流，只处理音频
		"-acodec", "pcm_s16le",
		"-ar", "16000",
		"-ac", "1",
		dst,
	)

	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("抽取音轨: %w (ffmpeg: %s)", err, tail(stderr.String(), 400))
	}
	return nil
}

// ProbeDuration 返回媒体文件的时长，单位秒。
func ProbeDuration(ctx context.Context, path string) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("探测时长: %w", err)
	}

	raw := strings.TrimSpace(string(out))
	d, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("解析时长 %q: %w", raw, err)
	}
	return d, nil
}

// SplitAudio 把音轨按时长切段，返回切出来的文件路径。
//
// 用 ffmpeg 的 segment 复用器而不是逐段 -ss 截取：
// 一次解码就能切完，而逐段截取每段都要从头定位，段数一多累计开销很明显。
func SplitAudio(ctx context.Context, src, outDir string, seconds int) ([]string, error) {
	release, err := acquireFFmpeg(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	pattern := filepath.Join(outDir, "seg_%03d.wav")

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-i", src,
		"-f", "segment",
		"-segment_time", strconv.Itoa(seconds),
		"-c", "copy",
		pattern,
	)

	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("切分音轨: %w (ffmpeg: %s)", err, tail(stderr.String(), 400))
	}

	// 按文件名排序读回结果。
	// ffmpeg 的分段序号是补零的，字典序与实际时间顺序一致。
	matches, err := filepath.Glob(filepath.Join(outDir, "seg_*.wav"))
	if err != nil {
		return nil, fmt.Errorf("查找分段文件: %w", err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("切分后没有生成任何分段")
	}

	// Glob 不保证顺序，显式排一次。
	sort.Strings(matches)
	return matches, nil
}

// acquireFFmpeg 获取一个转码槽位，拿不到时等待或随 ctx 取消。
func acquireFFmpeg(ctx context.Context) (func(), error) {
	select {
	case ffmpegSlots <- struct{}{}:
		return func() { <-ffmpegSlots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("等待转码槽位: %w", ctx.Err())
	}
}

// WorkDir 为某个任务创建临时工作目录。
// 调用方负责在用完后删除整个目录。
func WorkDir(taskID uint) (string, error) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("videoscope-task-%d", taskID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建工作目录: %w", err)
	}
	return dir, nil
}

// tail 截取字符串末尾的部分用于日志。
// 按 rune 截断，避免把多字节字符切成半个导致日志乱码。
func tail(s string, limit int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return "..." + string(r[len(r)-limit:])
}
