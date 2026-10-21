package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// OCRExtractor 从一张图片里提取文字。
type OCRExtractor interface {
	Extract(ctx context.Context, imagePath string) (string, error)
}

// NewOCRExtractor 返回可用的实现。
//
// 检测到系统里有 tesseract 就用它，没有就退化为模拟实现。
// 这样本机没装也能跑通链路，装了则自动切换到真实识别。
func NewOCRExtractor() OCRExtractor {
	if _, err := exec.LookPath("tesseract"); err == nil {
		return tesseractExtractor{}
	}
	return mockOCR{}
}

// tesseractExtractor 调用本地 tesseract 命令行。
//
// 语言优先用中文简体加英文混合，识别不了再退回纯英文——
// 中文语言包不是默认安装的，缺了会让整条命令直接失败。
type tesseractExtractor struct{}

func (tesseractExtractor) Extract(ctx context.Context, imagePath string) (string, error) {
	text, err := runTesseract(ctx, imagePath, "chi_sim+eng")
	if err == nil {
		return text, nil
	}

	fallback, fallbackErr := runTesseract(ctx, imagePath, "eng")
	if fallbackErr != nil {
		return "", fmt.Errorf("OCR 识别失败: %w", err)
	}
	return fallback, nil
}

func runTesseract(ctx context.Context, imagePath, lang string) (string, error) {
	cmd := exec.CommandContext(ctx, "tesseract", imagePath, "stdout", "-l", lang)

	var stderr strings.Builder
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tesseract(%s): %w (%s)", lang, err, tail(stderr.String(), 200))
	}
	return strings.TrimSpace(string(out)), nil
}

// mockOCR 是本地开发用的占位实现。
//
// 返回一个带文件名和时间的标记，让依赖它的对齐逻辑能跑出真实形状，
// 同时一眼能看出这不是真实识别结果。
type mockOCR struct{}

func (mockOCR) Extract(ctx context.Context, imagePath string) (string, error) {
	info, err := os.Stat(imagePath)
	if err != nil {
		return "", fmt.Errorf("读取关键帧: %w", err)
	}
	return fmt.Sprintf("[模拟画面文字] %s，%d 字节",
		filepath.Base(imagePath), info.Size()), nil
}
