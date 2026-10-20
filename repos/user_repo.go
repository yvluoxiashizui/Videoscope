package repos

import "videoscope/models"

// CreateUser 插入用户。用户名重复时由唯一索引返回冲突错误，调用方据此判断重复注册。
func CreateUser(u *models.User) error {
	return db.Create(u).Error
}

// FindUserByName 按用户名查询，未找到时返回 gorm.ErrRecordNotFound。
func FindUserByName(name string) (*models.User, error) {
	var u models.User
	if err := db.Where("username = ?", name).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// FindUserByID 按主键查询。
func FindUserByID(id uint) (*models.User, error) {
	var u models.User
	if err := db.Where("id = ?", id).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}
