package repos

import (
	"gorm.io/gorm"

	"videoscope/models"
)

// SaveChunks 重建某个任务的检索块索引。
//
// 先删后插而不是逐条 upsert：重建索引时新旧块的数量可能不同，
// 逐条更新会残留上一次多出来的块，检索时命中过期内容。
// 整个过程放在一个事务里，避免删成功而写失败留下空索引。
func SaveChunks(taskID uint, chunks []models.VideoChunk) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ?", taskID).Delete(&models.VideoChunk{}).Error; err != nil {
			return err
		}
		if len(chunks) == 0 {
			return nil
		}
		return tx.Create(&chunks).Error
	})
}

// FindChunksByTask 取出某个任务的全部检索块。
//
// 当前是全量取出后在应用层算相似度。单任务通常十几到几十块，
// 这个量级下引入向量数据库只增加部署复杂度，不带来实际收益。
func FindChunksByTask(taskID uint) ([]models.VideoChunk, error) {
	var chunks []models.VideoChunk
	err := db.Where("task_id = ?", taskID).
		Order("chunk_index ASC").
		Find(&chunks).Error
	if err != nil {
		return nil, err
	}
	return chunks, nil
}
