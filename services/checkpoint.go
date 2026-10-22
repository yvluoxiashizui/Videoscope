package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"videoscope/repos"
)

// SaveCheckpoint 保存处理上下文快照，供失败重试时恢复。
//
// 保存时机在转写与画面识别完成之后、模型调用之前。
// 这个位置的选择是：再往前存，恢复不出完整上下文；
// 再往后存，最贵的那一步已经跑完了，存了也没什么可省的。
func SaveCheckpoint(ctx context.Context, taskID uint, vctx *VideoContext) error {
	if vctx == nil || len(vctx.Segments) == 0 {
		return fmt.Errorf("上下文为空，不保存断点")
	}

	payload, err := json.Marshal(vctx)
	if err != nil {
		return fmt.Errorf("序列化上下文: %w", err)
	}

	if err := repos.SaveContext(taskID, string(payload)); err != nil {
		return fmt.Errorf("保存上下文快照: %w", err)
	}
	return nil
}

// LoadCheckpoint 读取处理上下文快照。
//
// 返回 nil 表示没有可用的断点，调用方应当完整重跑。
// 读取失败或者内容损坏都按「没有断点」处理——
// 恢复失败退回全量处理是安全的，而报错中止会让任务卡死。
func LoadCheckpoint(ctx context.Context, taskID uint) *VideoContext {
	payload, err := repos.LoadContext(taskID)
	if err != nil {
		// 首次处理时没有记录，属于正常情况，不记日志。
		return nil
	}
	if payload == "" {
		return nil
	}

	var vctx VideoContext
	if err := json.Unmarshal([]byte(payload), &vctx); err != nil {
		log.Printf("上下文快照损坏，改为全量重跑 task=%d: %v", taskID, err)
		return nil
	}
	if len(vctx.Segments) == 0 {
		return nil
	}

	log.Printf("从断点恢复 task=%d 上下文段=%d 时长=%.1fs", taskID, len(vctx.Segments), vctx.Duration)
	return &vctx
}
