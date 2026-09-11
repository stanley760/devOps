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
	Grpc  GrpcConfig          `mapstructure:"grpc"`
	Log   config.LogConfig    `mapstructure:"log"`
	Mysql MysqlConfig         `mapstructure:"mysql"`
	IDGen IDGenConfig         `mapstructure:"idgen"`
	Etcd  EtcdConfig          `mapstructure:"etcd"`
}

// GrpcConfig gRPC 服务配置
type GrpcConfig struct {
	Listen     string `mapstructure:"listen"`     // 监听地址，如 ":9090"
	Reflection bool   `mapstructure:"reflection"` // 是否开启 reflection（dev 调试用）
}

// MysqlConfig MySQL 连接配置（t_leaf_alloc 号段表）
type MysqlConfig struct {
	Host              string        `mapstructure:"host"`
	Port              int           `mapstructure:"port"`
	Username          string        `mapstructure:"username"`
	Password          string        `mapstructure:"password"`
	Database          string        `mapstructure:"database"`
	Charset           string        `mapstructure:"charset"`
	MaxIdleConns      int           `mapstructure:"max_idle_conns"`
	MaxOpenConns      int           `mapstructure:"max_open_conns"`
	ConnMaxLifetime   time.Duration `mapstructure:"conn_max_lifetime"`
	LogLevel          string        `mapstructure:"log_level"` // silent|error|warn|info
}

// IDGenConfig 发号器配置
type IDGenConfig struct {
	Mode     string `mapstructure:"mode"`      // segment（号段模式，默认）| snowflake
	Step     int    `mapstructure:"step"`      // segment：号段长度
	LowWater float64 `mapstructure:"low_water"` // segment：剩余比例低于该值时异步预加载
}

// EtcdConfig etcd 连接配置（仅 snowflake 模式使用）
type EtcdConfig struct {
	Endpoints   []string      `mapstructure:"endpoints"`
	DialTimeout time.Duration `mapstructure:"dial_timeout"`
	LeaseTTL    time.Duration `mapstructure:"lease_ttl"`     // 槽位持有 lease 的 TTL（宕机后槽位释放延迟）
	WorkerCache string        `mapstructure:"worker_cache"` // 本地槽位缓存文件（优先复用原槽）
}
