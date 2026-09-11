// 集成测试：验证 idclient 预取缓冲 + 服务端发号链路。
// 依赖本地 devops-id 服务（:9090）与已注册的 t_leaf_alloc 行，
// 跳过条件：连接失败（服务未启动）时 t.Skip。
package idclient_test

import (
	"context"
	"testing"
	"time"

	"devops-infra/grpcx"

	"devops-id/pkg/idclient"
)

func dial(t *testing.T) *idclient.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := grpcx.Dial(ctx, "127.0.0.1:9090", 2*time.Second)
	if err != nil {
		t.Skipf("devops-id 服务未启动，跳过集成测试: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return idclient.New(conn, idclient.BufferConfig{
		Batch:       10,
		LowWater:    0.2,
		CallTimeout: 2 * time.Second,
	})
}

func TestNextSequential(t *testing.T) {
	c := dial(t)
	ctx := context.Background()

	if err := c.Ping(ctx); err != nil {
		t.Fatalf("健康检查失败: %v", err)
	}

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
	t.Logf("user 号段 50 个 ID 连续递增，末值 %d", prev)
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
	// 不同 biz_tag 号段独立，值域不应保证大小关系，只验证可发号
	t.Logf("user=%d role=%d（不同 biz_tag 独立号段）", u1, r1)
}
