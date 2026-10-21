package repos

import (
	"gorm.io/gorm/clause"

	"videoscope/models"
)

// SaveTranscription 写入或更新某个任务的转录稿。
//
// 用 upsert 而不是「先删后插」：重试时同一个任务会再次写进来，
// 先删会让中间有一个转录稿不存在的窗口，upsert 只有一次写入。
// 冲突键是 task_id 上的唯一索引。
func SaveTranscription(t *models.VideoTranscription) error {
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "task_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"content", "word_count", "updated_at"}),
	}).Create(t).Error
}

// FindTranscription 按任务 ID 查转录稿。
func FindTranscription(taskID uint) (*models.VideoTranscription, error) {
	var t models.VideoTranscription
	if err := db.Where("task_id = ?", taskID).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}
