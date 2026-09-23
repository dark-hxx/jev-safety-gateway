#Requires -Version 5.1
<#
.SYNOPSIS
    启动 JEV 安全网关（本地 test 包内使用）。

.DESCRIPTION
    读取同目录 .env 作为一次性初始化配置，数据库默认放在包内 data\gateway.db，
    代理监听 :8080，管理控制台监听 :8081，并记录 gateway.pid 供停止脚本使用。

.PARAMETER ProxyAddr
    代理监听地址，默认 :8080（已设置 JEV_PROXY_ADDR 环境变量时不覆盖）。

.PARAMETER AdminAddr
    管理控制台监听地址，默认 :8081（已设置 JEV_ADMIN_ADDR 环境变量时不覆盖）。

.PARAMETER DbPath
    数据库路径，默认包内 data\gateway.db（已设置 JEV_DB_PATH 环境变量时不覆盖）。
#>
[CmdletBinding()]
param(
    [string]$ProxyAddr = ':8080',
    [string]$AdminAddr = ':8081',
    [string]$DbPath = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }

$packageDir = $PSScriptRoot
$exePath = Join-Path $packageDir 'gateway.exe'
if (-not (Test-Path -LiteralPath $exePath)) {
    Write-Host "错误：未找到 $exePath，请确认已完整解压测试包。" -ForegroundColor Red
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
    $env:JEV_DB_PATH = if ($DbPath) { $DbPath } else { Join-Path $packageDir 'data\gateway.db' }
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
Write-Host '按 Ctrl+C 结束，或在另一个窗口运行 stop-gateway.ps1。'
Write-Host ''

$process = Start-Process -FilePath $exePath -WorkingDirectory $packageDir -NoNewWindow -PassThru
$pidFile = Join-Path $packageDir 'gateway.pid'
Set-Content -LiteralPath $pidFile -Value $process.Id -Encoding ASCII
Write-Host "网关已启动（PID $($process.Id)）。"

try {
    Wait-Process -Id $process.Id
} finally {
    Remove-Item -LiteralPath $pidFile -Force -ErrorAction SilentlyContinue
}

Write-Host '网关已退出。'
