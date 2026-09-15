// Package config 定义应用配置结构，并封装 viper 的初始化与读取。
//
// 职责划分：
//   - Init(path) 先加载 bootstrap.yaml，再按 APP_ENV 合并模块环境覆盖文件，开启热更新
//   - LoadInto() 把配置反序列化到结构体
//
// 配置组织方式（bootstrap + 环境覆盖）：
//
//	config/bootstrap.yaml         基础配置，写全公共项
//	config/<module>-<env>.yaml    仅覆盖 bootstrap 中不同的字段
//	例如：devops-id-dev.yaml、devops-system-prod.yaml
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

	"github.com/spf13/viper"
)

// EnvAppEnv 用于选择环境的环境变量名，取值如 dev / prod / test。
const EnvAppEnv = "APP_ENV"

var (
	baseFile string
	envFile  string
)

// Init 加载 bootstrap 基础配置，再按 APP_ENV 合并模块环境差异文件。
func Init(path string) error {
	if path == "" {
		path = filepath.Join("config", "bootstrap.yaml")
	}
	viper.Reset()
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("读取基础配置 %s 失败: %w", path, err)
	}
	baseFile = path
	if err := mergeEnvConfig(path); err != nil {
		return err
	}
	viper.WatchConfig()
	return nil
}

// mergeEnvConfig 按 APP_ENV 加载模块级环境配置，文件名格式：<module>-<env>.yaml。
func mergeEnvConfig(basePath string) error {
	env := Env()
	configDir := filepath.Dir(basePath)
	module := filepath.Base(configDir)
	if module == "config" || module == "." || module == string(filepath.Separator) {
		parent := filepath.Base(filepath.Dir(configDir))
		if parent != "." && parent != string(filepath.Separator) {
			module = parent
		} else if wd, err := os.Getwd(); err == nil {
			module = filepath.Base(wd)
		}
	}
	candidate := filepath.Join(configDir, module+"-"+env+".yaml")
	if _, err := os.Stat(candidate); err != nil {
		legacy := filepath.Join(configDir, env+".yaml")
		if _, legacyErr := os.Stat(legacy); legacyErr == nil {
			candidate = legacy
		} else {
			return nil
		}
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
	if envFile == "" {
		return baseFile
	}
	return baseFile + " + " + filepath.Base(envFile)
}

// LoadInto 把合并后的配置反序列化到调用方定义的结构体。
func LoadInto(target any) error {
	if err := viper.Unmarshal(target); err != nil {
		return fmt.Errorf("解析配置失败: %w", err)
	}
	return nil
}

// ReplaceWithYAML replaces the active Viper configuration with a complete YAML
// document fetched from an external configuration center.
func ReplaceWithYAML(content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("外部配置内容为空")
	}
	viper.Reset()
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(strings.NewReader(content)); err != nil {
		return fmt.Errorf("解析外部 YAML 配置失败: %w", err)
	}
	return nil
}
