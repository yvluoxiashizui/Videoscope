package services

import (
	"context"
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
	models.TaskPending: {models.TaskQueued},
	// Queued 可以退回 Pending：投递消息失败时任务并没有真正进入队列，
	// 留在排队状态会让用户重试时被告知「已在处理中」，任务永久卡死。
	// 这条回退路径是投递失败的唯一出口。
	models.TaskQueued:     {models.TaskProcessing, models.TaskFailed, models.TaskPending},
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

// EnsureTask 保证一个视频有且只有一条任务记录，返回已有记录或新建的记录。
//
// 上传完成后调用。前端在合并超时后可能重试，同一个 MD5 会再次走到这里，
// 所以先查一次；查到就直接返回旧记录，不覆盖它已有的状态——
// 覆盖会把一个正在分析的任务打回待分析。
//
// 查询与插入之间仍有并发窗口，但 MD5 上的唯一索引会挡住重复插入，
// 插入失败时回查一次即可，这比加锁简单得多。
func EnsureTask(userID uint, md5, objectKey, filename string) (*models.VideoTask, error) {
	if existing, err := repos.FindTaskByMD5(md5); err == nil {
		return existing, nil
	}

	// 文件大小不影响业务逻辑，查不到就记 0，不为此中断上传流程。
	size, _ := ObjectSize(context.Background(), objectKey)

	t := &models.VideoTask{
		UserID:    userID,
		VideoMD5:  md5,
		ObjectKey: objectKey,
		Filename:  filename,
		Size:      size,
		Status:    models.TaskPending,
	}
	if err := repos.CreateTask(t); err != nil {
		if existing, findErr := repos.FindTaskByMD5(md5); findErr == nil {
			return existing, nil
		}
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

	// 状态落地之后再发通知。
	// 顺序反了会出现「客户端看到完成、数据库里还是处理中」的时间窗，
	// 用户刷新页面状态又退回去。
	PublishTransition(taskID, to, errMsg)

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
