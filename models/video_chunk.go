package models

import "time"

// VideoChunk 是转写内容的一个检索单元。
//
// 长视频的转写动辄几万字，追问时不可能整段塞给模型：
// 成本随长度线性增长，而且无关内容会干扰模型判断。
// 所以把内容切成块，每块单独向量化，检索时只取最相关的几块。
//
// 每块记录起止时间，命中之后可以把答案定位回视频的具体位置，
// 这比只给一段文字有用得多。
type VideoChunk struct {
	ID     uint `gorm:"primaryKey" json:"id"`
	TaskID uint `gorm:"not null;index:idx_chunk_task,priority:1" json:"task_id"`

	// 块在任务内的序号，用于保持原文顺序。
	ChunkIndex int `gorm:"not null" json:"chunk_index"`

	StartSeconds float64 `gorm:"not null" json:"start_seconds"`
	EndSeconds   float64 `gorm:"not null" json:"end_seconds"`

	Content string `gorm:"type:text;not null" json:"content"`

	// 向量以 JSON 数组形式存储。
	//
	// 当前没有引入向量数据库：单任务通常只有十几到几十个块，
	// 全量取出在内存里算余弦相似度完全够用，多引入一个组件
	// 只是增加部署与运维负担。
	//
	// 数据量上来之后再换：可以接 pgvector、Qdrant 或者 Milvus，
	// 检索接口的签名不用变，改的是这一层的实现。
	Embedding string `gorm:"type:mediumtext" json:"-"`

	CreatedAt time.Time `json:"created_at"`
}
