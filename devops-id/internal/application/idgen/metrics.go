package idgen

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	iddomain "github.com/stanley760/devOps/devops-id/internal/domain/idgen"
)

// MetricsObserver 实现 iddomain.Observer，将领域事件记录为 OTel 指标。
type MetricsObserver struct {
	fallbackTotal  metric.Int64Counter
	leaseLostTotal metric.Int64Counter
	rollbackTotal  metric.Int64Counter
	rejectedTotal  metric.Int64Counter
	healthState    metric.Int64ObservableGauge

	mu     sync.Mutex
	states map[string]iddomain.HealthState
}

// NewMetricsObserver 创建指标观察器。指标创建失败时 OTel 返回 noop（非 nil）。
func NewMetricsObserver() *MetricsObserver {
	meter := otel.Meter("devops-id/idgen")
	obs := &MetricsObserver{states: make(map[string]iddomain.HealthState)}
	var err error
	if obs.fallbackTotal, err = meter.Int64Counter(
		"idgen_fallback_total",
		metric.WithDescription("进入本地降级模式的次数"),
	); err != nil {
		otel.Handle(err)
	}
	if obs.leaseLostTotal, err = meter.Int64Counter(
		"idgen_lease_lost_total",
		metric.WithDescription("etcd lease 失效次数"),
	); err != nil {
		otel.Handle(err)
	}
	if obs.rollbackTotal, err = meter.Int64Counter(
		"idgen_clock_rollback_total",
		metric.WithDescription("时钟回拨导致发号拒绝的次数"),
	); err != nil {
		otel.Handle(err)
	}
	if obs.rejectedTotal, err = meter.Int64Counter(
		"idgen_requests_rejected_total",
		metric.WithDescription("发号被拒绝的次数"),
	); err != nil {
		otel.Handle(err)
	}
	if obs.healthState, err = meter.Int64ObservableGauge(
		"idgen_health_state",
		metric.WithDescription("发号健康状态（0=serving, 1=degraded, 2=not_serving）"),
	); err != nil {
		otel.Handle(err)
	}

	if _, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		obs.mu.Lock()
		defer obs.mu.Unlock()
		for tag, st := range obs.states {
			attrs := metric.WithAttributes(attribute.String("biz_tag", tag))
			o.ObserveInt64(obs.healthState, int64(st), attrs)
		}
		return nil
	}, obs.healthState); err != nil {
		otel.Handle(err)
	}
	return obs
}

func (o *MetricsObserver) OnFallbackActive(bizTag string) {
	if o == nil || o.fallbackTotal == nil {
		return
	}
	o.fallbackTotal.Add(context.Background(), 1, metric.WithAttributes(attribute.String("biz_tag", bizTag)))
}

func (o *MetricsObserver) OnLeaseLost(bizTag string) {
	if o == nil || o.leaseLostTotal == nil {
		return
	}
	o.leaseLostTotal.Add(context.Background(), 1, metric.WithAttributes(attribute.String("biz_tag", bizTag)))
}

func (o *MetricsObserver) OnClockRollback(bizTag string, _ int64) {
	if o == nil || o.rollbackTotal == nil {
		return
	}
	o.rollbackTotal.Add(context.Background(), 1, metric.WithAttributes(attribute.String("biz_tag", bizTag)))
}

func (o *MetricsObserver) OnState(bizTag string, state iddomain.HealthState) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.states[bizTag] = state
	o.mu.Unlock()
}
