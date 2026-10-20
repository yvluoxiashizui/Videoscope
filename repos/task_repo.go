package repos

import (
	"videoscope/models"
)

// CreateTask 创建任务。
// 视频 MD5 上有唯一索引，重复创建会返回冲突错误，
// 调用方据此判断「这个视频已经在处理或已处理过」。
func CreateTask(t *models.VideoTask) error {
	return db.Create(t).Error
}

// FindTaskByMD5 按内容摘要查询任务，未找到返回 gorm.ErrRecordNotFound。
func FindTaskByMD5(md5 string) (*models.VideoTask, error) {
	var t models.VideoTask
	if err := db.Where("video_md5 = ?", md5).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// FindTaskByID 按主键查询任务。
func FindTaskByID(id uint) (*models.VideoTask, error) {
	var t models.VideoTask
	if err := db.Where("id = ?", id).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateTaskStatus 条件更新状态：仅当当前状态等于 from 时才改成 to。
//
// 这是整个任务状态机的落地点。用「带条件的 UPDATE」而不是
// 先查后改，是因为查与改之间存在窗口：两个消费者可能同时
// 读到相同状态，各自判断可以推进，最终都写入成功。
// 条件更新把判断交给了数据库的行锁，同一时刻只有一个能成功。
//
// 返回 false 表示状态已被其他操作改变，调用方应当据此放弃本次推进。
func UpdateTaskStatus(id uint, from, to int8) (bool, error) {
	res := db.Model(&models.VideoTask{}).
		Where("id = ? AND status = ?", id, from).
		Update("status", to)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// UpdateTaskStatusWithError 在推进状态的同时记录失败原因。
//
// 用 map 而不是结构体做更新：GORM 对结构体会跳过零值字段，
// 而 ErrMsg 的零值正是空字符串——传空串来清除上一次的错误信息时，
// 结构体写法会静默不生效。
func UpdateTaskStatusWithError(id uint, from, to int8, errMsg string) (bool, error) {
	res := db.Model(&models.VideoTask{}).
		Where("id = ? AND status = ?", id, from).
		Updates(map[string]interface{}{
			"status":  to,
			"err_msg": errMsg,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// ListTasksByStatus 按状态捞取任务，用于调度器补捞积压。
// 走 (status, created_at) 联合索引，结果按创建时间升序，先处理排队更久的。
func ListTasksByStatus(status int8, limit int) ([]models.VideoTask, error) {
	var list []models.VideoTask
	err := db.Where("status = ?", status).
		Order("created_at ASC").
		Limit(limit).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

// ListTasksByUser 查询某个用户的任务，按时间倒序。
// 走 (user_id, created_at) 联合索引，排序在索引里完成，不需要额外的 filesort。
func ListTasksByUser(userID uint, limit int) ([]models.VideoTask, error) {
	var list []models.VideoTask
	err := db.Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}
