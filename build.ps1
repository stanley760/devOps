$ErrorActionPreference = "Stop"

$version = (git describe --tags --always --dirty).Trim()

$targets = @(
    @{ Module = "devops-infra";  Out = "" },  # 纯库，无产物
    @{ Module = "devops-id";     Out = "bin/devops-id.exe" },
    @{ Module = "devops-system"; Out = "bin/devops-system.exe" }
)

Write-Host "Building devOps $version"

foreach ($t in $targets) {
    if ($t.Out -eq "") { continue }
    Write-Host "  $($t.Module) -> $($t.Out)"
    go build -C $t.Module -trimpath -buildvcs=true `
        -ldflags "-X main.version=$version" -o "../$($t.Out)" ./cmd/$($t.Module)
    if ($LASTEXITCODE -ne 0) { throw "build $($t.Module) failed" }
}

Write-Host "Built devops-id, devops-system -> bin/"
