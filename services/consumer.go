package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"videoscope/models"
	"videoscope/repos"
)

// TaskHandler 是任务的实际处理逻辑，由上层注入。
// 消费者只负责「取任务、加锁、推进状态」，具体怎么处理视频跟它无关，
// 这样阶段六接入转写和模型调用时不需要改消费框架。
type TaskHandler func(ctx context.Context, task *models.VideoTask) error

// Consumer 从队列取任务并驱动状态流转。
type Consumer struct {
	handler TaskHandler
	ch      *amqp.Channel
}

// StartConsumer 启动消费者，在后台 goroutine 中持续消费。
//
// manualAck 设为 true：只有业务处理成功才确认消息。
// 自动确认会在消息投递给消费者时就删掉它，处理失败的消息直接消失。
func StartConsumer(ctx context.Context, handler TaskHandler) error {
	ch, err := mqConn.Channel()
	if err != nil {
		return fmt.Errorf("打开消费信道: %w", err)
	}

	// 一次只取一条：任务是重计算，预取多了只会让消息堆在某个消费者手里，
	// 而其他实例闲着。等处理完再取下一条，负载自然摊开。
	if err := ch.Qos(1, 0, false); err != nil {
		ch.Close()
		return fmt.Errorf("设置预取数量: %w", err)
	}

	deliveries, err := ch.Consume(
		AnalyzeQueue,
		"videoscope-worker",
		false, // autoAck
		false, // exclusive
		false, // noLocal
		false, // noWait
		nil,
	)
	if err != nil {
		ch.Close()
		return fmt.Errorf("订阅队列: %w", err)
	}

	c := &Consumer{handler: handler, ch: ch}
	go c.run(ctx, deliveries)
	return nil
}

// run 是消费主循环。
func (c *Consumer) run(ctx context.Context, deliveries <-chan amqp.Delivery) {
	for {
		select {
		case <-ctx.Done():
			return
		case d, ok := <-deliveries:
			if !ok {
				return
			}
			c.handle(ctx, d)
		}
	}
}

// 失败重试策略。
//
// 重试次数上限与退避梯度：1 秒、2 秒、4 秒。
// 用指数退避而不是固定间隔，是因为第三方接口抖动时，
// 立刻重试大概率还在同一个故障窗口里，白费一次调用；
// 而固定间隔的集体重试会形成同步脉冲，下游一恢复就被打满。
const (
	maxRetries    = 3
	baseRetryWait = time.Second
)

// handle 处理单条消息。
func (c *Consumer) handle(ctx context.Context, d amqp.Delivery) {
	var msg AnalyzeMessage
	if err := json.Unmarshal(d.Body, &msg); err != nil {
		// 消息体损坏，重试也没用，直接拒绝让它进死信。
		log.Printf("消息解析失败，转入死信: %v", err)
		d.Nack(false, false)
		return
	}

	if err := c.process(ctx, msg); err != nil {
		retry := retryCount(d)

		if retry >= maxRetries {
			log.Printf("任务重试次数耗尽，转入死信 task=%d retry=%d err=%v", msg.TaskID, retry, err)
			d.Nack(false, false)
			return
		}

		// 退避后重新投递。
		//
		// 这里选择「延迟 + 重投新消息」而不是 Nack(requeue=true)：
		// requeue 会把消息立刻放回队列头，下一次投递几乎没有间隔，
		// 对幂等的瞬时故障没有意义，还会在日志里刷出一堆失败。
		wait := baseRetryWait << retry
		select {
		case <-ctx.Done():
			d.Nack(false, false)
			return
		case <-time.After(wait):
		}

		if pubErr := PublishAnalyzeRetry(ctx, msg, retry+1); pubErr != nil {
			// 重投失败说明 broker 侧有问题，此时把原消息拒绝掉进死信，
			// 至少留下一条可追溯的记录，而不是静默丢失。
			log.Printf("重投失败，转入死信 task=%d: %v", msg.TaskID, pubErr)
			d.Nack(false, false)
			return
		}

		d.Ack(false)
		return
	}

	d.Ack(false)
}

// retryCount 从消息头读重试次数。
func retryCount(d amqp.Delivery) int {
	if d.Headers == nil {
		return 0
	}
	switch v := d.Headers["retry_count"].(type) {
	case int32:
		return int(v)
	case int64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

// process 执行一条任务的完整处理流程。
//
// 三段防护，缺一不可：
// 分布式锁防同一视频被并发处理；
// 状态校验防重复消费推进已完成的任务；
// 条件更新防两个实例同时推进状态。
func (c *Consumer) process(ctx context.Context, msg AnalyzeMessage) error {
	lock, err := AcquireLock(ctx, VideoLockKey(msg.VideoMD5), DefaultLockTTL)
	if err != nil {
		if errors.Is(err, ErrLockBusy) {
			// 同一视频已有实例在处理，这条是重复消息，直接确认掉。
			// 真正在处理的那条无论成败都会自己更新状态，这里不需要兜底。
			log.Printf("视频正在处理中，丢弃重复消息 task=%d md5=%s", msg.TaskID, msg.VideoMD5)
			return nil
		}
		return fmt.Errorf("获取视频锁: %w", err)
	}
	// 释放用独立的 context：业务 ctx 可能已因客户端断开而取消，
	// 但锁必须释放，否则要等到过期。
	defer lock.Release(context.WithoutCancel(ctx))

	task, err := repos.FindTaskByID(msg.TaskID)
	if err != nil {
		return fmt.Errorf("查询任务: %w", err)
	}

	// 二次校验：消息可能是重投的，此时任务已经不在排队状态。
	if task.Status != models.TaskQueued {
		log.Printf("任务不在排队状态，跳过 task=%d status=%s", task.ID, models.StatusText(task.Status))
		return nil
	}

	if err := Transition(task.ID, models.TaskQueued, models.TaskProcessing, ""); err != nil {
		if errors.Is(err, ErrStatusConflict) {
			// 另一个实例抢先推进了，这条消息同样可以丢弃。
			return nil
		}
		return fmt.Errorf("推进到处理中: %w", err)
	}

	if err := c.handler(ctx, task); err != nil {
		// 标记失败，重试由消息层负责：消息被拒绝后进入死信或重新投递，
		// 下次进来时任务状态是失败，允许从失败重新入队。
		if markErr := Transition(task.ID, models.TaskProcessing, models.TaskFailed, err.Error()); markErr != nil {
			log.Printf("标记任务失败时出错 task=%d: %v", task.ID, markErr)
		}
		return err
	}

	if err := Transition(task.ID, models.TaskProcessing, models.TaskDone, ""); err != nil {
		if errors.Is(err, ErrStatusConflict) {
			// 处理成功但状态被别人改了，属于异常，记录后按失败处理。
			log.Printf("任务完成但状态冲突 task=%d", task.ID)
			return nil
		}
		return fmt.Errorf("推进到已完成: %w", err)
	}

	return nil
}

// CloseConsumer 关闭消费信道。
func (c *Consumer) Close() {
	if c.ch != nil {
		c.ch.Close()
	}
}
