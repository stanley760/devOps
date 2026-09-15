package config

import "time"

type NacosConfig struct {
	Enabled   bool          `mapstructure:"enabled"`   // 总开关，false 时全包 no-op
	Endpoints []string      `mapstructure:"endpoints"` // ["127.0.0.1:8848"]
	Namespace string        `mapstructure:"namespace"` // devops
	Group     string        `mapstructure:"group"`     // DEVOPS_GROUP
	Username  string        `mapstructure:"username"`
	Password  string        `mapstructure:"password"`
	Timeout   time.Duration `mapstructure:"timeout"`

	ConfigCenter ConfigCenterConfig `mapstructure:"config"`   // data_ids/required
	Registry     RegistryConfig     `mapstructure:"registry"` // service_name/port/weight
}

type ConfigCenterConfig struct {
	DataIDs  []string      `mapstructure:"data_ids"`
	Group    string        `mapstructure:"group"`
	Timeout  time.Duration `mapstructure:"timeout"`
	Required bool          `mapstructure:"required"`
}

type RegistryConfig struct {
	ServiceName string            `mapstructure:"service_name"`
	Cluster     string            `mapstructure:"cluster"`
	Weight      float64           `mapstructure:"weight"`
	Meta        map[string]string `mapstructure:"meta"`
}
