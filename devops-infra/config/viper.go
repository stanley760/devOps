// Package config 定义应用配置结构，并封装 viper 的初始化与读取。
//
// 职责划分：
//   - Init(path) 先加载 .env，再加载 bootstrap.yaml，合并服务环境覆盖文件，开启热更新
//   - LoadInto() 把配置反序列化到结构体
//
// 配置组织方式（.env + bootstrap + 环境覆盖）：
//
//	config/.env                   本地环境变量（APP_ENV、NACOS_REGISTER_IP 等），不提交
//	config/bootstrap.yaml         基础配置，写全公共项
//	config/<service>-<env>.yaml   仅覆盖 bootstrap 中不同的字段
//	例如：devops-id-dev.yaml、devops-system-prod.yaml
//
// .env 文件优先级低于 OS 环境变量：已设置的环境变量不会被 .env 覆盖，
// 避免 CI/容器环境的外部注入被本地文件意外篡改。
//
// 使用方式（cmd 层）：
//
//	cobra.OnInitialize(func() { config.Init(confFile) })
//	cfg, err := config.LoadInto(&cfg)
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// EnvAppEnv 用于选择环境的环境变量名，取值如 dev / prod / test。
const EnvAppEnv = "APP_ENV"

// loadDotEnv 加载 bootstrap 同目录下的 .env 文件（存在时），将其中未在 OS 环境
// 中设置的变量注入 os.Environ。文件不存在时静默跳过——生产环境通常不依赖 .env。
func loadDotEnv(basePath string) {
	configDir := filepath.Dir(basePath)
	dotEnv := filepath.Join(configDir, ".env")
	if _, err := os.Stat(dotEnv); err != nil {
		return // .env 不存在，跳过
	}
	// Overload=false：不覆盖已设置的 OS 环境变量（CI/容器注入优先）
	if err := godotenv.Load(dotEnv); err == nil {
		dotEnvFile = ".env"
	}
}

var (
	baseFile       string
	envFile        string
	dotEnvFile     string
	remoteFiles    []string
	bootstrapNacos map[string]any
)

// Init 加载 .env（如有），再读取 bootstrap 基础配置，按 APP_ENV 合并服务环境差异文件。
func Init(path string) error {
	if path == "" {
		path = filepath.Join("config", "bootstrap.yaml")
	}
	// 先加载 .env：将 APP_ENV、NACOS_REGISTER_IP 等本地变量注入进程环境。
	// 已设置的 OS 环境变量优先，.env 不覆盖（CI/容器注入安全）。
	loadDotEnv(path)
	viper.Reset()
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("读取基础配置 %s 失败: %w", path, err)
	}
	baseFile = path
	remoteFiles = nil
	if err := mergeEnvConfig(path); err != nil {
		return err
	}
	bootstrapNacos = viper.GetStringMap("nacos")
	viper.WatchConfig()
	return nil
}

// mergeEnvConfig 按 APP_ENV 加载服务环境配置，文件名格式：<service>-<env>.yaml。
func mergeEnvConfig(basePath string) error {
	env := Env()
	serviceName := ServiceName()
	if serviceName == "" {
		return nil
	}
	configDir := filepath.Dir(basePath)
	candidate := filepath.Join(configDir, serviceName+"-"+env+".yaml")
	if _, err := os.Stat(candidate); err != nil {
		return nil
	}
	envViper := viper.New()
	envViper.SetConfigFile(candidate)
	if err := envViper.ReadInConfig(); err != nil {
		return fmt.Errorf("读取模块环境配置 %s 失败: %w", candidate, err)
	}
	if err := viper.MergeConfigMap(envViper.AllSettings()); err != nil {
		return fmt.Errorf("合并模块环境配置 %s 失败: %w", candidate, err)
	}
	envFile = candidate
	return nil
}

// ServiceName returns the service name used to derive Spring Boot style
// configuration names from nacos.registry.service_name.
func ServiceName() string {
	return strings.TrimSpace(viper.GetString("nacos.registry.service_name"))
}

// Env 返回当前生效的环境名，未设置时默认 dev。
func Env() string {
	env := strings.TrimSpace(os.Getenv(EnvAppEnv))
	if env == "" {
		return "dev"
	}
	return env
}

// Files 返回实际加载的配置文件来源。
func Files() string {
	sources := make([]string, 0, 4)
	if dotEnvFile != "" {
		sources = append(sources, dotEnvFile)
	}
	if baseFile != "" {
		sources = append(sources, baseFile)
	}
	if envFile != "" {
		sources = append(sources, filepath.Base(envFile))
	}
	for _, dataID := range remoteFiles {
		sources = append(sources, "nacos:"+dataID)
	}
	return strings.Join(sources, " + ")
}

// LoadInto 把合并后的配置反序列化到调用方定义的结构体。
func LoadInto(target any) error {
	if err := viper.Unmarshal(target); err != nil {
		return fmt.Errorf("解析配置失败: %w", err)
	}
	return nil
}

// MergeWithYAML incrementally merges a YAML document into the active local
// configuration. Existing values are preserved unless the document overrides
// them, matching Spring Boot's higher-priority remote configuration behavior.
func MergeWithYAML(content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("外部配置内容为空")
	}
	remote := viper.New()
	remote.SetConfigType("yaml")
	if err := remote.ReadConfig(strings.NewReader(content)); err != nil {
		return fmt.Errorf("解析外部 YAML 配置失败: %w", err)
	}
	if err := viper.MergeConfigMap(remote.AllSettings()); err != nil {
		return fmt.Errorf("合并外部 YAML 配置失败: %w", err)
	}
	return nil
}

// MergeRemoteYAML merges one remote configuration document and records its
// data ID as a configuration source.
func MergeRemoteYAML(dataID, content string) error {
	localNacos := bootstrapNacos
	if localNacos == nil {
		localNacos = map[string]any{}
	}
	if err := MergeWithYAML(content); err != nil {
		return err
	}
	viper.Set("nacos", localNacos)
	remoteFiles = append(remoteFiles, dataID)
	return nil
}

// RemoteDataIDs returns configured shared data IDs followed by the Spring
// Boot style service and profile data IDs.
func RemoteDataIDs(configured []string) []string {
	dataIDs := make([]string, 0, len(configured)+2)
	for _, dataID := range configured {
		if trimmed := strings.TrimSpace(dataID); trimmed != "" && !containsString(dataIDs, trimmed) {
			dataIDs = append(dataIDs, trimmed)
		}
	}
	serviceName := ServiceName()
	if serviceName == "" {
		return dataIDs
	}
	mainDataID := serviceName + ".yaml"
	if !containsString(dataIDs, mainDataID) {
		dataIDs = append(dataIDs, mainDataID)
	}
	profileDataID := serviceName + "-" + Env() + ".yaml"
	if !containsString(dataIDs, profileDataID) {
		dataIDs = append(dataIDs, profileDataID)
	}
	return dataIDs
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
