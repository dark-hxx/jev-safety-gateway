#Requires -Version 5.1
<#
.SYNOPSIS
    启动 JEV 安全网关（本地 test 包内使用）。

.DESCRIPTION
    ⚠️ 这是打包模板，不是仓库脚本：build-local-test.ps1 会把它复制进测试包并改名为
    start-jev-safety-gateway.ps1。它必须和 jev-safety-gateway.exe 同目录才能运行，在仓库里直接运行只会报错，
    仓库内的入口是 scripts\start-local-test.ps1。

    读取同目录 .env 作为一次性初始化配置，数据库默认放在包内 data\jev-safety-gateway.db，
    代理监听 :8080，管理控制台监听 127.0.0.1:8081（只绑本机），并记录 jev-safety-gateway.pid 供停止脚本使用。

.PARAMETER ProxyAddr
    代理监听地址，默认 :8080（已设置 JEV_PROXY_ADDR 环境变量时不覆盖）。

.PARAMETER AdminAddr
    管理控制台监听地址，默认 127.0.0.1:8081（已设置 JEV_ADMIN_ADDR 环境变量时不覆盖）。
    绑本机是有意的：控制台除自身登录外没有别的保护，绑 0.0.0.0 就等于放到公网。

.PARAMETER DbPath
    数据库路径，默认包内 data\jev-safety-gateway.db（已设置 JEV_DB_PATH 环境变量时不覆盖）。
#>
[CmdletBinding()]
param(
    [string]$ProxyAddr = ':8080',
    [string]$AdminAddr = '127.0.0.1:8081',
    [string]$DbPath = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }

$packageDir = $PSScriptRoot
$exePath = Join-Path $packageDir 'jev-safety-gateway.exe'
if (-not (Test-Path -LiteralPath $exePath)) {
    Write-Host "错误：未找到 $exePath" -ForegroundColor Red
    Write-Host 'scripts\template-*.ps1 是打包模板，必须和 jev-safety-gateway.exe 放在同一目录才能运行；' -ForegroundColor Yellow
    Write-Host 'build-local-test.ps1 会把它们复制进测试包。请在仓库根目录改用：' -ForegroundColor Yellow
    Write-Host '  .\scripts\start-local-test.ps1      # 构建并启动（推荐）' -ForegroundColor Yellow
    Write-Host '  .\scripts\build-local-test.ps1      # 只构建，再运行测试包内的 start-jev-safety-gateway.ps1' -ForegroundColor Yellow
    exit 1
}

$envFile = Join-Path $packageDir '.env'
if (Test-Path -LiteralPath $envFile) {
    Write-Host "加载配置：$envFile"
    foreach ($line in Get-Content -LiteralPath $envFile -Encoding UTF8) {
        $trimmed = $line.Trim()
        if ($trimmed.Length -eq 0 -or $trimmed.StartsWith('#')) { continue }
        $separator = $trimmed.IndexOf('=')
        if ($separator -lt 1) { continue }
        $name = $trimmed.Substring(0, $separator).Trim()
        $value = $trimmed.Substring($separator + 1).Trim()
        if ($value.Length -eq 0) { continue }
        [Environment]::SetEnvironmentVariable($name, $value, 'Process')
    }
}

if (-not $env:JEV_PROXY_ADDR) { $env:JEV_PROXY_ADDR = $ProxyAddr }
if (-not $env:JEV_ADMIN_ADDR) { $env:JEV_ADMIN_ADDR = $AdminAddr }
if (-not $env:JEV_DB_PATH) {
    $env:JEV_DB_PATH = if ($DbPath) { $DbPath } else { Join-Path $packageDir 'data\jev-safety-gateway.db' }
}
if (-not [System.IO.Path]::IsPathRooted($env:JEV_DB_PATH)) {
    $env:JEV_DB_PATH = Join-Path $packageDir $env:JEV_DB_PATH
}

$dbDir = Split-Path -Parent $env:JEV_DB_PATH
if ($dbDir -and -not (Test-Path -LiteralPath $dbDir)) {
    New-Item -ItemType Directory -Force -Path $dbDir | Out-Null
}

Write-Host "代理监听：$env:JEV_PROXY_ADDR"
Write-Host "控制台：  $env:JEV_ADMIN_ADDR  →  http://127.0.0.1:8081"
Write-Host "数据库：  $env:JEV_DB_PATH"
Write-Host '按 Ctrl+C 结束，或在另一个窗口运行 stop-jev-safety-gateway.ps1。'
Write-Host ''

$process = Start-Process -FilePath $exePath -WorkingDirectory $packageDir -NoNewWindow -PassThru
$pidFile = Join-Path $packageDir 'jev-safety-gateway.pid'
Set-Content -LiteralPath $pidFile -Value $process.Id -Encoding ASCII
Write-Host "网关已启动（PID $($process.Id)）。"

$exitCode = 0
try {
    Wait-Process -Id $process.Id
    try { $exitCode = [int]$process.ExitCode } catch { $exitCode = 0 }
} finally {
    Remove-Item -LiteralPath $pidFile -Force -ErrorAction SilentlyContinue
}

if ($exitCode -ne 0) {
    Write-Host "网关异常退出（退出码 $exitCode）。常见原因：端口被占用、上游地址或 JEV 配置无效、数据库路径不可写。" -ForegroundColor Red
    exit $exitCode
}

Write-Host '网关已退出。'
exit 0
