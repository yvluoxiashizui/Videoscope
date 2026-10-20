package repos

import "gorm.io/gorm"

// db 全局数据库句柄，由 main 注入。
// 这样各层只依赖 repos 的包级函数，不把 *gorm.DB 一路传递下去。
var db *gorm.DB

// SetDB 注入数据库连接。
func SetDB(d *gorm.DB) {
	db = d
}
