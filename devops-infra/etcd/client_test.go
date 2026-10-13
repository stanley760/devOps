package etcd

import (
	"testing"
	"time"

	infracfg "github.com/stanley760/devOps/devops-infra/config"
)

func TestClientOptionsUsesConfig(t *testing.T) {
	cfg, err := infracfg.NormalizeEtcdConfig(infracfg.EtcdConfig{
		Endpoints:   []string{"http://etcd-1:2379", "http://etcd-2:2379"},
		Username:    "root",
		Password:    "secret",
		DialTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	got := clientOptions(cfg)
	if len(got.Endpoints) != 2 || got.Endpoints[0] != cfg.Endpoints[0] {
		t.Fatalf("Endpoints = %v, want %v", got.Endpoints, cfg.Endpoints)
	}
	if got.Username != "root" || got.Password != "secret" {
		t.Fatalf("auth credentials not propagated: %+v", got)
	}
	if got.DialTimeout != 2*time.Second {
		t.Fatalf("DialTimeout = %v, want 2s", got.DialTimeout)
	}
}

func TestValidateTLSRejectsMissingCertificate(t *testing.T) {
	cfg := infracfg.EtcdConfig{
		Endpoints: []string{"https://etcd:2379"},
		TLS: &infracfg.EtcdTLSConfig{
			CertFile: "client.pem",
		},
	}
	if err := cfg.ValidateTLS(); err == nil {
		t.Fatal("expected error when only one certificate file is configured")
	}
}
