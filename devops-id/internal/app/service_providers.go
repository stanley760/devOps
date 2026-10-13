package app

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"

	etcdinfra "github.com/stanley760/devOps/devops-infra/etcd"

	idapp "github.com/stanley760/devOps/devops-id/internal/application/idgen"
	"github.com/stanley760/devOps/devops-id/internal/config"
	iddomain "github.com/stanley760/devOps/devops-id/internal/domain/idgen"
	"github.com/stanley760/devOps/devops-id/internal/infrastructure/etcdcoord"
	idgrpc "github.com/stanley760/devOps/devops-id/internal/interfaces/grpc"
	"github.com/stanley760/devOps/devops-id/pkg/idgenpb"
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
	return iddomain.Config{
		LeafName:                 cfg.IDGen.LeafName,
		AllowLocalFallback:       cfg.IDGen.AllowLocalFallback,
		MaxBizTagLen:             cfg.IDGen.MaxBizTagLen,
		MaxGenerators:            cfg.IDGen.MaxGenerators,
		MaxBatchCount:            cfg.IDGen.MaxBatchCount,
		WatermarkSafetyMargin:    cfg.IDGen.WatermarkSafetyMargin,
		WatermarkPersistInterval: cfg.IDGen.WatermarkPersistInterval,
		WatermarkMaxWait:         cfg.IDGen.WatermarkMaxWait,
	}
}

// provideObserver 组合指标观察器与健康桥为统一 Observer。
func provideObserver(bridge *HealthBridge) iddomain.Observer {
	return newFanoutObserver(idapp.NewMetricsObserver(), bridge)
}

// provideHealthBridge 构造健康桥。
func provideHealthBridge() *HealthBridge {
	return NewHealthBridge()
}

// provideManager 构造发号 Manager；cleanup 负责优雅关闭（revoke 全部 lease）。
func provideManager(cfg iddomain.Config, factory iddomain.TagInfraFactory, obs iddomain.Observer) (*iddomain.Manager, func(), error) {
	manager, err := iddomain.NewManager(cfg, factory, obs)
	if err != nil {
		return nil, nil, err
	}
	return manager, manager.Close, nil
}

// newGRPCRegister 返回 gRPC 服务注册回调，交给 grpcinfra.Run 挂载。
// 注册时绑定健康桥，让 readiness/liveness 反映真实发号能力。
func newGRPCRegister(srv *idgrpc.IdGenServer, bridge *HealthBridge) func(*grpc.Server, *health.Server) {
	return func(s *grpc.Server, hc *health.Server) {
		idgenpb.RegisterIdGenServer(s, srv)
		bridge.Bind(hc)
	}
}
