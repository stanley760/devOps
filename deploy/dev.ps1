param(
    [ValidateSet("up", "down", "restart", "logs", "ps", "migrate")]
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
