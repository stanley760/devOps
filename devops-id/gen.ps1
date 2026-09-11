# 由 proto/idgen.proto 生成 Go 代码。
# 产物提交入库（pkg/idgenpb），调用方无需安装工具链。
# 用法：仓库任意位置执行 powershell -File devops-id/gen.ps1
$ErrorActionPreference = "Stop"

$root = $PSScriptRoot   # devops-id 模块根

# GOPATH 解析：优先环境变量，兜底 go env（取形如 X:\ 的最后一行，
# 防止 shim 往 stdout 混入告警文本）
$gopath = $env:GOPATH
if (-not $gopath) {
    $gopath = (go env GOPATH) -split "`r?`n" |
        Where-Object { $_ -match '^[A-Za-z]:[\\/]' } | Select-Object -Last 1
}
if (-not $gopath) { throw "无法解析 GOPATH" }

$gobin = Join-Path $gopath.Trim() "bin"

# 显式指定 plugin 完整路径，不依赖 PATH（go env 输出被污染时 PATH 前缀会失效）
$pluginGo     = Join-Path $gobin "protoc-gen-go.exe"
$pluginGoGrpc = Join-Path $gobin "protoc-gen-go-grpc.exe"
if (-not (Test-Path $pluginGo))     { throw "缺少 $pluginGo，请执行: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest" }
if (-not (Test-Path $pluginGoGrpc)) { throw "缺少 $pluginGoGrpc，请执行: go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest" }

New-Item -ItemType Directory -Force -Path "$root\pkg\idgenpb" | Out-Null

protoc `
    --plugin=protoc-gen-go="$pluginGo" `
    --plugin=protoc-gen-go-grpc="$pluginGoGrpc" `
    --proto_path="$root\proto" `
    --go_out="$root\pkg\idgenpb" --go_opt=paths=source_relative `
    --go-grpc_out="$root\pkg\idgenpb" --go-grpc_opt=paths=source_relative `
    "$root\proto\idgen.proto"

if ($LASTEXITCODE -ne 0) { throw "protoc generate failed" }
Write-Host "generated -> pkg/idgenpb"
