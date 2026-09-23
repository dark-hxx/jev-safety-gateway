#Requires -Version 5.1
<#
.SYNOPSIS
    停止由 start-gateway.ps1 启动的 JEV 安全网关。

.DESCRIPTION
    优先使用包内 gateway.pid 记录的进程号；pid 文件缺失时，
    回退为查找可执行文件路径与包内 gateway.exe 一致的 gateway 进程。
#>
[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }

$packageDir = $PSScriptRoot
$pidFile = Join-Path $packageDir 'gateway.pid'
$exePath = Join-Path $packageDir 'gateway.exe'
$targetPid = $null

if (Test-Path -LiteralPath $pidFile) {
    $raw = (Get-Content -LiteralPath $pidFile -Raw).Trim()
    if ($raw -match '^\d+$') { $targetPid = [int]$raw }
}

if ($null -eq $targetPid) {
    $candidate = Get-Process -Name 'gateway' -ErrorAction SilentlyContinue |
        Where-Object { try { $_.Path -eq $exePath } catch { $false } } |
        Select-Object -First 1
    if ($candidate) { $targetPid = $candidate.Id }
}

if ($null -eq $targetPid) {
    Write-Host '未找到正在运行的网关进程（没有 gateway.pid，也没有路径匹配的 gateway 进程）。' -ForegroundColor Yellow
    exit 0
}

$process = Get-Process -Id $targetPid -ErrorAction SilentlyContinue
if (-not $process) {
    Write-Host "PID $targetPid 对应的进程已不存在，清理 gateway.pid。" -ForegroundColor Yellow
    Remove-Item -LiteralPath $pidFile -Force -ErrorAction SilentlyContinue
    exit 0
}

Write-Host "正在停止网关进程（PID $targetPid）..."
Stop-Process -Id $targetPid -Force
Remove-Item -LiteralPath $pidFile -Force -ErrorAction SilentlyContinue
Write-Host '已停止。' -ForegroundColor Green
exit 0
