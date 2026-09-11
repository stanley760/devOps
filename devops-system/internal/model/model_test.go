// P3 验收集成测试：BaseModel 发号 hook + idclient 预取缓冲端到端。
// 依赖本地 MySQL（devops 库）与 devops-id 服务（:9090），不可达时跳过。
package model_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	infracfg "devops-infra/config"
	"devops-infra/grpcx"

	"devops-id/pkg/idclient"

	"devops-system/internal/config"
	"devops-system/internal/db"
	"devops-system/internal/model"
)

// setup 加载配置 → 连接 MySQL → 装配发号客户端；任一依赖不可达则 Skip。
func setup(t *testing.T) {
	t.Helper()

	if err := infracfg.Init("../../config/config.yaml"); err != nil {
		t.Skipf("加载配置失败，跳过集成测试: %v", err)
	}
	var cfg config.Config
	if err := infracfg.LoadInto(&cfg); err != nil {
		t.Skipf("解析配置失败，跳过集成测试: %v", err)
	}
	if err := db.Init(cfg.Mysql); err != nil {
		t.Skipf("MySQL 不可达，跳过集成测试: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := grpcx.Dial(ctx, cfg.IDClient.Endpoint, 2*time.Second)
	if err != nil {
		t.Skipf("devops-id 拨号失败，跳过集成测试: %v", err)
	}
	idc := idclient.New(conn, idclient.BufferConfig{
		Batch:       100,
		LowWater:    0.2,
		CallTimeout: 3 * time.Second,
	})
	if err := idc.Ping(ctx); err != nil {
		t.Skipf("devops-id 健康检查失败，跳过集成测试: %v", err)
	}
	model.SetIDGenerator(idc)
	t.Cleanup(func() { _ = conn.Close() })
}

// TestBeforeCreateGeneratesID 创建用户 → 主键由 devops-id 发号（ID 批次递增）。
func TestBeforeCreateGeneratesID(t *testing.T) {
	setup(t)
	gdb := db.Get()

	ts := time.Now().UnixNano()
	u1 := &model.User{
		BaseTenantModel: model.BaseTenantModel{TenantID: 1},
		Username:        fmt.Sprintf("it_seq_%d_a", ts),
		Password:        "$2a$10$placeholder",
		Nickname:        "集成测试A",
		Status:          0,
	}
	u2 := &model.User{
		BaseTenantModel: model.BaseTenantModel{TenantID: 1},
		Username:        fmt.Sprintf("it_seq_%d_b", ts),
		Password:        "$2a$10$placeholder",
		Nickname:        "集成测试B",
		Status:          0,
	}
	t.Cleanup(func() {
		gdb.Unscoped().Where("username LIKE ?", fmt.Sprintf("it_seq_%d%%", ts)).Delete(&model.User{})
	})

	if err := gdb.Create(u1).Error; err != nil {
		t.Fatalf("创建用户A失败: %v", err)
	}
	if err := gdb.Create(u2).Error; err != nil {
		t.Fatalf("创建用户B失败: %v", err)
	}

	if u1.ID <= 0 {
		t.Fatalf("用户A 未获得发号: %d", u1.ID)
	}
	// 同一批预取缓冲内：批次递增（1001、1002 型连续或跨批递增）
	if u2.ID <= u1.ID {
		t.Fatalf("ID 未递增: u1=%d u2=%d", u1.ID, u2.ID)
	}
	t.Logf("发号成功: u1=%d u2=%d", u1.ID, u2.ID)
}

// TestBizTagOf 表名 → biz_tag 映射。
func TestBizTagOf(t *testing.T) {
	cases := []struct {
		table string
		tag   string
		ok    bool
	}{
		{"t_system_users", "user", true},
		{"t_system_role", "role", true},
		{"t_system_menu", "menu", true},
		{"t_system_dept", "dept", true},
		{"t_system_post", "post", true},
		{"t_tenant", "tenant", true},
		{"t_system_user_role", "", false}, // 关联表不走发号，无映射
		{"t_leaf_alloc", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		tag, err := model.BizTagOf(c.table)
		if c.ok && (err != nil || tag != c.tag) {
			t.Errorf("BizTagOf(%q) = %q, %v; want %q, nil", c.table, tag, err, c.tag)
		}
		if !c.ok && err == nil {
			t.Errorf("BizTagOf(%q) 应返回错误，得到 %q", c.table, tag)
		}
	}
}
