// Package logger 基于标准库 log/slog 封装应用日志。
//
// 能力：
//   - 日志级别通过配置读取（debug/info/warn/error）
//   - 文件滚动由 timberjack 负责，对齐 logback 的 SizeAndTimeBasedRollingPolicy：
//     单文件超过 max_size 自动切分，超过 rotation_interval 也会切分，
//     历史文件按 max_backups / max_age 保留，并可 gzip/zstd 压缩
//   - 控制台与文件双输出，控制台固定为易读的 text 格式
//   - 文件写入走异步缓冲区，磁盘 IO 不会阻塞业务协程
package logging

import (
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/DeRuina/timberjack"

	"devops-infra/config"
)

const defaultBufferSize = 4096

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

	rolling, err := newRollingLogger(cfg)
	if err != nil {
		return err
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
