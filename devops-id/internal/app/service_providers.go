package app

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	etcdinfra "devops-infra/etcd"

	"devops-id/internal/config"
	iddomain "devops-id/internal/domain/idgen"
	"devops-id/internal/infrastructure/etcdcoord"
	idgrpc "devops-id/internal/interfaces/grpc"
	"devops-id/pkg/idgenpb"
)

// provideEtcdClient 构造 etcd client；cleanup 负责关闭连接。
func provideEtcdClient(cfg config.Config) (*etcdinfra.Client, func(), error) {
	cli, err := etcdinfra.New(cfg.Etcd)
	if err != nil {
		return nil, nil, err
	}
	return cli, func() { _ = cli.Close() }, nil
}

// newTagInfraFactory 按 bizTag 构造领域基础设施（lease / 槽位协调 / 磁盘缓存）。
func newTagInfraFactory(cli *etcdinfra.Client, cfg config.Config) iddomain.TagInfraFactory {
	return etcdcoord.NewInfraFactory(cli, cfg.IDGen.LeafName, cfg.Etcd.LeaseTTL, cfg.IDGen.WorkerCache)
}

// newDomainConfig 从服务配置提取领域配置。
func newDomainConfig(cfg config.Config) iddomain.Config {
	return iddomain.Config{LeafName: cfg.IDGen.LeafName}
}

// provideManager 构造发号 Manager；cleanup 负责优雅关闭（revoke 全部 lease）。
func provideManager(cfg iddomain.Config, factory iddomain.TagInfraFactory) (*iddomain.Manager, func(), error) {
	manager, err := iddomain.NewManager(cfg, factory)
	if err != nil {
		return nil, nil, err
	}
	return manager, manager.Close, nil
}

// newGRPCRegister 返回 gRPC 服务注册回调，交给 grpcinfra.Run 挂载。
func newGRPCRegister(srv *idgrpc.IdGenServer) func(*grpc.Server, *health.Server) {
	return func(s *grpc.Server, hc *health.Server) {
		idgenpb.RegisterIdGenServer(s, srv)
		hc.SetServingStatus("idgen.IdGen", healthpb.HealthCheckResponse_SERVING)
	}
}
