package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"videoscope/repos"
)

// 断点恢复。
//
// 整条链路里最耗时的是转写与画面识别，两者结果合并在多模态上下文里。
// 后续的模型调用或索引建立失败时，任务会被消息队列重投；
// 如果没有断点，重投意味着重新下载视频、重新抽音轨、重新识别一遍——
// 前面几分钟的计算和调用费用全部白付。
//
// 上下文快照随转录稿一起落库，恢复时直接取出来用。

// PrepareCheckpoint 把上下文序列化成可入库的字符串。
//
// 与转录稿一起写入，避免先写一份再更新一次。
func PrepareCheckpoint(vctx *VideoContext) (string, error) {
	if vctx == nil || len(vctx.Segments) == 0 {
		return "", fmt.Errorf("上下文为空")
	}
	payload, err := json.Marshal(vctx)
	if err != nil {
		return "", fmt.Errorf("序列化上下文: %w", err)
	}
	return string(payload), nil
}

// LoadCheckpoint 读取处理上下文快照。
//
// 返回 nil 表示没有可用的断点，调用方应当完整重跑。
// 读取失败或内容损坏都按「没有断点」处理——
// 恢复不了就退回全量重跑，这是安全的；报错中止会让任务直接卡死。
func LoadCheckpoint(ctx context.Context, taskID uint) *VideoContext {
	payload, err := repos.LoadContext(taskID)
	if err != nil {
		// 首次处理时没有记录，属于正常情况。
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
