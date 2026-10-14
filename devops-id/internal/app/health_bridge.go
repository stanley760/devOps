package app

import (
	iddomain "github.com/stanley760/devops/devops-id/internal/domain/idgen"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"sync"
)

// HealthBridge 将领域健康状态同步到 gRPC health server。
//
// 映射策略（readiness / liveness 分离）：
//   - ""（默认）和 "idgen.IdGen"：仅 Serving 时 SERVING，Degraded/NotServing → NOT_SERVING
//     ——readiness 探针摘流
//   - "idgen.IdGen.live"：Serving+Degraded 时 SERVING，仅 NotServing → NOT_SERVING
//     ——liveness 探针，防止降级 Pod 被反复重启
type HealthBridge struct {
	mu    sync.Mutex
	hc    *health.Server
	state iddomain.HealthState
}

// NewHealthBridge 创建健康桥。Bind 之前事件仅记录状态，不触达 gRPC。
func NewHealthBridge() *HealthBridge {
	return &HealthBridge{state: iddomain.HealthServing}
}

// Bind 绑定 gRPC health server 并立即应用当前状态。
func (b *HealthBridge) Bind(hc *health.Server) {
	b.mu.Lock()
	b.hc = hc
	state := b.state
	b.mu.Unlock()
	b.apply(state)
}

// OnState 实现 iddomain.Observer 的状态回调。
func (b *HealthBridge) OnState(_ string, state iddomain.HealthState) {
	b.mu.Lock()
	// 整体状态取最差值（tag="" 表示整体）。
	if state > b.state {
		b.state = state
	}
	hc := b.hc
	cur := b.state
	b.mu.Unlock()
	if hc != nil {
		b.apply(cur)
	}
}

// OnFallbackActive 实现 iddomain.Observer（本地降级触发 NOT_SERVING 摘流）。
func (b *HealthBridge) OnFallbackActive(_ string) {
	b.OnState("", iddomain.HealthNotServing)
}

// OnLeaseLost 实现 iddomain.Observer（lease 失效 → NOT_SERVING）。
func (b *HealthBridge) OnLeaseLost(_ string) {
	b.OnState("", iddomain.HealthNotServing)
}

// OnClockRollback 实现 iddomain.Observer（时钟回拨为暂态，不改变健康状态）。
func (b *HealthBridge) OnClockRollback(_ string, _ int64) {}

// apply 将状态写入三个 health service name。
func (b *HealthBridge) apply(state iddomain.HealthState) {
	b.mu.Lock()
	hc := b.hc
	b.mu.Unlock()
	if hc == nil {
		return
	}
	// readiness：仅完全健康时 SERVING
	readiness := healthpb.HealthCheckResponse_SERVING
	if state != iddomain.HealthServing {
		readiness = healthpb.HealthCheckResponse_NOT_SERVING
	}
	// liveness：降级仍存活，仅 NotServing 才不健康
	liveness := healthpb.HealthCheckResponse_SERVING
	if state == iddomain.HealthNotServing {
		liveness = healthpb.HealthCheckResponse_NOT_SERVING
	}
	hc.SetServingStatus("", readiness)
	hc.SetServingStatus("idgen.IdGen", readiness)
	hc.SetServingStatus("idgen.IdGen.live", liveness)
}

// fanoutObserver 组合多个 Observer 为一个。
type fanoutObserver struct {
	observers []iddomain.Observer
}

// newFanoutObserver 创建组合观察器。
func newFanoutObserver(observers ...iddomain.Observer) iddomain.Observer {
	return &fanoutObserver{observers: observers}
}

func (f *fanoutObserver) OnFallbackActive(bizTag string) {
	for _, o := range f.observers {
		o.OnFallbackActive(bizTag)
	}
}

func (f *fanoutObserver) OnLeaseLost(bizTag string) {
	for _, o := range f.observers {
		o.OnLeaseLost(bizTag)
	}
}

func (f *fanoutObserver) OnClockRollback(bizTag string, rollbackMs int64) {
	for _, o := range f.observers {
		o.OnClockRollback(bizTag, rollbackMs)
	}
}

func (f *fanoutObserver) OnState(bizTag string, state iddomain.HealthState) {
	for _, o := range f.observers {
		o.OnState(bizTag, state)
	}
}
