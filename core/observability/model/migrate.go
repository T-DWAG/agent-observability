package model

import "gorm.io/gorm"

// AutoMigrate 创建/更新 observability 相关表结构。
func AutoMigrate(db *gorm.DB) error {
	// 新增 (trace_id, dimension) 唯一索引前，保留每组最新记录，
	// 避免旧版本重复评估数据导致 AutoMigrate 创建索引失败。
	if db.Migrator().HasTable(&Evaluation{}) &&
		db.Migrator().HasColumn(&Evaluation{}, "trace_id") &&
		db.Migrator().HasColumn(&Evaluation{}, "dimension") {
		if err := db.Exec(`
			DELETE FROM obs_evaluations
			WHERE id NOT IN (
				SELECT MAX(id) FROM obs_evaluations GROUP BY trace_id, dimension
			)
		`).Error; err != nil {
			return err
		}
	}

	if err := db.AutoMigrate(&Span{}, &Trace{}, &Evaluation{}, &MetricSnapshot{}); err != nil {
		return err
	}
	// 兼容专题 4 初版的 Error 字段：迁移到明确映射的 error_msg。
	if db.Migrator().HasColumn(TableEvaluations, "error") {
		if err := db.Exec(`
			UPDATE obs_evaluations
			SET error_msg = "error"
			WHERE (error_msg IS NULL OR error_msg = '') AND "error" IS NOT NULL
		`).Error; err != nil {
			return err
		}
	}
	// 旧 Trace 的空租户回填为 default。
	return db.Exec(`UPDATE obs_traces SET tenant_id = 'default' WHERE tenant_id IS NULL OR tenant_id = ''`).Error
}
