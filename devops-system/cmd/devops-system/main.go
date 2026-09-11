package main

import (
	"log"
	"runtime/debug"

	"github.com/fatih/color"

	"devops-system/cmd"
)

var version string

// ui 用于打印 banner 与版本信息这类"程序输出"。
// 独立 log 实例（Flags=0、输出到 color.Output），多行 banner 不被时间前缀顶歪，
// 终端正常显色、重定向自动去转义。
var ui = log.New(color.Output, "", 0)

func main() {
	ver := resolveVersion()

	ui.Println("     _                           ")
	ui.Println("  __| | _____   _____  _ __  ___ ")
	ui.Println(" / _` |/ _ \\ \\ / / _ \\| '_ \\/ __|")
	ui.Println("| (_| |  __/\\ V / (_) | |_) \\__ \\")
	ui.Println(" \\__,_|\\___| \\_/ \\___/| .__/|___/")
	ui.Println("                      |_|        ")
	ui.Println("  ____                    _   _             ")
	ui.Println(" / ___|_ __ ___  ___  __| | | | ___   ___  ")
	ui.Println("| |   | '__/ _ \\/ _ \\/ _` | | |/ _ \\ / _ \\")
	ui.Println("| |___| | |  __/  __/ (_| |_| | (_) |  __/")
	ui.Println(" \\____|_|  \\___|\\___|\\__,_|_|_|\\___/ \\___|")

	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	ui.Printf(" %s: %s", cyan("devops-system Service Version"), green(ver))

	cmd.Execute(ver)
}

// resolveVersion 按优先级获取服务版本：
//  1. 编译期 -ldflags "-X main.version=xxx" 注入
//  2. 模块版本（从模块代理安装时）
//  3. vcs.revision（git commit 短哈希 + dirty 标记）
//  4. 兜底 unknown
func resolveVersion() string {
	if version != "" {
		return version
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
