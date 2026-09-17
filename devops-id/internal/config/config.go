// Package config 定义 devops-id 服务的配置结构。
//
// 加载机制复用 devops-infra/config（基础 + APP_ENV 环境覆盖合并），
// 本包只定义自己的根 Config 与专有配置段。
package config

import (
	"devops-infra/config"
)

// Config devops-id 服务根配置，对应 config/config.yaml 顶层结构
type Config struct {
	Grpc  GrpcConfig         `mapstructure:"grpc"`
	Log   config.LogConfig   `mapstructure:"log"`
	IDGen IDGenConfig        `mapstructure:"idgen"`
	Etcd  config.EtcdConfig  `mapstructure:"etcd"`
	Nacos config.NacosConfig `mapstructure:"nacos"`
}

// GrpcConfig gRPC 服务配置
type GrpcConfig struct {
	Listen     string `mapstructure:"listen"`     // 监听地址，如 ":9090"
	Reflection bool   `mapstructure:"reflection"` // 是否开启 reflection（dev 调试用）
}

// IDGenConfig 发号器配置
type IDGenConfig struct {
	LeafName    string `mapstructure:"leaf_name"`
	WorkerCache string `mapstructure:"worker_cache"`
}
