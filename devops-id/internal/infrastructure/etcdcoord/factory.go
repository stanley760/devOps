// Package etcdcoord 组装发号域的 etcd 基础设施：lease 生命周期与 worker
// 槽位协调由共享库 devops-infra/etcd 提供，本包按 biz_tag 构造 TagInfra
// 并把基础设施错误适配为领域错误。
package etcdcoord

import (
	"time"

	etcdinfra "devops-infra/etcd"

	iddomain "devops-id/internal/domain/idgen"
	"devops-id/internal/infrastructure/workerfile"
)

// NewInfraFactory 返回按 biz_tag 构造领域 TagInfra 的工厂。
// client 为 nil 时 Lease/Slots 为 nil（领域层走本地槽位回退）；
// cacheRoot 为空时默认 "data"。
func NewInfraFactory(client *etcdinfra.Client, leafName string, leaseTTL time.Duration, cacheRoot string) iddomain.TagInfraFactory {
	if cacheRoot == "" {
		cacheRoot = "data"
	}
	return func(bizTag string) iddomain.TagInfra {
		infra := iddomain.TagInfra{Cache: workerfile.New(cacheRoot, leafName, bizTag)}
		if client != nil {
			lease := etcdinfra.NewLeaseManager(client, leaseTTL)
			infra.Lease = lease
			infra.Slots = slotCoordinatorAdapter{coord: etcdinfra.NewSlotCoordinator(client, lease)}
		}
		return infra
	}
}

// 编译期端口断言：基础设施实现必须满足领域端口。
var (
	_ iddomain.LeaseLifecycle  = (*etcdinfra.LeaseManager)(nil)
	_ iddomain.SlotCoordinator = slotCoordinatorAdapter{}
	_ iddomain.WorkerCache     = (*workerfile.Cache)(nil)
)
