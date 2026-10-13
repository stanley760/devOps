// 集成测试：验证 idclient 预取缓冲 + 服务端发号链路。
// 依赖本地 devops-id 服务（:9990），
// 跳过条件：连接失败（服务未启动）时 t.Skip。
package idclient_test

import (
	"context"
	"github.com/stanley760/devOps/devops-id/pkg/idclient"
	grpcinfra "github.com/stanley760/devOps/devops-infra/grpc"
	"testing"
	"time"
)

// dial 建立客户端连接并探活：gRPC 懒连接，dial 不触发真实连接，
// 故以 Ping 失败（服务未启动）作为跳过条件。
func dial(t *testing.T) *idclient.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := grpcinfra.Dial(ctx, "127.0.0.1:9990", 2*time.Second)
	if err != nil {
		t.Skipf("devops-id 服务未启动，跳过集成测试: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	c := idclient.New(conn, idclient.BufferConfig{
		Batch:       10,
		LowWater:    0.2,
		CallTimeout: 2 * time.Second,
	})
	if err := c.Ping(ctx); err != nil {
		t.Skipf("devops-id 服务未启动，跳过集成测试: %v", err)
	}
	return c
}

func TestNextSequential(t *testing.T) {
	c := dial(t)
	ctx := context.Background()

	var prev int64
	for i := 0; i < 50; i++ {
		id, err := c.Next(ctx, "user")
		if err != nil {
			t.Fatalf("第 %d 次发号失败: %v", i, err)
		}
		if id <= prev {
			t.Fatalf("ID 未递增: prev=%d cur=%d", prev, id)
		}
		prev = id
	}
	t.Logf("user worker 50 个 ID 连续递增，末值 %d", prev)
}

func TestMultiTagIsolation(t *testing.T) {
	c := dial(t)
	ctx := context.Background()

	u1, err := c.Next(ctx, "user")
	if err != nil {
		t.Fatal(err)
	}
	r1, err := c.Next(ctx, "role")
	if err != nil {
		t.Fatal(err)
	}
	// 不同 biz_tag 拥有独立 worker 空间，允许重号，只验证可发号
	t.Logf("user=%d role=%d（不同 biz_tag 独立 worker 空间）", u1, r1)
}
