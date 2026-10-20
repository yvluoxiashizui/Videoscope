package services

import (
	"errors"
	"fmt"

	"videoscope/models"
	"videoscope/repos"
)

// allowedTransitions 定义所有合法的状态流转。
//
// 用显式的表而不是散落在业务代码里的 if 判断：
// 判断散落在各处时，新增一个状态很容易漏掉某条路径，
// 最终表现为任务能跳过中间态直接完成。
var allowedTransitions = map[int8][]int8{
	models.TaskPending:    {models.TaskQueued},
	models.TaskQueued:     {models.TaskProcessing, models.TaskFailed},
	models.TaskProcessing: {models.TaskDone, models.TaskFailed},
	models.TaskDone:       {},
	// 失败后允许重新入队，这是重试的唯一入口。
	// 不能靠新建任务绕过：同一个视频的 MD5 上有唯一索引。
	models.TaskFailed: {models.TaskQueued},
}

// ErrIllegalTransition 表示调用方试图做一次不允许的状态流转。
var ErrIllegalTransition = errors.New("非法的状态流转")

// ErrStatusConflict 表示任务当前状态与预期不符，
// 通常意味着另一个执行者已经先一步推进了状态。
var ErrStatusConflict = errors.New("任务状态已被其他操作更新")

// CanTransition 判断一次状态流转是否合法。
func CanTransition(from, to int8) bool {
	for _, candidate := range allowedTransitions[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

// CreateTask 创建任务记录，初始状态为待上传。
func CreateTask(userID uint, md5, objectKey, filename string, size int64) (*models.VideoTask, error) {
	t := &models.VideoTask{
		UserID:    userID,
		VideoMD5:  md5,
		ObjectKey: objectKey,
		Filename:  filename,
		Size:      size,
		Status:    models.TaskPending,
	}
	if err := repos.CreateTask(t); err != nil {
		return nil, fmt.Errorf("创建任务: %w", err)
	}
	return t, nil
}

// Transition 推进任务状态。
//
// 两道防线缺一不可：先按流转表校验合法性，拦住代码层面的错误调用；
// 再用带条件的更新落地，拦住并发场景下两个执行者同时推进。
// 只做前者，两个消费者可能同时通过校验再各自写入；
// 只做后者，非法跳转会在运行到一半时才失败。
func Transition(taskID uint, from, to int8, errMsg string) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, models.StatusText(from), models.StatusText(to))
	}

	// 错误信息只对失败态有意义，推进到其他状态时清空，
	// 避免上一次失败的原因留在记录里误导排查。
	if to != models.TaskFailed {
		errMsg = ""
	}

	updated, err := repos.UpdateTaskStatusWithError(taskID, from, to, errMsg)
	if err != nil {
		return fmt.Errorf("更新任务状态: %w", err)
	}
	if !updated {
		return ErrStatusConflict
	}
	return nil
}

// GetTask 查询任务。
func GetTask(taskID uint) (*models.VideoTask, error) {
	return repos.FindTaskByID(taskID)
}

// FindTaskByMD5 按内容摘要查任务，用于上传阶段的去重判断。
func FindTaskByMD5(md5 string) (*models.VideoTask, error) {
	return repos.FindTaskByMD5(md5)
}
