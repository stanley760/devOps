package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stanley760/devops/devops-infra/config"
)

// TestRollingBySize 验证单文件超过 max_size 后会自动切分并压缩历史文件。
func TestRollingBySize(t *testing.T) {
	dir := t.TempDir()

	cfg := config.LogConfig{
		Level:      "info",
		Dir:        dir,
		Console:    false, // 关掉控制台输出，避免干扰测试输出
		Format:     "text",
		Buffer:     16384,
		MaxSize:    1, // 1MB
		MaxBackups: 10,
		Compress:   "gzip",
	}

	if err := Init(cfg); err != nil {
		t.Fatalf("初始化失败: %v", err)
	}

	// 写入约 3MB，应触发 3 次按大小切分
	line := strings.Repeat("x", 512)
	const total = 3 * 1024 * 1024
	for written := 0; written < total; written += len(line) {
		slog.Info(line)
	}

	if err := Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	gz, _ := filepath.Glob(filepath.Join(dir, "*.gz"))
	if len(gz) < 2 {
		all, _ := filepath.Glob(filepath.Join(dir, "*"))
		t.Fatalf("应至少产生 2 个压缩的历史文件，实际 %d 个；目录内容: %v", len(gz), all)
	}

	// 当前写入的文件不应超过 max_size
	if info, err := os.Stat(filepath.Join(dir, "devops.log")); err == nil {
		if info.Size() > int64(1024*1024) {
			t.Errorf("当前文件 %d 字节，超过了 1MB 上限", info.Size())
		}
	}

	t.Logf("切分结果: %d 个 .gz 历史文件", len(gz))
	for _, f := range gz {
		t.Logf("  %s", filepath.Base(f))
	}
}

// TestRollingKeepsMaxBackups 验证历史文件数量受 max_backups 约束。
func TestRollingKeepsMaxBackups(t *testing.T) {
	dir := t.TempDir()

	cfg := config.LogConfig{
		Level:      "info",
		Dir:        dir,
		Console:    false,
		Format:     "text",
		Buffer:     16384,
		MaxSize:    1, // 1MB
		MaxBackups: 2, // 只保留 2 个历史文件
		Compress:   "none",
	}

	if err := Init(cfg); err != nil {
		t.Fatalf("初始化失败: %v", err)
	}

	line := strings.Repeat("y", 512)
	const total = 5 * 1024 * 1024
	for written := 0; written < total; written += len(line) {
		slog.Info(line)
	}

	if err := Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}

	// 当前文件 + 最多 2 个历史文件
	if len(entries) > 3 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("文件数 %d 超过了 max_backups(2)+当前文件 的限制: %v", len(entries), names)
	}

	t.Logf("保留文件数: %d（max_backups=2）", len(entries))
}

func TestParseInterval(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"0", 0},
		{"24h", 24 * time.Hour},
		{"30m", 30 * time.Minute},
		{"1h30m", 90 * time.Minute},
	}

	for _, c := range cases {
		got, err := parseInterval(c.in)
		if err != nil {
			t.Fatalf("parseInterval(%q) 报错: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("parseInterval(%q) = %v, 期望 %v", c.in, got, c.want)
		}
	}

	if _, err := parseInterval("abc"); err == nil {
		t.Error("非法间隔应返回错误")
	}
}
