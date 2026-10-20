package models

import "time"

// AISummary 大模型输出的结构化分析结果。
//
// 用 JSON 列而不是拆成多个字段，原因是模型输出格式会随提示词迭代变化：
// 今天只有章节摘要，明天可能要加时间轴打点、关键词、思维导图节点。
// 用 JSON 存储时改提示词不需要改表结构，避免频繁 DDL。
//
// 独立成表是为了解耦基础转写与上层分析两条业务线：
// 转写只要跑一次，分析可能重跑多次（换模型、改提示词），
// 分开之后重跑分析不影响转录数据。
type AISummary struct {
	ID uint `gorm:"primaryKey" json:"id"`

	TaskID uint `gorm:"not null;uniqueIndex:uk_summary_task" json:"task_id"`

	// 模型返回的原始结构化数据。
	Structured string `gorm:"type:json;not null" json:"structured"`

	// 产出该结果的模型标识，便于回溯与横向对比。
	ModelName string `gorm:"size:64" json:"model_name"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
