package utils

import (
	"os"
	"runtime/debug"
)



// resolveVersion 按优先级获取服务版本：
//  1. .env 文件 APP_VERSION（本地 go run 开发，显式指定版本号）
//  2. 模块版本（go install 从模块代理安装时）
//  3. vcs.revision（git commit 短哈希 + dirty 标记）
//  4. 兜底 unknown
func ResolveVersion() string {

	if v := os.Getenv("APP_VERSION"); v != "" {
		return v
	}
	
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}

		var revision, modified string
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.modified":
				if s.Value == "true" {
					modified = "_dirty"
				}
			}
		}
		if revision != "" {
			if len(revision) > 7 {
				revision = revision[:7]
			}
			return revision + modified
		}
	}

	return "unknown"
}
