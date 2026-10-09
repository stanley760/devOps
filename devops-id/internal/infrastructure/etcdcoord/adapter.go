package etcdcoord

import (
	"context"
	"errors"
	"fmt"

	etcdinfra "devops-infra/etcd"

	iddomain "devops-id/internal/domain/idgen"
)

// slotCoordinatorAdapter 将 etcdinfra.SlotCoordinator 适配为领域 SlotCoordinator
// 端口：仅把 ErrEtcdUnavailable 包装为领域 ErrInfraUnavailable（触发本地槽位
// 回退），ErrSlotOccupied / ErrSlotsExhausted / 参数错误原样透传。包装而非替换，
// errors.Is 对两个 sentinel 均成立。
type slotCoordinatorAdapter struct {
	coord *etcdinfra.SlotCoordinator
}

func (a slotCoordinatorAdapter) Acquire(ctx context.Context, keyPrefix, identity string, count int) (int, error) {
	slot, err := a.coord.Acquire(ctx, keyPrefix, identity, count)
	if errors.Is(err, etcdinfra.ErrEtcdUnavailable) {
		return slot, fmt.Errorf("%w: %w", iddomain.ErrInfraUnavailable, err)
	}
	return slot, err
}

func (a slotCoordinatorAdapter) Refresh(ctx context.Context, keyPrefix, identity string, slot, timestamp int64) error {
	return a.coord.Refresh(ctx, keyPrefix, identity, slot, timestamp)
}
