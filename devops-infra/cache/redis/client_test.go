package redis

import (
	"context"
	"testing"
)

func TestNilClientReturnsError(t *testing.T) {
	var client *Client
	ctx := context.Background()

	if _, err := client.Get(ctx, "key"); err == nil {
		t.Fatal("Get() 应返回初始化错误")
	}
	if err := client.Set(ctx, "key", "value", 0); err == nil {
		t.Fatal("Set() 应返回初始化错误")
	}
	if err := client.Delete(ctx, "key"); err == nil {
		t.Fatal("Delete() 应返回初始化错误")
	}
	if _, err := client.Exists(ctx, "key"); err == nil {
		t.Fatal("Exists() 应返回初始化错误")
	}
	if err := client.Ping(ctx); err == nil {
		t.Fatal("Ping() 应返回初始化错误")
	}
}
