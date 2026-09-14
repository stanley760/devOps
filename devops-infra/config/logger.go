package config

// LogConfig 日志配置
type LogConfig struct {
	Level   string `mapstructure:"level"`   // debug | info | warn | error
	Dir     string `mapstructure:"dir"`     // 日志文件存放目录
	Console bool   `mapstructure:"console"` // 是否同时输出到控制台
	Format  string `mapstructure:"format"`  // text | json
	Buffer  int    `mapstructure:"buffer"`  // 异步写入缓冲区大小，满则丢弃

	// 以下为文件滚动策略
	MaxSize          int    `mapstructure:"max_size"`          // 单文件上限（MB），写满即切分
	MaxBackups       int    `mapstructure:"max_backups"`       // 保留的历史文件个数，0 表示不限制
	MaxAge           int    `mapstructure:"max_age"`           // 历史文件保留天数，0 表示不限制
	Compress         string `mapstructure:"compress"`          // none | gzip | zstd
	RotationInterval string `mapstructure:"rotation_interval"` // 时间滚动间隔，如 24h；留空或 0 则仅按大小滚动
}
