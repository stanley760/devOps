param(
    [ValidateSet("up", "local-up", "down", "local-down", "restart", "logs", "ps", "migrate")]
    [string]$Command = "up"
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$ComposeFile = Join-Path $PSScriptRoot "docker-compose-dev.yml"

function Invoke-Compose {
    param([string[]]$Arguments)
    & docker compose -f $ComposeFile @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Docker Compose command failed (exit code $LASTEXITCODE)"
    }
}

Push-Location $Root
try {
    switch ($Command) {
        "up" {
            Invoke-Compose @("build")
            Invoke-Compose @("up", "-d", "mysql", "redis", "nacos", "etcd")
            Invoke-Compose @("run", "--rm", "devops-system", "migrate")
            Invoke-Compose @("up", "-d", "devops-id", "devops-system")
            Invoke-Compose @("up", "-d", "higress")
        }
        "local-up" {
            Invoke-Compose @("stop", "devops-system")
            Invoke-Compose @("build", "devops-id")
            Invoke-Compose @("up", "-d", "mysql", "redis", "nacos", "etcd")
            Invoke-Compose @("up", "-d", "devops-id", "higress")
            Write-Host "基础设施和 Higress 已启动。请在 IDE 中启动 devops-system，并设置 APP_ENV=local。"
            Write-Host "本地服务必须设置 NACOS_REGISTER_IP 为宿主机局域网 IPv4 地址。"
        }
        "local-down" {
            Invoke-Compose @("stop", "higress", "devops-id", "mysql", "redis", "nacos", "etcd")
        }
        "migrate" { Invoke-Compose @("run", "--rm", "devops-system", "migrate") }
        "down" { Invoke-Compose @("down") }
        "restart" { Invoke-Compose @("restart", "devops-id", "devops-system", "higress") }
        "logs" { Invoke-Compose @("logs", "-f") }
        "ps" { Invoke-Compose @("ps") }
    }
}
finally {
    Pop-Location
}
