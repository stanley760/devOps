// Package logger 基于标准库 log/slog 封装应用日志。
//
// 能力：
//   - 日志级别通过配置读取（debug/info/warn/error）
//   - 文件滚动由 timberjack 负责，对齐 logback 的 SizeAndTimeBasedRollingPolicy：
//     单文件超过 max_size 自动切分，超过 rotation_interval 也会切分，
//     历史文件按 max_backups / max_age 保留，并可 gzip/zstd 压缩
//   - 控制台与文件双输出，控制台固定为易读的 text 格式
//   - 文件写入走异步缓冲区，磁盘 IO 不会阻塞业务协程
package logger

import (
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DeRuina/timberjack"

	"devops-infra/config"
)

const (
	defaultPrefix     = "devops"
	defaultBufferSize = 4096
	// 备份文件名中的时间格式，形如 devops-2026-09-10-16-44-05.123-size.log
	// 两个注意点：
	//  1. timberjack 会校验格式能否完整往返（format→parse 后时间不变），
	//     因此必须包含时分秒，只写 2006-01-02 会校验失败
	//  2. 必须带毫秒，否则高吞吐下同一秒内多次切分会产生同名文件而互相覆盖
	defaultTimeFormat = "2006-01-02-15-04-05.000"
)

var (
	currentRolling *timberjack.Logger
	currentAsync   *asyncWriter
)

// Init 按配置初始化全局 slog 默认 logger。
// 之后直接使用 slog.Info / slog.Error 等包级函数即可。
func Init(cfg config.LogConfig) error {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return err
	}

	dir := strings.TrimSpace(cfg.Dir)
	if dir == "" {
		dir = "logs"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}

	interval, err := parseInterval(cfg.RotationInterval)
	if err != nil {
		return err
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
		return fmt.Errorf("日志备份时间格式非法: %w", err)
	}

	// 落盘交给 timberjack，外面再包一层异步缓冲，避免 IO 影响业务
	currentRolling = rolling
	currentAsync = newAsyncWriter(rolling, cfg.Buffer)

	opts := &slog.HandlerOptions{Level: level}

	var fileHandler slog.Handler
	if strings.EqualFold(strings.TrimSpace(cfg.Format), "json") {
		fileHandler = slog.NewJSONHandler(currentAsync, opts)
	} else {
		fileHandler = slog.NewTextHandler(currentAsync, opts)
	}

	if !cfg.Console {
		slog.SetDefault(slog.New(fileHandler))
		return nil
	}

	// Go 1.26 起标准库提供 slog.NewMultiHandler，语义与自研实现一致：
	// Enabled 取任一 handler，Handle 分发给所有启用的 handler。
	// 文件与控制台格式不同（文件可配 json，控制台固定易读 text），所以必须分两个 handler。
	slog.SetDefault(slog.New(slog.NewMultiHandler(
		fileHandler,
		slog.NewTextHandler(os.Stdout, opts),
	)))
	return nil
}

// Close 排空异步缓冲区并关闭日志文件，建议在程序退出前调用。
func Close() error {
	var errs []error

	if currentAsync != nil {
		if dropped := currentAsync.Dropped(); dropped > 0 {
			// 此时异步通道即将关闭，用标准库 log 同步写 stderr，
			// 保证这条告警不丢也不会再进缓冲队列
			log.Printf("logger: 因缓冲区已满丢弃 %d 条日志", dropped)
		}
		if err := currentAsync.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if currentRolling != nil {
		if err := currentRolling.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("不支持的日志级别: %q（可选 debug/info/warn/error）", s)
	}
}

// parseInterval 解析时间滚动间隔，空值或 0 表示仅按大小滚动
func parseInterval(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("rotation_interval 格式非法: %q（示例 24h、12h30m）: %w", s, err)
	}
	return d, nil
}

func normalizeCompression(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "gzip"
	}
	return s
}
