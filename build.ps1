# 在 Windows 上建置 dist\MantisWatcher.exe（需要 Go 1.22 以上：https://go.dev/dl/）
param([string]$Version = "2.0.0")
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"

New-Item -ItemType Directory -Force -Path dist | Out-Null
go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$Version" -o dist\MantisWatcher.exe .\cmd\mantis-watcher
if ($LASTEXITCODE -ne 0) { throw "build failed" }
Write-Host "完成：dist\MantisWatcher.exe"
