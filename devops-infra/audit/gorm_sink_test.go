package audit

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/stanley760/devOps/devops-common/audit"
	"github.com/stanley760/devOps/devops-infra/database"
)

// row 是测试用的审计表实体，gorm tag 与真实业务表同构（列宽对齐契约常量）。
type row struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	TraceID   string `gorm:"column:trace_id;size:100"`
	Username  string `gorm:"column:username;size:30"`
	UserIP    string `gorm:"column:user_ip;size:50"`
	UserAgent string `gorm:"column:user_agent;size:512"`
	Module    string `gorm:"column:module;size:50"`
	Result    int8   `gorm:"column:result"`
}

func (row) TableName() string { return "t_test_audit" }

// convert 与真实业务用法同构：返回指针，GORM 才能回写自增主键。
func convert(rec audit.Record) *row {
	return &row{
		TraceID:   rec.TraceID,
		Username:  rec.Username,
		UserIP:    rec.UserIP,
		UserAgent: rec.UserAgent,
		Module:    rec.Module,
		Result:    rec.Result,
	}
}

// gormExecutor 把 *gorm.DB 适配为 ctx 感知的 Executor（测试替身，
// 生产实现见各业务包的同名适配器）。
type gormExecutor struct{ db *gorm.DB }

func (g gormExecutor) Create(ctx context.Context, value any) *gorm.DB {
	return g.db.WithContext(ctx).Create(value)
}

