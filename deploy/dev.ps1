param(
    [ValidateSet("up", "down", "restart", "logs", "ps", "migrate")]
    [string]$Command = "up"
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$ComposeFile = Join-Path $PSScriptRoot "docker-compose-dev.yml"

# Resolve-DockerCompose 定位 docker CLI：优先 PATH，其次 Docker Desktop 默认安装目录
# （用户级安装时 resourcesin 不一定在 PATH，直接调用会报「无法将 docker 项识别」）。
function Resolve-DockerCompose {
    $cmd = Get-Command docker -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    $pf86 = [Environment]::GetFolderPath("ProgramFilesX86")
    $candidates = @(
        (Join-Path $env:ProgramFiles "Docker\Docker\resources\bin\docker.exe"),
        (Join-Path $pf86 "Docker\Docker\resources\bin\docker.exe"),
        (Join-Path $env:LOCALAPPDATA "Programs\DockerDesktop\resources\bin\docker.exe")
    ) | Where-Object { Test-Path $_ }
    if (-not $candidates) {
        throw "未找到 docker CLI：请安装 Docker Desktop 并启动，或将 docker.exe 所在目录加入 PATH。"
    }
    return $candidates[0]
}
$Docker = Resolve-DockerCompose

function Invoke-Compose {
    param([string[]]$Arguments)
    & $Docker compose -f $ComposeFile @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Docker Compose command failed (exit code $LASTEXITCODE)"
    }
}

Push-Location $Root
try {
    switch ($Command) {
        "up" {
            # 只启动中间件 + 网关 + 可观测栈；业务服务由本地 go run 启动（见下方说明）
            Invoke-Compose @("up", "-d", "mysql", "redis", "nacos", "etcd", "higress")
            Invoke-Compose @("up", "-d", "jaeger", "prometheus", "grafana")
            Write-Host ""
            Write-Host "中间件与可观测栈已启动。请在宿主机本地启动业务服务："
            Write-Host ""
            Write-Host "  # 终端 1：启动 devops-id（gRPC 发号）"
            Write-Host "  cd devops-id && go run . server"
            Write-Host ""
            Write-Host "  # 终端 2：执行数据库迁移（首次或 schema 变更后）"
            Write-Host "  .\deploy\dev.ps1 -Command migrate"
            Write-Host ""
            Write-Host "  # 终端 3：启动 devops-system（HTTP 服务）"
            Write-Host "  cd devops-system && go run . server"
            Write-Host ""
            Write-Host "  # 首次使用：复制 .env.example 为 .env 并填入局域网 IP"
            Write-Host "  #   copy devops-system\config\.env.example devops-system\config\.env"
            Write-Host "  #   copy devops-id\config\.env.example devops-id\config\.env"
            Write-Host "  # .env 含 APP_ENV=dev 和 NACOS_REGISTER_IP=192.168.x.x"
            Write-Host ""
        }
        "migrate" {
            # 迁移用一次性容器执行（devops-system 服务在 app profile 下）
            Invoke-Compose @("build", "devops-system")
            & $Docker compose -f $ComposeFile --profile app run --rm devops-system migrate
            if ($LASTEXITCODE -ne 0) { throw "migrate failed (exit code $LASTEXITCODE)" }
        }
        "down" { Invoke-Compose @("down") }
        "restart" { Invoke-Compose @("restart", "mysql", "redis", "nacos", "etcd", "higress") }
        "logs" { Invoke-Compose @("logs", "-f") }
        "ps" { Invoke-Compose @("ps") }
    }
}
finally {
    Pop-Location
}
