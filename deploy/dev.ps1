param(
    [ValidateSet("up", "app", "stop", "down", "restart", "sync", "logs", "ps", "migrate", "watch")]
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

# —— watch：监测本地业务服务（存活+就绪）。存活失败自动重启（退避+熔断），就绪失败仅告警 ——
# 端点为 dev 配置硬编码：devops-id 存活探针走 metrics :9991（若关闭 metrics 会被误判死亡，
# 由熔断兜底）；devops-system /readyz 内含 mysql/redis/devops-id 三项依赖检查。
$WatchIntervalSec = 10
$WatchBackoffSec  = 60
$WatchBreakerMax  = 5

function Invoke-HttpProbe {
    # Windows 自带 curl.exe 探测，返回 HTTP 状态码（连接失败为 000）。
    # 不用 Invoke-WebRequest：PS5.1 下非 2xx 抛异常且取响应体繁琐；不用 Test-NetConnection：太慢。
    param([string]$Url, [string]$BodyFile = "")
    $curlArgs = @("-s", "-m", "3", "-w", "%{http_code}", "-o")
    if ($BodyFile -ne "") { $curlArgs += $BodyFile } else { $curlArgs += "NUL" }
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"   # 2>$null 重定向原生命令 stderr，Stop 模式下会误抛
    try {
        $code = & curl.exe @curlArgs $Url 2>$null
    } catch {
        $code = "000"
    } finally {
        $ErrorActionPreference = $prev
    }
    $code = "$code".Trim()
    if ([string]::IsNullOrWhiteSpace($code)) { $code = "000" }
    return $code
}

function New-WatchSvc {
    param([string]$Name, [string]$Dir, [string]$AliveUrl, [string]$ReadyUrl = "")
    return @{
        Name = $Name; Dir = $Dir; AliveUrl = $AliveUrl; ReadyUrl = $ReadyUrl
        State = "INIT"; New = ""; Detail = ""
        Breaker = 0; Tripped = $false; LastRestart = 0
    }
}

function Update-WatchSvc {
    # 探测一个服务：存活探针决定 UP/DOWN；就绪探针（仅 system）细分出 UNREADY
    param([hashtable]$Svc, [string]$BodyFile)
    if ((Invoke-HttpProbe -Url $Svc.AliveUrl) -ne "200") {
        $Svc.New = "DOWN"; $Svc.Detail = ""; return
    }
    if ($Svc.ReadyUrl -eq "") {
        $Svc.New = "UP"; $Svc.Detail = ""; return
    }
    if ((Invoke-HttpProbe -Url $Svc.ReadyUrl -BodyFile $BodyFile) -eq "200") {
        $Svc.New = "UP"; $Svc.Detail = ""; return
    }
    $Svc.New = "UNREADY"
    $detail = ""
    if (Test-Path $BodyFile) {
        $body = Get-Content $BodyFile -Raw -ErrorAction SilentlyContinue
        if ($body -match '"unready":\[[^\]]*\]') { $detail = $Matches[0] }
    }
    if ($detail -eq "") { $detail = '"unready":[?]' }
    $Svc.Detail = $detail
}

function Write-WatchState {
    param([hashtable]$Svc)
    $ts = Get-Date -Format "HH:mm:ss"
    switch ($Svc.New) {
        "UP"      { Write-Host "[$ts] $($Svc.Name) UP" -ForegroundColor Green }
        "DOWN"    { Write-Host "[$ts] $($Svc.Name) DOWN" -ForegroundColor Red }
        "UNREADY" { Write-Host "[$ts] $($Svc.Name) UNREADY（依赖异常，仅告警不重启）: $($Svc.Detail)" -ForegroundColor Yellow }
        default   { Write-Host "[$ts] $($Svc.Name) $($Svc.New)" }
    }
}

function Restart-WatchSvc {
    # 退避期内返回 backoff；否则（可选）等 $WaitAliveUrl 存活后重启，返回 restarted
    param([hashtable]$Svc, [string]$WaitAliveUrl = "")
    $now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    if ($now - $Svc.LastRestart -lt $WatchBackoffSec) { return "backoff" }
    if ($WaitAliveUrl -ne "" -and (Invoke-HttpProbe -Url $WaitAliveUrl) -ne "200") {
        Write-Host "  等待 devops-id 恢复后再重启 devops-system（最长 30s）..." -ForegroundColor Cyan
        $waited = 0
        while ($waited -lt 30) {
            Start-Sleep -Seconds 3
            $waited += 3
            if ((Invoke-HttpProbe -Url $WaitAliveUrl) -eq "200") { break }
        }
    }
    $Svc.LastRestart = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    Write-Host "  [$(Get-Date -Format 'HH:mm:ss')] 自动重启 $($Svc.Name) ..." -ForegroundColor Cyan
    try {
        Start-LocalService -Name $Svc.Name -Dir $Svc.Dir
    } catch {
        Write-Host "  重启 $($Svc.Name) 失败: $($_.Exception.Message)" -ForegroundColor Red
    }
    return "restarted"
}

function Watch-Services {
    $bodyFile = Join-Path $env:TEMP "devops-watch-readyz.json"
    $id  = New-WatchSvc -Name "devops-id"     -Dir "devops-id"     -AliveUrl "http://127.0.0.1:9991/metrics"
    $sys = New-WatchSvc -Name "devops-system" -Dir "devops-system" -AliveUrl "http://127.0.0.1:9999/healthz" -ReadyUrl "http://127.0.0.1:9999/readyz"

    Write-Host "监测 devops-id（存活 :9991/metrics）与 devops-system（存活 :9999/healthz，就绪 :9999/readyz）"
    Write-Host "每 ${WatchIntervalSec}s 轮询；存活失败自动重启（退避 ${WatchBackoffSec}s，连续 ${WatchBreakerMax} 次熔断）；就绪失败仅告警。Ctrl-C 退出。"
    Write-Host ''

    $cycle = 0
    try {
        while ($true) {
            $cycle++
            Update-WatchSvc $id $bodyFile
            Update-WatchSvc $sys $bodyFile

            # 状态变化即时打印；恢复 UP 复位熔断计数并解除熔断
            if ($id.New -ne $id.State) { Write-WatchState $id; $id.State = $id.New }
            if ($id.New -eq "UP") { $id.Breaker = 0; $id.Tripped = $false }
            if ($sys.New -ne $sys.State) { Write-WatchState $sys; $sys.State = $sys.New }
            if ($sys.New -eq "UP") { $sys.Breaker = 0; $sys.Tripped = $false }

            # 心跳：每 6 轮（约 60s）一行
            if ($cycle % 6 -eq 0) {
                Write-Host "[$(Get-Date -Format 'HH:mm:ss')] 心跳: devops-id=$($id.State) devops-system=$($sys.State)" -ForegroundColor DarkGray
            }

            # 自动重启（仅存活失败触发；就绪失败是容器/依赖问题，重启 Go 服务无意义）
            if ($id.New -eq "DOWN" -and -not $id.Tripped) {
                if ((Restart-WatchSvc $id) -eq "restarted") {
                    $id.Breaker++
                    if ($id.Breaker -ge $WatchBreakerMax) {
                        $id.Tripped = $true
                        Write-Host "[$(Get-Date -Format 'HH:mm:ss')] devops-id 连续 ${WatchBreakerMax} 次重启仍不健康，停止自动重启，请人工检查" -ForegroundColor Red
                    }
                }
            }
            if ($sys.New -eq "DOWN" -and -not $sys.Tripped) {
                # 两服务同时死亡：等 devops-id 恢复后再重启 devops-system
                $waitUrl = ""
                if ($id.New -eq "DOWN") { $waitUrl = $id.AliveUrl }
                if ((Restart-WatchSvc $sys $waitUrl) -eq "restarted") {
                    $sys.Breaker++
                    if ($sys.Breaker -ge $WatchBreakerMax) {
                        $sys.Tripped = $true
                        Write-Host "[$(Get-Date -Format 'HH:mm:ss')] devops-system 连续 ${WatchBreakerMax} 次重启仍不健康，停止自动重启，请人工检查" -ForegroundColor Red
                    }
                }
            }

            Start-Sleep -Seconds $WatchIntervalSec
        }
    }
    finally {
        Write-Host ''
        Write-Host '监测已退出（服务不受影响）' -ForegroundColor Cyan
    }
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
        "watch" {
            # 监测本地业务服务：存活失败自动重启，就绪失败仅告警（容器与网关不在监测范围）
            Watch-Services
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
