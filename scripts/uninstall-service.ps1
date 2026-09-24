#Requires -Version 5.1
<#
.SYNOPSIS
    卸载 JEV 安全网关 Windows 服务。

.DESCRIPTION
    停止并删除服务注册，移除安装目录中的二进制与旧二进制备份。

    ⚠️ 数据库与日志被有意保留，本脚本从不删除它们。数据库是 SQLite 三件套
    （.db / .db-wal / .db-shm），其中 -wal 可能远大于主库，数据主要落在 WAL 里——
    单独留下主库没有意义，三者必须作为一个整体保护。清理请按脚本末尾打印的命令
    手工执行，并先确认备份副本可读。

.PARAMETER InstallDir
    安装目录，默认 $env:ProgramData\jev-safety-gateway。

.PARAMETER Purge
    连同数据库与日志一并删除。需要显式指定，且会先要求二次确认。
#>
[CmdletBinding()]
param(
    [string]$InstallDir = '',
    [switch]$Purge
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }

$serviceName = 'jev-safety-gateway'

function Write-Step { param([string]$Message) Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Info { param([string]$Message) Write-Host "    $Message" }
function Stop-WithError { param([string]$Message) Write-Host "错误：$Message" -ForegroundColor Red; exit 1 }

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object Security.Principal.WindowsPrincipal($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Stop-WithError '需要管理员权限。请以管理员身份打开 PowerShell 后重试。'
}

if (-not $InstallDir) {
    $InstallDir = Join-Path $env:ProgramData 'jev-safety-gateway'
}
$InstallDir = [System.IO.Path]::GetFullPath($InstallDir)
$dataDir = Join-Path $InstallDir 'data'
$logDir = Join-Path $InstallDir 'logs'

# ---------- 停止并删除服务 ----------
$existing = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if ($existing) {
    Write-Step "停止服务 $serviceName（优雅退出，在途请求会放完）"
    if ($existing.Status -ne 'Stopped') {
        & sc.exe stop $serviceName | Out-Null
        $deadline = (Get-Date).AddSeconds(30)
        while ((Get-Service -Name $serviceName -ErrorAction SilentlyContinue) -and
               (Get-Service -Name $serviceName).Status -ne 'Stopped' -and (Get-Date) -lt $deadline) {
            Start-Sleep -Milliseconds 500
        }
        $svc = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
        if ($svc -and $svc.Status -ne 'Stopped') {
            Stop-WithError '服务在 30 秒内未能停止，已中止卸载。请先排查卡住的请求。'
        }
    }
    Write-Step '删除服务注册'
    & sc.exe delete $serviceName | Out-Null
    if ($LASTEXITCODE -ne 0) { Stop-WithError "sc.exe delete 失败（退出码 $LASTEXITCODE）。" }
} else {
    Write-Info "未找到服务 $serviceName，跳过停止步骤。"
}

# ---------- 移除二进制 ----------
Write-Step '移除二进制（保留 data\ 与 logs\）'
if (Test-Path -LiteralPath $InstallDir) {
    Get-ChildItem -LiteralPath $InstallDir -Filter 'jev-safety-gateway.exe*' -File |
        ForEach-Object {
            Write-Info "删除 $($_.Name)"
            Remove-Item -LiteralPath $_.FullName -Force
        }
} else {
    Write-Info "安装目录不存在：$InstallDir"
}

# ---------- 清理数据（仅在显式 -Purge 时） ----------
if ($Purge) {
    Write-Host ''
    Write-Host '⚠️  -Purge 会永久删除数据库（全部配置、JEV 密钥、审计日志）与日志文件。' -ForegroundColor Yellow
    Write-Host '    删除不进回收站，本机也没有卷影副本，删掉无法找回。' -ForegroundColor Yellow
    Write-Host ''
    $answer = Read-Host '确认删除？输入 DELETE 继续，其它任意输入取消'
    if ($answer -ne 'DELETE') {
        Write-Info '已取消删除，数据库与日志原样保留。'
    } else {
        # 先做一份备份副本再删，避免"以为有备份其实没有"。
        $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
        $backup = Join-Path $env:USERPROFILE "jev-safety-gateway-backup-$stamp.zip"
        Write-Step "备份到 $backup"
        $toArchive = @()
        if (Test-Path -LiteralPath $dataDir) { $toArchive += $dataDir }
        if (Test-Path -LiteralPath $logDir) { $toArchive += $logDir }
        if ($toArchive.Count -gt 0) {
            Compress-Archive -Path $toArchive -DestinationPath $backup -Force
            if (-not (Test-Path -LiteralPath $backup)) {
                Stop-WithError "备份失败，未执行删除。目标：$backup"
            }
            Write-Info "备份完成：$backup（请确认可读后再依赖它）"
        }

        Write-Step '删除 data\ 与 logs\'
        if (Test-Path -LiteralPath $dataDir) { Remove-Item -LiteralPath $dataDir -Recurse -Force }
        if (Test-Path -LiteralPath $logDir) { Remove-Item -LiteralPath $logDir -Recurse -Force }
    }
}

# ---------- 收尾 ----------
if (Test-Path -LiteralPath $InstallDir) {
    $remaining = Get-ChildItem -LiteralPath $InstallDir -Force
    if ($remaining.Count -eq 0) {
        Remove-Item -LiteralPath $InstallDir -Force
        Write-Info "安装目录已空，一并移除：$InstallDir"
    }
}

Write-Host ''
Write-Host '卸载完成。' -ForegroundColor Green
if (-not $Purge -and (Test-Path -LiteralPath $InstallDir)) {
    Write-Host ''
    Write-Host '以下内容被有意保留：' -ForegroundColor Yellow
    if (Test-Path -LiteralPath $dataDir) { Write-Host "  数据库：$dataDir" }
    if (Test-Path -LiteralPath $logDir) { Write-Host "  日志：  $logDir" }
    Write-Host ''
    Write-Host '  数据库是三件套 jev-safety-gateway.db / .db-wal / .db-shm，'
    Write-Host '  其中 -wal 可能远大于主库——要么整体保留，要么整体删除。'
    Write-Host '  需要清理请先备份并确认副本可读，再手工删除，或重跑本脚本加 -Purge。'
    Write-Host ''
    Write-Host "  备份命令："
    Write-Host "    Compress-Archive -Path '$InstallDir' -DestinationPath `"`$env:USERPROFILE\jev-safety-gateway-backup-`$(Get-Date -Format yyyyMMdd).zip`""
}
exit 0
