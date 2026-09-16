package config

import (
	"testing"
	"time"
)

func TestNormalizeEtcdConfigDefaults(t *testing.T) {
	cfg, err := NormalizeEtcdConfig(EtcdConfig{
		Endpoints: []string{"http://127.0.0.1:2379"},
	})
	if err != nil {
		t.Fatalf("NormalizeEtcdConfig() error = %v", err)
	}
	if cfg.DialTimeout != 5*time.Second {
		t.Fatalf("DialTimeout = %v, want 5s", cfg.DialTimeout)
	}
	if cfg.LeaseTTL != 10*time.Second {
		t.Fatalf("LeaseTTL = %v, want 10s", cfg.LeaseTTL)
	}
}

func TestNormalizeEtcdConfigRejectsMissingEndpoints(t *testing.T) {
	if _, err := NormalizeEtcdConfig(EtcdConfig{}); err == nil {
		t.Fatal("expected error for empty endpoints")
	}
}

func TestNormalizeEtcdConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		cfg  EtcdConfig
	}{
		{"dial timeout", EtcdConfig{Endpoints: []string{"http://etcd"}, DialTimeout: -time.Second}},
		{"lease ttl", EtcdConfig{Endpoints: []string{"http://etcd"}, LeaseTTL: -time.Second}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NormalizeEtcdConfig(tt.cfg); err == nil {
				t.Fatalf("expected error for invalid %s", tt.name)
			}
		})
	}
}

func TestNormalizeEtcdConfigRejectsIncompleteAuth(t *testing.T) {
	_, err := NormalizeEtcdConfig(EtcdConfig{
		Endpoints: []string{"http://etcd:2379"},
		Username:  "root",
	})
	if err == nil {
		t.Fatal("expected error when username is set without password")
	}
}
