# Windows 本地预构建脚本
# 用法:
#   powershell -NoProfile -File deploy/build-local.ps1          # 增量:只建缺失的 goose
#   powershell -NoProfile -File deploy/build-local.ps1 -Force   # 强制重建 goose

param(
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
# PowerShell 7:关掉 "native 命令 stderr 自动触发终结" 的坑
if ($PSVersionTable.PSVersion.Major -ge 7) {
    $PSNativeCommandUseErrorActionPreference = $false
}

$root = Resolve-Path "$PSScriptRoot/.."
Set-Location $root

Write-Host "[build-local] repo  = $root"
Write-Host "[build-local] step1 = cross-build gpt2api"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
New-Item -ItemType Directory -Force deploy/bin | Out-Null
go build -ldflags "-s -w" -o deploy/bin/gpt2api ./cmd/server
if ($LASTEXITCODE -ne 0) { throw "gpt2api build failed" }

$goosePath = Join-Path $root "deploy/bin/goose"
if ($Force -or -not (Test-Path $goosePath)) {
    Write-Host "[build-local] step2 = cross-build goose (tmp module)"
    $tmp = Join-Path $env:TEMP "gpt2api-goose-src"
    if (Test-Path $tmp) { Remove-Item -Recurse -Force $tmp }
    New-Item -ItemType Directory -Force $tmp | Out-Null
    Push-Location $tmp
    try {
        cmd /c "go mod init goose-wrapper >nul 2>&1"
        cmd /c "go get github.com/pressly/goose/v3/cmd/goose@v3.20.0 >nul 2>&1"
        go build -ldflags "-s -w" -o $goosePath github.com/pressly/goose/v3/cmd/goose
        if ($LASTEXITCODE -ne 0) { throw "goose build failed" }
    } finally {
        Pop-Location
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }
} else {
    Write-Host "[build-local] step2 = skip goose (exists). use -Force to rebuild"
}

Write-Host "[build-local] step3 = npm run build (web)"
Push-Location (Join-Path $root "web")
try {
    if (-not (Test-Path node_modules)) {
        npm install --no-audit --no-fund --loglevel=error
        if ($LASTEXITCODE -ne 0) { throw "npm install failed" }
    }
    npm run build
    if ($LASTEXITCODE -ne 0) { throw "npm run build failed" }
} finally {
    Pop-Location
}

Write-Host "[build-local] done. artifacts:"
Get-Item deploy/bin/gpt2api, deploy/bin/goose, web/dist/index.html | Format-Table -AutoSize
