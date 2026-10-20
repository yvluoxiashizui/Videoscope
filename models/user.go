package models

import "time"

// 账户类型，用于区分资源配额。
const (
	AccountFree int8 = 0
	AccountPaid int8 = 1
)

// User 用户表。
type User struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Username string `gorm:"uniqueIndex;size:32;not null" json:"username"`

	// 密码只存 bcrypt 哈希，永远不序列化到响应里。
	Password string `gorm:"size:72;not null" json:"-"`

	// 0 免费用户，1 付费用户。阶段四的限流会按这个字段区分配额。
	AccountType int8 `gorm:"not null;default:0" json:"account_type"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
