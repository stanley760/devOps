$ErrorActionPreference = "Stop"

$version = (git describe --tags --always --dirty).Trim()
$output = "bin/devops.exe"

Write-Host "Building devOps $version"
go build -trimpath -buildvcs=true -ldflags "-X main.version=$version" -o $output .

Write-Host "Built $output"