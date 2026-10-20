package models

import "time"

// 任务状态。
//
// 用数字而不是字符串：状态字段读写频繁，tinyint 占一个字节、比较是整数运算；
// 存「处理中」这类中文串要走字符集与排序规则，索引体积也大。
// 可读性问题在代码里用常量名解决，不指望数据库里存中文。
const (
	TaskPending    int8 = 0 // 已创建，等待分片传完
	TaskQueued     int8 = 1 // 已入队，等待消费者
	TaskProcessing int8 = 2 // 消费者处理中
	TaskDone       int8 = 3 // 已完成
	TaskFailed     int8 = 4 // 失败
)

// StatusText 返回状态的中文描述，用于日志与前端展示。
func StatusText(s int8) string {
	switch s {
	case TaskPending:
		return "待上传"
	case TaskQueued:
		return "排队中"
	case TaskProcessing:
		return "处理中"
	case TaskDone:
		return "已完成"
	case TaskFailed:
		return "失败"
	default:
		return "未知"
	}
}

// VideoTask 视频处理任务，整个异步流程的枢纽。
//
// 这张表不加逻辑删除。原因是 VideoMD5 上有唯一索引，
// 它承担着内容级去重与消费幂等的物理兜底；
// 如果引入 deleted_at，已删除的行仍占着索引，
// 同一个视频再次上传时会撞唯一键，而 MySQL 的唯一索引不忽略软删除行。
//
// 任务记录本身属于审计数据，业务上也不提供删除入口。
type VideoTask struct {
	ID     uint `gorm:"primaryKey" json:"id"`
	UserID uint `gorm:"not null;index:idx_user_created,priority:1" json:"user_id"`

	// 内容摘要，去重与幂等都以它为准。
	VideoMD5 string `gorm:"size:32;not null;uniqueIndex:uk_video_md5" json:"video_md5"`

	// 合并后成品在对象存储中的路径。
	ObjectKey string `gorm:"size:255;not null" json:"object_key"`

	Filename string `gorm:"size:255" json:"filename"`
	Size     int64  `gorm:"not null;default:0" json:"size"`

	// 状态机字段，后台调度与消息消费都依赖它做幂等判断。
	Status int8 `gorm:"not null;default:0;index:idx_status_created,priority:1" json:"status"`

	// 失败原因，仅 Status 为失败时有值。
	ErrMsg string `gorm:"size:512" json:"err_msg,omitempty"`

	// 两个联合索引：
	// (user_id, created_at) 支持「查我的历史任务」，结果天然按时间有序；
	// (status, created_at) 支持调度器按状态捞取积压任务。
	CreatedAt time.Time `gorm:"index:idx_user_created,priority:2;index:idx_status_created,priority:2" json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
