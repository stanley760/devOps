// Package audit 提供操作审计的 GORM 本地落库实现。
//
// 本包是 devops-common/audit 的一个 Sink 实现：契约（记录什么、列宽多少、
// 用户类型枚举）全在 common 层，本包只负责把契约映射到业务表并写入。
//
// 审计是旁路能力，本包严格遵守三条约束：
//  1. 写入失败仅 slog.Warn，绝不阻断业务主链路（Emit 不返回 error）；
//  2. 落库前按契约列宽截断，杜绝超长字段在 MySQL 严格模式下导致整行被拒；
//  3. 审计行不做软删——查询侧删除走 Unscoped 物理删除，避免 deleted_at 行让表膨胀。
package audit

import (
	"context"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/stanley760/devOps/devops-common/audit"
	"github.com/stanley760/devOps/devops-infra/database"
)

// Executor 审计写入所需的数据库执行器。
//
// 接收 ctx 是必需的：多租户场景下连接要按 ctx 解析租户（也可能复用 ctx 中
// 已开启的事务）。*gorm.DB 无法直接满足，实现方用一个小适配器包一层即可。
type Executor interface {
	Create(ctx context.Context, value any) *gorm.DB
}

// GORSink 把审计记录写入本地业务库的 Sink 实现。
//
// 泛型参数 T 为审计表实体类型，由业务侧提供 convert 映射函数，
// 这样 infra 不必知道 t_system_operation_log 的具体列名与 gorm tag。
type GORSink[T any] struct {
	executor Executor
	convert  func(audit.Record) T

	// timeout 单条审计写入的超时上限。
	// 审计是旁路，不应因数据库慢查询而堆积 goroutine。
	timeout time.Duration
}

// GORSinkConfig 装配参数。
type GORSinkConfig[T any] struct {
	// Executor 数据库执行器；必填。
	Executor Executor
	// Convert Record → 实体的映射；必填。
	//
	// 必须返回指针（如 func(rec audit.Record) *model.OperationLog），
	// 否则 GORM 无法回写主键，Create 阶段会 panic。
	Convert func(audit.Record) T
	// Timeout 单条写入超时；0 → DefaultWriteTimeout。
	Timeout time.Duration
}

// DefaultWriteTimeout 单条审计写入的默认超时。
const DefaultWriteTimeout = 3 * time.Second

// NewGORSink 创建本地落库 Sink。
func NewGORSink[T any](cfg GORSinkConfig[T]) *GORSink[T] {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultWriteTimeout
	}
	return &GORSink[T]{
		executor: cfg.Executor,
		convert:  cfg.Convert,
		timeout:  timeout,
	}
}

// Emit 实现 audit.Sink。
//
// 不返回错误：审计失败只记日志，绝不影响业务主链路。
func (s *GORSink[T]) Emit(ctx context.Context, rec audit.Record) {
	if s == nil || s.executor == nil || s.convert == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// 上下文已取消（业务失败、超时或事务回滚）时不再写入：
	// 落进即将失效的事务没有意义，只会拖慢失败路径。
	if err := ctx.Err(); err != nil {
		slog.WarnContext(ctx, "审计跳过：上下文已取消",
			"module", rec.Module, "subModule", rec.SubModule, "err", err)
		return
	}

	rec = rec.Normalize()
	row := s.convert(rec)

	writeCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	if err := s.executor.Create(writeCtx, row).Error; err != nil {
		slog.WarnContext(ctx, "审计落库失败",
			"module", rec.Module,
			"subModule", rec.SubModule,
			"url", rec.RequestURL,
			"err", err,
		)
		return
	}
	slog.DebugContext(ctx, "审计已记录",
		"module", rec.Module,
		"subModule", rec.SubModule,
		"bizID", rec.BizID,
		"result", rec.Result,
	)
}

// ExecutorOf 从 ctx 中取出事务执行器，否则回退到给定的默认执行器。
//
// 审计表与业务表同库时，优先复用业务事务可避免"业务成功但审计丢失"；
// ctx 无事务时用默认连接池写入（审计容许这种弱一致）。
func ExecutorOf(ctx context.Context, fallback Executor) Executor {
	if tx, ok := database.ExecutorFromContext(ctx); ok && tx != nil {
		return txExecutor{tx: tx}
	}
	return fallback
}

// txExecutor 把 *gorm.DB 适配为 ctx 感知的 Executor（忽略传入的 ctx，
// 因为事务句柄本身已绑定更外层的 ctx）。
type txExecutor struct{ tx *gorm.DB }

func (t txExecutor) Create(_ context.Context, value any) *gorm.DB {
	return t.tx.Create(value)
}

// Scoped 审计查询的统一作用域：Unscoped 绕过软删。
//
// 审计行没有软删语义，查询与删除都应看到全部历史行；
// 各业务查询侧复用此入口，避免各处遗漏 Unscoped。
func Scoped(db *gorm.DB) *gorm.DB { return db.Unscoped() }

// 编译期断言：GORSink 必须满足 common 层契约。
var _ audit.Sink = (*GORSink[any])(nil)