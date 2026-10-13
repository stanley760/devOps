//go:build wireinject

package app

import (
	"github.com/google/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"

	"github.com/stanley760/devOps/devops-id/internal/config"
)

// InitializeGRPC 装配发号服务依赖图：etcd client → per-tag 基础设施工厂 →
// snowflake Manager → 应用服务 → gRPC handler → 注册回调。
// 返回的 cleanup 逆序执行：revoke 所有 per-tag lease → 关闭 etcd client。
// 重新生成：go generate -tags=wireinject ./internal/app
//
//go:generate go run github.com/google/wire/cmd/wire
func InitializeGRPC(cfg config.Config) (func(*grpc.Server, *health.Server), func(), error) {
	wire.Build(infraSet, domainSet, serviceSet, grpcSet)
	return nil, nil, nil
}