// newDB 为每个用例建独立的内存库并建表。
// 不使用 file::memory:?cache=shared：共享缓存会让并行/连续用例互相阻塞，
// 且 t.Cleanup 删表与下一个用例的建表产生交叉。
func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=private"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	// 内存库随连接释放而消失，显式关连接避免句柄泄漏。
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.AutoMigrate(&row{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestGORSinkWritesRecord(t *testing.T) {
	db := newDB(t)
	sink := NewGORSink(GORSinkConfig[*row]{Executor: gormExecutor{db}, Convert: convert})

	sink.Emit(t.Context(), audit.Record{
		Module:   "system",
		SubModule: "user",
		Username: "alice",
		UserIP:   "10.0.0.1",
		Result:   audit.ResultSuccess,
	})

	var got row
	if err := db.First(&got).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Module != "system" || got.Username != "alice" {
		t.Fatalf("unexpected row: %+v", got)
	}
}

// TestGORSinkClipsBeforeHittingDatabase 验证截断发生在送入数据库之前：
// MySQL 严格模式对超长 varchar 会整行拒绝，丢审计行比截断更不可接受。
func TestGORSinkClipsBeforeHittingDatabase(t *testing.T) {
	db := newDB(t)

	// 捕获送进GORM 的实体：用一个包装 Executor 记录 convert 的输出。
	spy := &capturingExecutor{inner: gormExecutor{db}}
	sink := NewGORSink(GORSinkConfig[*row]{Executor: spy, Convert: convert})

// 超长输入必须超过所有列宽上限（MaxIP=50 / MaxModule=50 /
	// MaxUsername=30 / MaxUserAgent=512 / MaxTraceID=100），否则"不截断"才是正确行为。
	long := strings.Repeat("超长内容", 200) // 800 字符
	sink.Emit(t.Context(), audit.Record{
		Username:  long,
		UserIP:    long,
		UserAgent: long,
		TraceID:   long,
		Module:    long,
	})

	got := spy.snap
	if got == nil {
		t.Fatal("Convert did not produce a row")
	}
	checks := []struct {
		name  string
		value string
		limit int
	}{
		{"Username", got.Username, audit.MaxUsername},
		{"UserIP", got.UserIP, audit.MaxIP},
		{"UserAgent", got.UserAgent, audit.MaxUserAgent},
		{"TraceID", got.TraceID, audit.MaxTraceID},
		{"Module", got.Module, audit.MaxModule},
	}
	for _, c := range checks {
		if r := len([]rune(c.value)); r != c.limit {
			t.Fatalf("%s runes = %d, want clipped to %d before hitting DB", c.name, r, c.limit)
		}
		if !utf8.ValidString(c.value) {
			t.Fatalf("%s was truncated by byte and produced invalid UTF-8", c.name)
		}
	}
}

// capturingExecutor 在透传给真实执行器之前，快照送入 Create 的行。
//
// 必须快照而非持有指针：GORM 会在 Create 后回写字段（SQLite 还会按列宽
// 静默截断），事后读同一指针看到的已不是 Sink 交给数据库的值。
type capturingExecutor struct {
	inner Executor
	snap  *row
}

func (c *capturingExecutor) Create(ctx context.Context, value any) *gorm.DB {
	if r, ok := value.(*row); ok {
		snapshot := *r
		c.snap = &snapshot
	}
	return c.inner.Create(ctx, value)
}

func TestGORSinkSkipsWhenContextCancelled(t *testing.T) {
	db := newDB(t)
	sink := NewGORSink(GORSinkConfig[*row]{Executor: gormExecutor{db}, Convert: convert})

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // 模拟业务失败回滚 / 请求超时

	// 不 panic、不阻断：Emit 契约上无返回值。
	sink.Emit(ctx, audit.Record{Module: "system", Username: "alice"})

	var n int64
	if err := db.Model(&row{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("cancelled context should not write, got %d rows", n)
	}
}

func TestGORSinkWriteFailureDoesNotPanic(t *testing.T) {
	db := newDB(t)
	// 写入必然失败的执行器（表不存在）。
	bad := db.Table("t_does_not_exist")
	sink := NewGORSink(GORSinkConfig[*row]{Executor: gormExecutor{bad}, Convert: convert})

	// 关键：Emit 不返回错误也不 panic —— 审计是旁路。
	sink.Emit(t.Context(), audit.Record{Module: "system", Username: "alice"})
}

func TestGORSinkIsNoopWhenNotConfigured(t *testing.T) {
	// 零值/半配置状态必须安全：业务代码无需到处判 nil。
	var nilSink *GORSink[*row]
	nilSink.Emit(t.Context(), audit.Record{Module: "system"})

	NewGORSink(GORSinkConfig[*row]{Convert: convert}).Emit(t.Context(), audit.Record{})
	NewGORSink(GORSinkConfig[*row]{Executor: gormExecutor{newDB(t)}}).Emit(t.Context(), audit.Record{})
}

func TestGORSinkHandlesNilContext(t *testing.T) {
	db := newDB(t)
	sink := NewGORSink(GORSinkConfig[*row]{Executor: gormExecutor{db}, Convert: convert})

	//nolint:staticcheck // 刻意验证 nil ctx 不 panic
	sink.Emit(nil, audit.Record{Module: "system", Username: "alice"})

	var n int64
	if err := db.Model(&row{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("nil context should fall back to Background and write, got %d rows", n)
	}
}

func TestExecutorOfPrefersTransaction(t *testing.T) {
	db := newDB(t)

	// 无事务时回退到默认执行器。
	if got := ExecutorOf(t.Context(), gormExecutor{db}); got == nil {
		t.Fatal("ExecutorOf returned nil without transaction")
	}

	// 有事务时 ctx 中注入了事务执行器，ExecutorOf 应可取到。
	if err := database.Transaction(t.Context(), db, func(ctx context.Context) error {
		if _, ok := database.ExecutorFromContext(ctx); !ok {
			t.Error("transaction not injected into ctx")
		}
		if got := ExecutorOf(ctx, gormExecutor{db}); got == nil {
			t.Error("ExecutorOf returned nil inside transaction")
		}
		return nil
	}); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func TestScopedBypassesSoftDelete(t *testing.T) {
	db := newDB(t)
	// 本测试实体无 DeletedAt 字段，Scoped 只是保证查询侧有统一的 Unscoped 入口。
	q := Scoped(db.Model(&row{}))
	if q == nil {
		t.Fatal("Scoped returned nil")
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		t.Fatalf("Scoped chain must stay usable: %v", err)
	}
}

var _ audit.Sink = NewGORSink(GORSinkConfig[*row]{})

// 保证 DefaultWriteTimeout 为正，防止零值退化为无超时。
func TestDefaultWriteTimeoutIsPositive(t *testing.T) {
	if DefaultWriteTimeout <= 0 {
		t.Fatalf("DefaultWriteTimeout = %v, want positive", DefaultWriteTimeout)
	}
	if DefaultWriteTimeout > 30*time.Second {
		t.Fatalf("DefaultWriteTimeout = %v, too long for a side-channel write", DefaultWriteTimeout)
	}
}