package models

import "time"

// VideoTranscription 语音转写的逐字稿。
//
// 单独成表是垂直拆分：一节两小时的课转写下来几万字，
// 如果和任务流水放在同一张表，用户刷新任务列表时
// 数据库要把这一大段文本一起加载进内存再丢弃，主表查询会被拖垮。
//
// 拆开之后是一对一关系，主表保持窄行，高频查询不受影响。
type VideoTranscription struct {
	ID uint `gorm:"primaryKey" json:"id"`

	// 一个任务只对应一份转录稿，用唯一索引保证重复消费不会写出两条。
	TaskID uint `gorm:"not null;uniqueIndex:uk_transcription_task" json:"task_id"`

	// 用 mediumtext 而不是 text：text 上限 64KB，一小时课程的字数会超。
	Content string `gorm:"type:mediumtext;not null" json:"content"`

	// 字数用于前端展示与后续按长度分段处理。
	WordCount int `gorm:"not null;default:0" json:"word_count"`

	// ContextJSON 保存融合后的多模态上下文，作为失败重试的断点。
	//
	// 转写和画面识别是整条链路里最耗时的两步，结果都合并在上下文里。
	// 后续的模型调用或索引建立失败时，从这里恢复就能跳过前面几步，
	// 不必重新下载视频、重新抽音轨、重新识别一遍。
	//
	// 存 JSON 而不是拆成多张表：这个字段只用于恢复，不参与查询，
	// 拆表带不来任何查询上的收益，只增加维护成本。
	ContextJSON string `gorm:"type:mediumtext" json:"-"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
