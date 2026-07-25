package model

import "gorm.io/gorm"

// AutoMigrate 创建/更新 observability 相关表结构。
func AutoMigrate(db *gorm.DB) error {
	// 	return db.AutoMigrate(
	// 		&Span{},
	// 		&Trace{},
	// 		&Evaluation{},
	// 	)
	// }
	// 1. 根据结构体创建/更新表（不是执行业务 SQL）
	if err := db.AutoMigrate(&Span{}, &Trace{}, &Evaluation{}); err != nil {
		return err
	}
	// 2. 再跑一条数据回填 SQL：把旧数据里「空租户」补成 default
	//    （NULL / '' → 'default'，不是设为空）
	return db.Exec(`UPDATE obs_traces SET tenant_id = 'default' WHERE tenant_id IS NULL OR tenant_id = ''`).Error
}
