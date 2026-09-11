// Package config 定义应用配置结构，并封装 viper 的初始化与读取。
//
// 职责划分：
//   - Init(path) 加载基础配置，再按 APP_ENV 合并环境差异，开启热更新
//   - Load()     把配置反序列化到结构体
//
// 配置组织方式（基础 + 覆盖）：
//
//	config/config.yaml   基础配置，写全公共项
//	config/dev.yaml      仅覆盖与基础配置不同的字段
//	config/prod.yaml     同上
//	config/test.yaml     同上
//
// 使用方式（cmd 层）：
//
//	cobra.OnInitialize(func() { config.Init(confFile) })
//	cfg, err := config.Load()
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// EnvAppEnv 用于选择环境的环境变量名，取值如 dev / prod / test。
//
// 注意这与「用环境变量覆盖单个配置值」是两回事：
// 这里只读一个变量来决定加载哪个配置文件，不需要逐项绑定，
// 因此不会引入 DEVOPS_LOG_LEVEL 那种重复映射。
const EnvAppEnv = "APP_ENV"

// 记录实际加载来源，供 Files() 展示。
// viper.ConfigFileUsed() 在 MergeInConfig 后只返回最后那个文件，
// 会让人误以为基础配置没生效，所以这里自己维护。
var (
	baseFile string
	envFile  string
)

// Init 加载配置：先读基础文件，再按 APP_ENV 合并同名环境文件。
// 出错时返回 error，由调用方决定如何处理（cmd 层目前选择 panic）。
func Init(path string) error {
	viper.SetConfigFile(path)

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("读取基础配置 %s 失败: %w", path, err)
	}
	baseFile = path

	if err := mergeEnvConfig(path); err != nil {
		return err
	}

	// 监听配置文件变化，修改后自动重载
	viper.WatchConfig()

	return nil
}

// mergeEnvConfig 按 APP_ENV 合并环境差异配置。
// APP_ENV 为空则跳过，只使用基础配置。
func mergeEnvConfig(basePath string) error {
	env := strings.TrimSpace(os.Getenv(EnvAppEnv))
	if env == "" {
		return nil
	}

	// 环境文件与基础配置放在同一目录
	dir := filepath.Dir(basePath)
	viper.SetConfigName(env)
	viper.AddConfigPath(dir)

	// MergeInConfig 只覆盖环境文件中显式写出的字段，其余保留基础配置的值
	if err := viper.MergeInConfig(); err != nil {
		return fmt.Errorf("合并环境配置 %s.yaml 失败（目录: %s）: %w", env, dir, err)
	}
	envFile = filepath.Join(dir, env+".yaml")

	return nil
}

// Env 返回当前生效的环境名，未设置时为空字符串。
func Env() string {
	return strings.TrimSpace(os.Getenv(EnvAppEnv))
}

// Files 返回实际加载的配置文件来源。
// 未设置 APP_ENV 时只有基础文件；否则形如 "config/config.yaml + prod.yaml"。
func Files() string {
	if envFile == "" {
		return baseFile
	}
	return baseFile + " + " + filepath.Base(envFile)
}

// Load 读取全部配置，反序列化到 Config。
// 配置来自基础文件与环境文件的合并结果，未配置的字段为零值。
func Load() (Config, error) {
	var cfg Config

	if err := viper.Unmarshal(&cfg); err != nil {
		return cfg, fmt.Errorf("解析配置失败: %w", err)
	}

	return cfg, nil
}
