package nacos

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

type registeredInstance struct {
	service string
	group   string
	ip      string
	port    uint64
	cluster string
}

var registered *registeredInstance

func Register(serviceName string, port int, meta map[string]string) error {
	if !isEnabled() {
		return nil
	}
	if strings.TrimSpace(serviceName) == "" || port <= 0 || port > 65535 {
		return errors.New("invalid nacos registration service or port")
	}
	client := getNamingClient()
	if client == nil {
		return errors.New("nacos naming client is not initialized")
	}
	state.mu.RLock()
	cfg := state.cfg
	state.mu.RUnlock()
	group := currentGroup()
	ip, err := registerIP()
	if err != nil {
		return err
	}
	weight := cfg.Registry.Weight
	if weight <= 0 {
		weight = 1
	}
	cluster := cfg.Registry.Cluster
	if meta == nil {
		meta = cfg.Registry.Meta
	}
	_, err = client.RegisterInstance(vo.RegisterInstanceParam{
		Ip: ip, Port: uint64(port), ServiceName: serviceName, GroupName: group,
		ClusterName: cluster, Weight: weight, Enable: true, Healthy: true,
		Ephemeral: true, Metadata: meta,
	})
	if err != nil {
		return fmt.Errorf("register nacos instance %s/%s:%d: %w", group, serviceName, port, err)
	}
	registered = &registeredInstance{service: serviceName, group: group, ip: ip, port: uint64(port), cluster: cluster}
	return nil
}

func Deregister(serviceName string, port int) error {
	if !isEnabled() {
		return nil
	}
	client := getNamingClient()
	if client == nil || registered == nil {
		return nil
	}
	instance := *registered
	if serviceName != "" {
		instance.service = serviceName
	}
	if port > 0 {
		instance.port = uint64(port)
	}
	_, err := client.DeregisterInstance(vo.DeregisterInstanceParam{
		Ip: instance.ip, Port: instance.port, ServiceName: instance.service,
		GroupName: instance.group, Cluster: instance.cluster, Ephemeral: true,
	})
	if err != nil {
		return fmt.Errorf("deregister nacos instance %s/%s:%d: %w", instance.group, instance.service, instance.port, err)
	}
	registered = nil
	return nil
}

func registerIP() (string, error) {
	for _, name := range []string{"NACOS_REGISTER_IP", "POD_IP", "HOST_IP"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value, nil
		}
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", fmt.Errorf("discover nacos registration IP: %w", err)
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			return ipNet.IP.String(), nil
		}
	}
	return "", errors.New("unable to discover nacos registration IP")
}

func DrainWait() time.Duration {
	return 5 * time.Second
}
