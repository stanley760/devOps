package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 应用配置
type Config struct {
	Log LogConfig `yaml:"log"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level   string `yaml:"level"`   // debug | info | warn | error
	Dir     string `yaml:"dir"`     // 日志文件存放目录
	Console bool   `yaml:"console"` // 是否同时输出到控制台
	Format  string `yaml:"format"`  // text | json
	Buffer  int    `yaml:"buffer"`  // 异步写入缓冲区大小，满则丢弃，默认 4096

	// 以下为文件滚动策略，对齐 logback 的 SizeAndTimeBasedRollingPolicy
	MaxSize          int    `yaml:"max_size"`          // 单文件上限（MB），写满即切分，默认 100
	MaxBackups       int    `yaml:"max_backups"`       // 保留的历史文件个数，默认 30，0 表示不限制
	MaxAge           int    `yaml:"max_age"`           // 历史文件保留天数，0 表示不限制
	Compress         string `yaml:"compress"`          // none | gzip | zstd，默认 gzip
	RotationInterval string `yaml:"rotation_interval"` // 时间滚动间隔，如 24h；留空或 0 则仅按大小滚动
}

// Load 从 yaml 文件加载配置。文件中缺省的字段会使用默认值。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 先填默认值，yaml 中未出现的字段不会被覆盖
	cfg := &Config{
		Log: LogConfig{
			Level:            "info",
			Dir:              "logs",
			Console:          true,
			Format:           "text",
			Buffer:           4096,
			MaxSize:          100,
			MaxBackups:       30,
			MaxAge:           0,
			Compress:         "gzip",
			RotationInterval: "24h",
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return cfg, nil
}
