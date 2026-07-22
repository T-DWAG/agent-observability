package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/T-Dwag/agent-observability/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PostgresStorage struct {
	db *gorm.DB
}

func NewPostgresStorage(db *gorm.DB) *PostgresStorage {
	return &PostgresStorage{db: db}
}

func OpenPostgres(dsn string) (*gorm.DB, error) {
	//根据dsn创建数据库连接
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres database: %w", err)
	}
	return db, nil
}

// SaveTrace：把一条 Trace 存进数据库。
// 简单说：同一个 trace_id 存两次也没事——第一次是新增，第二次是覆盖更新，不会报“重复”错误。
func (p *PostgresStorage) SaveTrace(ctx context.Context, trace *model.Trace) error {
	err := p.db.WithContext(ctx).
		// Clauses 用来告诉数据库：“插入时如果撞车了，该怎么办”。
		// 这里的 OnConflict 意思是：
		//   1. 先尝试插入这条 Trace
		//   2. 如果库里已经有相同的 trace_id（撞车了）
		//   3. 那就别报错，直接用新数据把旧那一行全部改掉
		// 效果：反复调用 SaveTrace 保存同一条 Trace，结果都一样，不会因为重复插入而失败。
		Clauses(clause.OnConflict{
			// 用哪一列判断“是不是同一条”：看 trace_id
			Columns: []clause.Column{{Name: "trace_id"}},
			// 撞车后怎么处理：把这一行的所有字段都更新成新值
			UpdateAll: true,
		}).
		Create(trace).Error
	if err != nil {
		return fmt.Errorf("save trace: %w", err)
	}
	return nil
}

func (p *PostgresStorage) GetTrace(ctx context.Context, traceID string) (*model.Trace, error) {
	var tr model.Trace
	err := p.db.WithContext(ctx).Where("trace_id = ?", traceID).First(&tr).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrorNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get trace: %w", err)
	}
	return &tr, nil
}

func (p *PostgresStorage) GetTraceSpans(ctx context.Context, traceID string) ([]*model.Span, error) {
	var spans []*model.Span
	err := p.db.WithContext(ctx).Where("trace_id =?", traceID).
		Order("start_time ASC").Find(&spans).Error

	if err != nil {
		return nil, fmt.Errorf("get trace spans: %w", err)
	}
	return spans, nil
}

// ListTraces：按条件分页查询 Trace 列表，同时返回符合条件的总数（方便前端做分页）。
// 流程：先 normalize 过滤条件 → 拼 WHERE → Count 总数 → 再按时间倒序 Offset/Limit 取当前页。
func (p *PostgresStorage) ListTraces(ctx context.Context, filter TraceFilter) ([]*model.Trace, int64, error) {
	// 补齐默认分页参数（比如 Page/Size 非法时落到合理值），避免后面 Offset/Limit 算出负数或 0
	filter = filter.normalize()
	q := p.db.WithContext(ctx).Model(&model.Trace{})

	// 按需叠加过滤条件：有值才加 WHERE，空值表示“不限”
	if filter.SessionID != "" {
		q = q.Where("session_id = ?", filter.SessionID)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if !filter.StartTime.IsZero() {
		// 起始时间：Trace 的 start_time 不早于该时刻
		q = q.Where("start_time >= ?", filter.StartTime)
	}
	if !filter.EndTime.IsZero() {
		// 结束时间：Trace 的 start_time 不晚于该时刻（按开始时间落在区间内筛选）
		q = q.Where("start_time <= ?", filter.EndTime)
	}

	// 先 Count：拿到过滤后的总条数（分页 UI 需要），注意 Count 要在 Offset/Limit 之前做
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count traces: %w", err)
	}

	// 再查当前页数据：最新的 Trace 排前面，用 offset/size 做分页切片
	var traces []*model.Trace
	err := q.Order("start_time desc").
		Offset(filter.offset()).
		Limit(filter.Size).
		Find(&traces).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list traces: %w", err)
	}
	return traces, total, nil
}

func (p *PostgresStorage) SaveSpan(ctx context.Context, span *model.Span) error {
	err := p.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "span_id"}},
			UpdateAll: true,
		}).
		Create(span).Error
	if err != nil {
		return fmt.Errorf("save span: %w", err)
	}
	return nil
}

func (p *PostgresStorage) SaveEvaluation(ctx context.Context, eval *model.Evaluation) error {
	if err := p.db.WithContext(ctx).Create(eval).Error; err != nil {
		return fmt.Errorf("save evaluation: %w", err)
	}
	return nil
}

func (p *PostgresStorage) ListEvaluations(ctx context.Context, traceID string) ([]*model.Evaluation, error) {
	var list []*model.Evaluation
	err := p.db.WithContext(ctx).
		Where("trace_id = ?", traceID).
		Order("created_at asc").
		Find(&list).Error

	if err != nil {
		return nil, fmt.Errorf("list evaluations: %w", err)
	}
	return list, nil
}
