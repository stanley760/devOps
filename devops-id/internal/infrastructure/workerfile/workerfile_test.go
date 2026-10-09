package workerfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	cache := New(t.TempDir(), "leaf", "user")
	if err := cache.Ensure(); err != nil {
		t.Fatal(err)
	}
	cache.Save(7)

	got, ok := cache.Load()
	if !ok || got != 7 {
		t.Fatalf("Load() = (%d, %v), want (7, true)", got, ok)
	}
	// 磁盘格式必须是纯十进制整数文本，与历史缓存文件兼容。
	data, err := os.ReadFile(cache.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "7" {
		t.Fatalf("file content = %q, want %q", data, "7")
	}
}

func TestLoadMissing(t *testing.T) {
	cache := New(t.TempDir(), "leaf", "user")
	if _, ok := cache.Load(); ok {
		t.Fatal("缺失文件不应加载成功")
	}
}

func TestLoadCorrupt(t *testing.T) {
	cache := New(t.TempDir(), "leaf", "user")
	if err := cache.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache.path, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Load(); ok {
		t.Fatal("损坏文件不应加载成功")
	}
}

func TestEnsureCreatesNestedDirs(t *testing.T) {
	root := t.TempDir()
	cache := New(filepath.Join(root, "a"), "leaf", "user")
	if err := cache.Ensure(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(root, "a", "leaf", "user")); err != nil || !info.IsDir() {
		t.Fatalf("嵌套目录未创建: %v", err)
	}
}
