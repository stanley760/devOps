package main

import (
	"log"
	"runtime/debug"

	"github.com/fatih/color"

	"devOps/cmd"
)

var (
	version string
)

// ui 用于打印 banner 与版本信息这类"程序输出"。
//
// 用独立的 log 实例而不是全局默认 logger，原因：
//   - Flags 设为 0：log 默认带日期时间前缀，多行 banner 会被顶歪
//   - 输出到 color.Output：它是 os.Stdout 的跨平台包装（Windows 下正确渲染 ANSI）。
//     fatih/color 的 NoColor 判定依据正是 os.Stdout，输出目标与之保持一致，
//     终端里正常显色，重定向到文件时自动去掉转义码
//   - 不影响全局 log：运行日志仍走 slog，日志系统自身的告警走 stderr
var ui = log.New(color.Output, "", 0)

func main() {
	ver := resolveVersion()

	ui.Println("     _                           ")
	ui.Println("  __| | _____   _____  _ __  ___ ")
	ui.Println(" / _` |/ _ \\ \\ / / _ \\| '_ \\/ __|")
	ui.Println("| (_| |  __/\\ V / (_) | |_) \\__ \\")
	ui.Println(" \\__,_|\\___| \\_/ \\___/| .__/|___/")
	ui.Println("                      |_|        ")

	// 使用颜色来突出显示
	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	ui.Printf(" %s: %s", cyan("Service Version"), green(ver))

	// 配置加载、日志初始化都在 cmd 层由 cobra 统一完成
	// 配置来源为 viper（internal/config），路径由 -f/--config-file 指定
	cmd.Execute(ver)
}

// resolveVersion 按优先级获取服务版本：
//  1. 编译期通过 -ldflags "-X main.version=xxx" 注入，发布时可控
//  2. 构建时由 Go 自动嵌入的模块版本（需从模块代理安装才非 (devel)，仓库内构建不适用）
//  3. vcs.revision（git commit 短哈希 + dirty 标记），仓库内 go build / go run 自动嵌入，
//     开发期也能看到可定位的版本
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
