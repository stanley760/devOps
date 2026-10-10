// Package config 定义 devops-id 服务的配置结构。
//
// 加载机制复用 devops-infra/config（基础 + APP_ENV 环境覆盖合并），
// 本包只定义自己的根 Config 与专有配置段。
package config

import (
	"time"

	"devops-infra/config"
)

// Config devops-id 服务根配置，对应 config/config.yaml 顶层结构
type Config struct {
	Grpc          GrpcConfig          `mapstructure:"grpc"`
	Log           config.LogConfig    `mapstructure:"log"`
	IDGen         IDGenConfig         `mapstructure:"idgen"`
	Etcd          config.EtcdConfig   `mapstructure:"etcd"`
	Nacos         config.NacosConfig  `mapstructure:"nacos"`
	Observability ObservabilityConfig `mapstructure:"observability"`
}

// GrpcConfig gRPC 服务配置
type GrpcConfig struct {
	Listen     string `mapstructure:"listen"`     // 监听地址，如 ":9990"
	Reflection bool   `mapstructure:"reflection"` // 是否开启 reflection（dev 调试用）
}

// IDGenConfig 发号器配置
type IDGenConfig struct {
	LeafName    string `mapstructure:"leaf_name"`
	WorkerCache string `mapstructure:"worker_cache"`

	// AllowLocalFallback 显式开启 etcd 不可用时的本地降级发号。
	// 默认 false（fail-closed）：etcd 故障时拒绝发号，宁可拒发不可重号。
	AllowLocalFallback bool `mapstructure:"allow_local_fallback"`

	// MaxBizTagLen biz_tag 最大字节长度（默认 64）。
	MaxBizTagLen int `mapstructure:"max_biz_tag_len"`
	// MaxGenerators 并发 generator 数上限（默认 1024）。
	MaxGenerators int `mapstructure:"max_generators"`
	// MaxBatchCount 单次批量取号上限（默认 100000）。
	MaxBatchCount int `mapstructure:"max_batch_count"`

	// WatermarkSafetyMargin 水位预留窗口（默认 5s），须大于 WatermarkPersistInterval。
	WatermarkSafetyMargin time.Duration `mapstructure:"watermark_safety_margin"`
	// WatermarkPersistInterval 水位持久化间隔（默认 3s）。
	WatermarkPersistInterval time.Duration `mapstructure:"watermark_persist_interval"`
	// WatermarkMaxWait 重启等待水位窗口的最长时长（默认 30s）。
	WatermarkMaxWait time.Duration `mapstructure:"watermark_max_wait"`
}

// ObservabilityConfig 服务级观测配置；ServiceName 由 nacos.registry.service_name 派生。
type ObservabilityConfig struct {
	OTLPEndpoint   string  `mapstructure:"otlp_endpoint"`
	SamplerRatio   float64 `mapstructure:"sampler_ratio"`
	MetricsEnabled bool    `mapstructure:"metrics_enabled"`
	MetricsListen  string  `mapstructure:"metrics_listen"`
}
