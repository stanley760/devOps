package database

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"gorm.io/gorm"
)

type fakeExecutor struct {
	tx *gorm.DB
}

func (e fakeExecutor) Transaction(fn func(tx *gorm.DB) error, _ ...*sql.TxOptions) error {
	return fn(e.tx)
}

func TestTransactionInjectsExecutor(t *testing.T) {
	ctx := context.Background()
	tx := &gorm.DB{
		Config:    &gorm.Config{},
		Statement: &gorm.Statement{Context: ctx},
	}

	err := Transaction(ctx, fakeExecutor{tx: tx}, func(txCtx context.Context) error {
		got, ok := ExecutorFromContext(txCtx)
		if !ok || got == nil {
			t.Fatal("事务未注入 context")
		}
		if got.Statement.Context != ctx {
			t.Fatal("事务执行器未继承原始 context")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("事务执行失败: %v", err)
	}
}

func TestTransactionNilArguments(t *testing.T) {
	if err := Transaction(context.Background(), nil, func(context.Context) error { return nil }); err == nil {
		t.Fatal("executor 为 nil 时应返回错误")
	}
	if err := Transaction(context.Background(), fakeExecutor{}, nil); err == nil {
		t.Fatal("事务函数为 nil 时应返回错误")
	}
}

func TestTransactionPropagatesError(t *testing.T) {
	want := errors.New("rollback")
	tx := &gorm.DB{Config: &gorm.Config{}, Statement: &gorm.Statement{Context: context.Background()}}
	err := Transaction(context.Background(), fakeExecutor{tx: tx}, func(context.Context) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
