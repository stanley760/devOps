// P3 验收集成测试：BaseModel 发号 hook + idclient 预取缓冲端到端。
// 依赖本地 MySQL（devops 库）与 devops-id 服务（:9090），不可达时跳过。
package model_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	infracfg "devops-infra/config"
	grpcinfra "devops-infra/grpc"

	"devops-id/pkg/idclient"
	client "devops-infra/database/mysql"
	"devops-system/internal/config"
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
	if err := client.Init(cfg.Mysql); err != nil {
		t.Skipf("MySQL 不可达，跳过集成测试: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := grpcinfra.Dial(ctx, cfg.IDClient.Endpoint, 2*time.Second)
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
	gdb := client.Get()

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

// TestBizTagOf 模型类型 → biz_tag 推导。
func TestBizTagOf(t *testing.T) {
	cases := []struct {
		model any
		tag   string
	}{
		{&model.User{}, "user"},
		{&model.Role{}, "role"},
		{&model.Menu{}, "menu"},
		{&model.Dept{}, "dept"},
		{&model.Post{}, "post"},
		{&model.Tenant{}, "tenant"},
		{&model.LoginLog{}, "login_log"},
	}
	for _, c := range cases {
		tag, err := model.BizTagOf(c.model)
		if err != nil || tag != c.tag {
			t.Errorf("BizTagOf(%T) = %q, %v; want %q, nil", c.model, tag, err, c.tag)
		}
	}
}
