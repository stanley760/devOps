//go:build wireinject

// Package app 装配 devops-id 的依赖图（DDD 各层 → gRPC 注册回调），
// 与 devops-system/internal/app 同构。
package app

import (
	"github.com/google/wire"

	idapp "devops-id/internal/application/idgen"
	idgrpc "devops-id/internal/interfaces/grpc"
)

var infraSet = wire.NewSet(
	provideEtcdClient,
	newTagInfraFactory,
)

var domainSet = wire.NewSet(
	newDomainConfig,
	provideManager,
)

var serviceSet = wire.NewSet(
	idapp.NewService,
	wire.Bind(new(idapp.Generator), new(*idapp.Service)),
)

var grpcSet = wire.NewSet(
	idgrpc.NewIdGenServer,
	newGRPCRegister,
)
