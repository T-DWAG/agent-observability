package model

import "gorm.io/gorm"

// AutoMigrate 创建/更新 observability 相关表结构。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&Span{},
		&Trace{},
		&Evaluation{},
	)
}
