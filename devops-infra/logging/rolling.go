package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DeRuina/timberjack"

	"github.com/stanley760/devOps/devops-infra/config"
)

const (
	defaultPrefix = "devops"
	// 备份文件名中的时间格式，形如 devops-2026-09-10-16-44-05.123-size.log。
	// 必须包含时分秒和毫秒，避免高吞吐下多次切分产生同名文件。
	defaultTimeFormat = "2006-01-02-15-04-05.000"
)

// newRollingLogger 按配置创建文件滚动 writer。
func newRollingLogger(cfg config.LogConfig) (*timberjack.Logger, error) {
	dir := strings.TrimSpace(cfg.Dir)
	if dir == "" {
		dir = "logs"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}

	interval, err := parseInterval(cfg.RotationInterval)
	if err != nil {
		return nil, err
	}

	rolling := &timberjack.Logger{
		Filename:         filepath.Join(dir, defaultPrefix+".log"),
		MaxSize:          cfg.MaxSize,
		MaxBackups:       cfg.MaxBackups,
		MaxAge:           cfg.MaxAge,
		Compression:      normalizeCompression(cfg.Compress),
		RotationInterval: interval,
		LocalTime:        true,
		BackupTimeFormat: defaultTimeFormat,
	}
	if err := rolling.ValidateBackupTimeFormat(); err != nil {
		return nil, fmt.Errorf("日志备份时间格式非法: %w", err)
	}
	return rolling, nil
}

// parseInterval 解析时间滚动间隔，空值或 0 表示仅按大小滚动。
func parseInterval(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "0" {
		return 0, nil
	}
	interval, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("rotation_interval 格式非法: %q（示例 24h、12h30m）: %w", value, err)
	}
	return interval, nil
}

func normalizeCompression(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "gzip"
	}
	return value
}
