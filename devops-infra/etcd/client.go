package etcd

import (
	clientv3 "go.etcd.io/etcd/client/v3"

	infracfg "github.com/stanley760/devOps/devops-infra/config"
)

// Client wraps clientv3.Client so callers depend on the infrastructure module
// instead of constructing and validating etcd options themselves.
type Client struct {
	raw *clientv3.Client
}

func New(cfg infracfg.EtcdConfig) (*Client, error) {
	normalized, err := infracfg.NormalizeEtcdConfig(cfg)
	if err != nil {
		return nil, err
	}
	options := clientOptions(normalized)
	options.TLS, err = normalized.TLSConfig()
	if err != nil {
		return nil, err
	}

	raw, err := clientv3.New(options)
	if err != nil {
		return nil, err
	}
	return &Client{raw: raw}, nil
}

func clientOptions(cfg infracfg.EtcdConfig) clientv3.Config {
	return clientv3.Config{
		Endpoints:   cfg.Endpoints,
		Username:    cfg.Username,
		Password:    cfg.Password,
		DialTimeout: cfg.DialTimeout,
	}
}

func (c *Client) Close() error {
	if c == nil || c.raw == nil {
		return nil
	}
	return c.raw.Close()
}
