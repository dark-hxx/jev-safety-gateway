#Requires -Version 5.1
<#
.SYNOPSIS
    停止由 start-jev-safety-gateway.ps1 启动的 JEV 安全网关。

.DESCRIPTION
    ⚠️ 这是打包模板，不是仓库脚本：build-local-test.ps1 会把它复制进测试包并改名为
    stop-jev-safety-gateway.ps1。它必须和 jev-safety-gateway.exe 同目录才能运行。

    优先使用包内 jev-safety-gateway.pid 记录的进程号；pid 文件缺失时，
    回退为查找可执行文件路径与包内 jev-safety-gateway.exe 一致的进程。
#>
[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }

$packageDir = $PSScriptRoot
$pidFile = Join-Path $packageDir 'jev-safety-gateway.pid'
$exePath = Join-Path $packageDir 'jev-safety-gateway.exe'
$targetPid = $null

if (Test-Path -LiteralPath $pidFile) {
    $raw = (Get-Content -LiteralPath $pidFile -Raw).Trim()
    if ($raw -match '^\d+$') { $targetPid = [int]$raw }
}

if ($null -eq $targetPid) {
    $candidate = Get-Process -Name 'jev-safety-gateway' -ErrorAction SilentlyContinue |
        Where-Object { try { $_.Path -eq $exePath } catch { $false } } |
        Select-Object -First 1
    if ($candidate) { $targetPid = $candidate.Id }
}

if ($null -eq $targetPid) {
    Write-Host '未找到正在运行的网关进程（没有 jev-safety-gateway.pid，也没有路径匹配的进程）。' -ForegroundColor Yellow
    exit 0
}

$process = Get-Process -Id $targetPid -ErrorAction SilentlyContinue
if (-not $process) {
    Write-Host "PID $targetPid 对应的进程已不存在，清理 jev-safety-gateway.pid。" -ForegroundColor Yellow
    Remove-Item -LiteralPath $pidFile -Force -ErrorAction SilentlyContinue
    exit 0
}

Write-Host "正在停止网关进程（PID $targetPid）..."
Stop-Process -Id $targetPid -Force
Remove-Item -LiteralPath $pidFile -Force -ErrorAction SilentlyContinue
Write-Host '已停止。' -ForegroundColor Green
exit 0
