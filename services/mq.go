package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// 队列与交换机名称。
//
// 主队列直接绑定默认交换机，routing key 就是队列名——
// 这个场景只需要点对点投递，不需要主题路由，
// 多一层自定义 exchange 只会增加理解成本。
const (
	AnalyzeQueue  = "videoscope.analyze"
	AnalyzeDLQ    = "videoscope.analyze.dlq"
	AnalyzeDLX    = "videoscope.dlx"
	AnalyzeDLXKey = "analyze"
)

var (
	mqConn    *amqp.Connection
	mqChannel *amqp.Channel
)

// AnalyzeMessage 是投递给分析消费者的消息体。
//
// 只带定位任务所需的最小信息，不带转写文本或模型结果：
// 消息体积越小，broker 的存储与传输压力越低；
// 而且任务数据以数据库为准，消息里冗余一份会有不一致风险。
type AnalyzeMessage struct {
	TaskID    uint   `json:"task_id"`
	UserID    uint   `json:"user_id"`
	VideoMD5  string `json:"video_md5"`
	ObjectKey string `json:"object_key"`
}

// InitMQ 建立连接并声明队列与死信链路。
func InitMQ(url string) error {
	conn, err := amqp.Dial(url)
	if err != nil {
		return fmt.Errorf("连接 RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("打开信道: %w", err)
	}

	if err := declareTopology(ch); err != nil {
		ch.Close()
		conn.Close()
		return err
	}

	mqConn = conn
	mqChannel = ch
	return nil
}

// declareTopology 声明死信链路与主队列。
//
// 主队列通过参数指定死信交换机：消息被拒绝且不重新入队时，
// broker 自动把它转到死信交换机，再由死信队列接住。
// 这条链路是「三次重试后转人工」的落点。
func declareTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(AnalyzeDLX, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("声明死信交换机: %w", err)
	}
	if _, err := ch.QueueDeclare(AnalyzeDLQ, true, false, false, false, nil); err != nil {
		return fmt.Errorf("声明死信队列: %w", err)
	}
	if err := ch.QueueBind(AnalyzeDLQ, AnalyzeDLXKey, AnalyzeDLX, false, nil); err != nil {
		return fmt.Errorf("绑定死信队列: %w", err)
	}

	args := amqp.Table{
		"x-dead-letter-exchange":    AnalyzeDLX,
		"x-dead-letter-routing-key": AnalyzeDLXKey,
	}
	if _, err := ch.QueueDeclare(AnalyzeQueue, true, false, false, false, args); err != nil {
		return fmt.Errorf("声明分析队列: %w", err)
	}
	return nil
}

// PublishAnalyze 投递一条分析任务。
//
// 用同步确认模式发送：只有 broker 确认接收后才返回成功。
// 异步发送省那几毫秒，但消息发丢时前端已经拿到「已提交」的响应，
// 用户会一直等一个永远不会开始的任务。
func PublishAnalyze(ctx context.Context, msg AnalyzeMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("序列化消息: %w", err)
	}

	// 开启确认模式，配合下面的 NotifyPublish 等待 broker 回执。
	if err := mqChannel.Confirm(false); err != nil {
		return fmt.Errorf("开启发布确认: %w", err)
	}
	confirms := mqChannel.NotifyPublish(make(chan amqp.Confirmation, 1))

	err = mqChannel.PublishWithContext(ctx,
		"",
		AnalyzeQueue,
		false, // mandatory：没有匹配队列时退回而不是静默丢弃
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent, // 持久化，broker 重启后消息还在
			MessageId:    fmt.Sprintf("analyze-%d", msg.TaskID),
			Timestamp:    time.Now(),
			Body:         body,
		},
	)
	if err != nil {
		return fmt.Errorf("投递消息: %w", err)
	}

	select {
	case c, ok := <-confirms:
		if !ok {
			return fmt.Errorf("确认通道已关闭")
		}
		if !c.Ack {
			return fmt.Errorf("broker 拒绝接收消息")
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("等待确认超时: %w", ctx.Err())
	}
}

// CloseMQ 关闭连接，进程退出前调用。
func CloseMQ() {
	if mqChannel != nil {
		mqChannel.Close()
	}
	if mqConn != nil {
		mqConn.Close()
	}
}
