package main

import (
	_ "embed"
	"log"
	"os"
	"runtime/debug"

	"github.com/fatih/color"
	"github.com/joho/godotenv"

	"devops-system/internal/cmd"
)

var version string

// banner 在编译期由 //go:embed 从 banner.txt 嵌入，运行时无需读文件；
// 镜像里也不必携带 banner.txt（多阶段构建只 COPY 二进制即可）。
//
//go:embed banner.txt
var banner string

// ui 用于打印 banner 与版本信息这类"程序输出"。
// 独立 log 实例（Flags=0、输出到 color.Output），多行 banner 不被时间前缀顶歪，
// 终端正常显色、重定向自动去转义。
var ui = log.New(color.Output, "", 0)

func main() {
	// 提前加载 config/.env：resolveVersion 依赖 APP_VERSION，
	// 而 config.Init（cobra.OnInitialize）要等到 cmd.Execute 才执行。
	// godotenv.Load 不覆盖已设置的 OS 环境变量，CI/容器注入安全。
	_ = godotenv.Load("config/.env")

	ver := resolveVersion()

	ui.Print(banner)

	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	ui.Printf(" %s: %s\n", cyan("devops-system Service Version"), green(ver))

	cmd.Execute(ver)
}

// resolveVersion 按优先级获取服务版本：
//  1. 编译期 -ldflags "-X main.version=xxx" 注入（CI/CD，最高优先）
//  2. .env 文件 APP_VERSION（本地 go run 开发，显式指定版本号）
//  3. 模块版本（go install 从模块代理安装时）
//  4. vcs.revision（git commit 短哈希 + dirty 标记）
//  5. 兜底 unknown
func resolveVersion() string {
	if version != "" {
		return version
	}

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
					modified = "-dirty"
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
