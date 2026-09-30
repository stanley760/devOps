param(
    [ValidateSet("up", "app", "stop", "down", "restart", "sync", "logs", "ps", "migrate")]
    [string]$Command = "up"
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$ComposeFile = Join-Path $PSScriptRoot "docker-compose-dev.yml"


function Invoke-Compose {
    param([string[]]$Arguments)
    & Docker compose -f $ComposeFile @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Docker Compose command failed (exit code $LASTEXITCODE)"
    }
}

# 在独立 cmd 窗口内 `go run` 启动一个业务服务，关闭窗口即停止该服务。
function Start-LocalService {
    param([string]$Name, [string]$Dir)
    $WorkDir = Join-Path $Root $Dir
    if (-not (Test-Path (Join-Path $WorkDir "go.mod"))) {
        throw "未找到 $WorkDir\go.mod，无法启动 $Name"
    }
    Start-Process -FilePath "cmd.exe" -WorkingDirectory $WorkDir -ArgumentList "/k", "title $Name && go run . server"
    Write-Host "  $Name 已在新窗口启动（关闭窗口即停止）"
} 
function Start-LocalServices {
    Write-Host ''
    Write-Host '正在启动本地业务服务...'
    Start-LocalService -Name "devops-id" -Dir "devops-id"
    # devops-system 依赖 devops-id 发号，等它先就绪
    Start-Sleep -Seconds 3
    Start-LocalService -Name "devops-system" -Dir "devops-system"
    Write-Host ''
    Write-Host '首次运行或 schema 变更后，请先执行数据库迁移：'
    Write-Host '  .\deploy\dev.ps1 -Command migrate'
    Write-Host '首次使用前需复制 .env.example 为 .env 并填入局域网 IP（NACOS_REGISTER_IP）。'
}

Push-Location $Root
try {
    switch ($Command) {
        "up" {
            # 启动中间件 + 网关 + 可观测栈。higress-init 会在 higress 前自动灌入
            # deploy/higress 的配置（首次/down 后）。
            Invoke-Compose @("up", "-d", "mysql", "redis", "nacos", "etcd", "higress")
            Invoke-Compose @("up", "-d", "jaeger", "prometheus", "alertmanager", "grafana")
            Write-Host ''
            Write-Host '中间件与可观测栈已启动。'
            Start-LocalServices 
        }
        "app" {
            # 仅启动/补起本地业务服务，不动容器
            Start-LocalServices
        }
        "stop" {
            # 停止本地业务服务窗口由关闭对应窗口完成；此处仅提示（容器不受影响）
            Write-Host '本地业务服务运行在独立的 cmd 窗口中，请直接关闭 devops-id / devops-system 窗口停止。'
            Write-Host '如需停止容器，请执行：.\deploy\dev.ps1 -Command down'
        }
        "migrate" {
            # 迁移用一次性容器执行（devops-system 服务在 app profile 下）
            Invoke-Compose @("build", "devops-system")
            Invoke-Compose @("--profile", "app", "run", "--rm", "devops-system", "migrate")
        }
        "down" { Invoke-Compose @("down") }
        "restart" { Invoke-Compose @("restart", "mysql", "redis", "nacos", "etcd", "higress") }
        "sync" {
            # 修改 deploy/higress 下 yaml 后的显式同步，兼治 envoy OOM 毒配置：
            #   停网关 → 重灌 git 配置 → 删 higress-config 的 inclusionRegexps（新卷首启
            #   自动再生；CRLF 行尾需先 tr 规范化；envoy 因此被 OOM 杀且 supervisord 不拉起）
            #   → 起网关。新卷首次 up 后建议跑一次本命令
            Invoke-Compose @("stop", "higress")
            Invoke-Compose @("up", "-d", "--force-recreate", "higress-init")
            Invoke-Compose @("run", "--rm", "--entrypoint", "sh", "higress-init", "-c", 'if [ -f /data/configmaps/higress-config.yaml ]; then tr -d "\r" < /data/configmaps/higress-config.yaml > /tmp/hc && mv /tmp/hc /data/configmaps/higress-config.yaml && sed -i "/^      proxyStatsMatcher:$/,/^        - \.\*$/d" /data/configmaps/higress-config.yaml; fi')
            Invoke-Compose @("start", "higress")
        }
        "logs" { Invoke-Compose @("logs", "-f") }
        "ps" { Invoke-Compose @("ps") }
    }
}
finally {
    Pop-Location
}
