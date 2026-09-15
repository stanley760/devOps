package nacos

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"devops-infra/config"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

type clientState struct {
	mu      sync.RWMutex
	cfg     config.NacosConfig
	config  config_client.IConfigClient
	naming  naming_client.INamingClient
	enabled bool
}

var state clientState

func Init(cfg config.NacosConfig) error {
	state.mu.Lock()
	defer state.mu.Unlock()

	if !cfg.Enabled {
		state.cfg = config.NacosConfig{}
		state.config = nil
		state.naming = nil
		state.enabled = false
		return nil
	}
	if len(cfg.Endpoints) == 0 {
		return errors.New("nacos endpoints cannot be empty")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	servers, err := serverConfigs(cfg.Endpoints)
	if err != nil {
		return err
	}
	cc := constant.ClientConfig{
		TimeoutMs:           uint64(timeout / time.Millisecond),
		NamespaceId:         cfg.Namespace,
		Username:            cfg.Username,
		Password:            cfg.Password,
		NotLoadCacheAtStart: false,
	}
	param := vo.NacosClientParam{ClientConfig: &cc, ServerConfigs: servers}
	configClient, err := clients.NewConfigClient(param)
	if err != nil {
		return fmt.Errorf("create nacos config client: %w", err)
	}
	namingClient, err := clients.NewNamingClient(param)
	if err != nil {
		return fmt.Errorf("create nacos naming client: %w", err)
	}

	state.cfg = cfg
	state.config = configClient
	state.naming = namingClient
	state.enabled = true
	RegisterNacosResolver()
	return nil
}

func Close() error {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.cfg = config.NacosConfig{}
	state.config = nil
	state.naming = nil
	state.enabled = false
	return nil
}

func getConfigClient() config_client.IConfigClient {
	state.mu.RLock()
	defer state.mu.RUnlock()
	if !state.enabled {
		return nil
	}
	return state.config
}

func isEnabled() bool {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.enabled
}

func getNamingClient() naming_client.INamingClient {
	state.mu.RLock()
	defer state.mu.RUnlock()
	if !state.enabled {
		return nil
	}
	return state.naming
}

func serverConfigs(endpoints []string) ([]constant.ServerConfig, error) {
	servers := make([]constant.ServerConfig, 0, len(endpoints))
	for _, endpoint := range endpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			continue
		}
		host, portText, err := net.SplitHostPort(endpoint)
		if err != nil {
			host = endpoint
			portText = "8848"
		}
		port, err := strconv.ParseUint(portText, 10, 16)
		if err != nil || port == 0 {
			return nil, fmt.Errorf("invalid nacos endpoint %q", endpoint)
		}
		servers = append(servers, constant.ServerConfig{IpAddr: host, Port: port})
	}
	if len(servers) == 0 {
		return nil, errors.New("nacos endpoints cannot be empty")
	}
	return servers, nil
}
