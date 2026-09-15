package nacos

import (
	"fmt"
	"strings"
	"sync"

	"github.com/nacos-group/nacos-sdk-go/v2/model"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"google.golang.org/grpc/resolver"
)

var registerResolverOnce sync.Once

func RegisterNacosResolver() {
	registerResolverOnce.Do(func() {
		resolver.Register(&nacosBuilder{})
	})
}

type nacosBuilder struct{}

func (*nacosBuilder) Scheme() string { return "nacos" }

func (*nacosBuilder) Build(target resolver.Target, cc resolver.ClientConn, _ resolver.BuildOptions) (resolver.Resolver, error) {
	serviceName := strings.TrimSpace(target.Endpoint())
	if serviceName == "" {
		return nil, fmt.Errorf("nacos service name cannot be empty")
	}
	r := &nacosResolver{serviceName: serviceName, cc: cc}
	if err := r.update(); err != nil {
		return nil, err
	}
	if err := r.subscribe(); err != nil {
		return nil, err
	}
	return r, nil
}

type nacosResolver struct {
	serviceName string
	cc          resolver.ClientConn
}

func (r *nacosResolver) ResolveNow(resolver.ResolveNowOptions) {
	_ = r.update()
}

func (*nacosResolver) Close() {}

func (r *nacosResolver) update() error {
	client := getNamingClient()
	if client == nil {
		return fmt.Errorf("nacos naming client is not initialized")
	}
	instances, err := client.SelectInstances(vo.SelectInstancesParam{
		ServiceName: r.serviceName, GroupName: currentGroup(), HealthyOnly: true,
	})
	if err != nil {
		return fmt.Errorf("resolve nacos service %q: %w", r.serviceName, err)
	}
	addresses := make([]resolver.Address, 0, len(instances))
	for _, instance := range instances {
		if !instance.Enable || instance.Weight <= 0 || instance.Ip == "" || instance.Port == 0 {
			continue
		}
		addresses = append(addresses, resolver.Address{Addr: fmt.Sprintf("%s:%d", instance.Ip, instance.Port)})
	}
	return r.cc.UpdateState(resolver.State{
		Addresses:     addresses,
		ServiceConfig: r.cc.ParseServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`),
	})
}

func (r *nacosResolver) subscribe() error {
	client := getNamingClient()
	if client == nil {
		return fmt.Errorf("nacos naming client is not initialized")
	}
	return client.Subscribe(&vo.SubscribeParam{
		ServiceName: r.serviceName,
		GroupName:   currentGroup(),
		SubscribeCallback: func(_ []model.Instance, err error) {
			if err == nil {
				_ = r.update()
			}
		},
	})
}
