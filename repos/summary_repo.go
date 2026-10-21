package repos

import (
	"gorm.io/gorm/clause"

	"videoscope/models"
)

// SaveSummary 写入或更新某个任务的分析结果。
//
// 与转录稿同样的 upsert 策略：task_id 上有唯一索引，
// 重试时会覆盖上一次的结果，不会留下多份。
func SaveSummary(s *models.AISummary) error {
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "task_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"structured", "model_name", "updated_at"}),
	}).Create(s).Error
}

// FindSummary 按任务 ID 查分析结果。
func FindSummary(taskID uint) (*models.AISummary, error) {
	var s models.AISummary
	if err := db.Where("task_id = ?", taskID).First(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}
