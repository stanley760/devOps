package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestInitUsesRegistryServiceNameAndServiceProfile(t *testing.T) {
	t.Setenv(EnvAppEnv, "test")

	dir := t.TempDir()
	bootstrap := filepath.Join(dir, "bootstrap.yaml")
	writeTestFile(t, bootstrap, "nacos:\n  config:\n    data_ids:\n      - other-service.yaml\n  registry:\n    service_name: devops-id\nlog:\n  level: info\n")
	writeTestFile(t, filepath.Join(dir, "devops-id-test.yaml"), "log:\n  level: debug\n")

	if err := Init(bootstrap); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(viper.Reset)

	if got := viper.GetString("log.level"); got != "debug" {
		t.Fatalf("log.level = %q, want %q", got, "debug")
	}
	if got := ServiceName(); got != "devops-id" {
		t.Fatalf("ServiceName() = %q, want %q", got, "devops-id")
	}
}

func TestMergeWithYAMLOverridesWithoutReset(t *testing.T) {
	dir := t.TempDir()
	bootstrap := filepath.Join(dir, "bootstrap.yaml")
	writeTestFile(t, bootstrap, "server:\n  port: 9000\nlog:\n  level: info\n")

	if err := Init(bootstrap); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(viper.Reset)

	if err := MergeWithYAML("log:\n  level: warn\n"); err != nil {
		t.Fatalf("MergeWithYAML() error = %v", err)
	}
	if got := viper.GetString("log.level"); got != "warn" {
		t.Fatalf("log.level = %q, want %q", got, "warn")
	}
	if got := viper.GetInt("server.port"); got != 9000 {
		t.Fatalf("server.port = %d, want 9000", got)
	}
}

func TestRemoteDataIDsAddsServiceAndProfileConfigs(t *testing.T) {
	t.Setenv(EnvAppEnv, "prod")

	dir := t.TempDir()
	bootstrap := filepath.Join(dir, "bootstrap.yaml")
	writeTestFile(t, bootstrap, "nacos:\n  registry:\n    service_name: devops-system\n")

	if err := Init(bootstrap); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(viper.Reset)

	got := RemoteDataIDs([]string{"devops-common.yaml"})
	want := []string{"devops-common.yaml", "devops-system.yaml", "devops-system-prod.yaml"}
	if len(got) != len(want) {
		t.Fatalf("RemoteDataIDs() = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("RemoteDataIDs()[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
