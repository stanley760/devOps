package model_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/stanley760/devOps/devops-system/internal/auditctx"
	"github.com/stanley760/devOps/devops-system/internal/model"
)

func TestBaseModelAuditHooks(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:model_audit_hooks?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}

	ctx := auditctx.WithActor(context.Background(), auditctx.Actor{Username: "alice"})
	user := &model.User{
		BaseTenantModel: model.BaseTenantModel{
			BaseModel: model.BaseModel{ID: 1, Creator: "untrusted", Updater: "untrusted"},
			TenantID:  7,
		},
		Username: "audit-user",
		Password: "hashed",
		Nickname: "Audit User",
	}
	if err := db.WithContext(ctx).Create(user).Error; err != nil {
		t.Fatal(err)
	}
	if user.Creator != "alice" || user.Updater != "alice" {
		t.Fatalf("created audit fields = (%q, %q), want (alice, alice)", user.Creator, user.Updater)
	}

	updateCtx := auditctx.WithActor(context.Background(), auditctx.Actor{Username: "bob"})
	if err := db.WithContext(updateCtx).Model(&model.User{}).
		Where("id = ?", user.ID).
		Updates(map[string]any{"nickname": "Updated", "updater": "untrusted"}).Error; err != nil {
		t.Fatal(err)
	}

	var updated model.User
	if err := db.First(&updated, user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Creator != "alice" {
		t.Errorf("creator after update = %q, want alice", updated.Creator)
	}
	if updated.Updater != "bob" {
		t.Errorf("updater after update = %q, want bob", updated.Updater)
	}
}

func TestBaseModelAuditHooksPreserveSystemValuesWithoutActor(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:model_audit_system?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}

	user := &model.User{
		BaseTenantModel: model.BaseTenantModel{
			BaseModel: model.BaseModel{ID: 1, Creator: "system", Updater: "system"},
			TenantID:  7,
		},
		Username: "system-user",
		Password: "hashed",
		Nickname: "System User",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	if user.Creator != "system" || user.Updater != "system" {
		t.Fatalf("system audit fields = (%q, %q), want (system, system)", user.Creator, user.Updater)
	}
}
