package repos

import (
	"gorm.io/gorm/clause"

	"videoscope/models"
)

// SaveTranscription 写入或更新某个任务的转录稿与处理上下文。
//
// 用 upsert 而不是「先删后插」：重试时同一个任务会再次写进来，
// 先删会让中间有一个转录稿不存在的窗口，upsert 只有一次写入。
// 冲突键是 task_id 上的唯一索引。
func SaveTranscription(t *models.VideoTranscription) error {
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "task_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"content", "word_count", "context_json", "updated_at",
		}),
	}).Create(t).Error
}

// SaveContext 单独更新某个任务的上下文快照。
//
// 与 SaveTranscription 分开，是因为两者的写入时机不同：
// 上下文在转写与画面识别完成后立刻要能恢复，
// 而转录稿的内容可能还要在后续步骤里补充。
func SaveContext(taskID uint, payload string) error {
	return db.Model(&models.VideoTranscription{}).
		Where("task_id = ?", taskID).
		Update("context_json", payload).Error
}

// LoadContext 读取某个任务的上下文快照。
// 没有记录或字段为空时返回空字符串，调用方据此判断是否需要重跑。
func LoadContext(taskID uint) (string, error) {
	var rec models.VideoTranscription
	err := db.Select("context_json").Where("task_id = ?", taskID).First(&rec).Error
	if err != nil {
		return "", err
	}
	return rec.ContextJSON, nil
}

// FindTranscription 按任务 ID 查转录稿。
func FindTranscription(taskID uint) (*models.VideoTranscription, error) {
	var t models.VideoTranscription
	if err := db.Where("task_id = ?", taskID).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}
