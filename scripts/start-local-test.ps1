#Requires -Version 5.1
<#
.SYNOPSIS
    构建并启动本地 test 网关（Windows，一条命令）。

.DESCRIPTION
    先调用同目录的 build-local-test.ps1 完成前端构建、后端构建与打包，
    再在测试包目录内运行 start-jev-safety-gateway.ps1 启动网关。

    scripts\template-*.ps1 是打包模板 —— build-local-test.ps1 会把它们复制进测试包，
    真正可运行的是测试包目录里的那一份。本脚本负责这个跳转。

    默认行为：
      * 构建前先停掉测试包里正在运行的网关。Windows 上 jev-safety-gateway.exe 被占用时
        go build 无法覆盖它，不先停就会构建失败。
      * 测试包里已有 .env 时保留它，不被仓库根目录的 .env.example 覆盖。
      * 数据库放在仓库根目录的 data\jev-safety-gateway.db，而不是测试包目录。运行态和源码
        在同一棵树里好找好备份，构建脚本的 -Clean 也只会清测试包，碰不到它。

.PARAMETER SkipBuild
    跳过构建，直接启动已有测试包（要求包内已有 jev-safety-gateway.exe）。

.PARAMETER OutputDir
    测试包目录，默认 out\jev-safety-gateway（必须位于仓库目录内），与 build-local-test.ps1 一致。

.PARAMETER RefreshEnv
    构建时用仓库根目录的 .env.example 覆盖测试包内的 .env（会重置上游地址与密钥）。

.PARAMETER NoRestart
    测试包里已有网关在运行时不再自动停止，直接报错退出。

.PARAMETER Clean
    构建前清空测试包目录。注意包内 data\jev-safety-gateway.db 与 .env 会一并删除。

.PARAMETER SkipFrontend
    跳过前端构建（前端沿用仓库中已提交的 web\dist）。

.PARAMETER Offline
    不访问网络：缺少 web\node_modules 时直接报错退出。

.PARAMETER NoZip
    不生成 zip 压缩包。

.PARAMETER ProxyAddr
    代理监听地址，默认 :8080。

.PARAMETER AdminAddr
    管理控制台监听地址，默认 127.0.0.1:8081（只绑本机）。

.PARAMETER DbPath
    数据库路径，默认仓库根目录的 data\jev-safety-gateway.db。相对路径按仓库根目录解析。

.EXAMPLE
    .\scripts\start-local-test.ps1

    构建前端与后端，打包到 out\jev-safety-gateway，然后启动网关。

.EXAMPLE
    .\scripts\start-local-test.ps1 -SkipFrontend -NoZip

    只构建后端（前端沿用已提交产物），不生成 zip，然后启动。日常改 Go 代码时最快。

.EXAMPLE
    .\scripts\start-local-test.ps1 -SkipBuild

    不重新构建，直接重启测试包里已有的网关。
#>
[CmdletBinding()]
param(
    [switch]$SkipBuild,
    [string]$OutputDir = 'out\jev-safety-gateway',
    [switch]$RefreshEnv,
    [switch]$NoRestart,
    [switch]$Clean,
    [switch]$SkipFrontend,
    [switch]$Offline,
    [switch]$NoZip,
    [string]$ProxyAddr = ':8080',
    [string]$AdminAddr = '127.0.0.1:8081',
    [string]$DbPath = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }
$OutputEncoding = [System.Text.Encoding]::UTF8

function Write-Step { param([string]$Message) Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Info { param([string]$Message) Write-Host "    $Message" }
function Stop-WithError { param([string]$Message) Write-Host "错误：$Message" -ForegroundColor Red; exit 1 }

# $LASTEXITCODE 在首次调用外部命令前并不存在，StrictMode 下直接读取会抛异常。
function Get-LastExitCode {
    if (Test-Path Variable:LASTEXITCODE) { return [int]$LASTEXITCODE }
    return 0
}

# 找出测试包内正在运行的网关：优先 jev-safety-gateway.pid，其次按可执行文件路径匹配。
function Get-PackageGatewayPid {
    param([Parameter(Mandatory)][string]$PackageDir)

    $pidFile = Join-Path $PackageDir 'jev-safety-gateway.pid'
    if (Test-Path -LiteralPath $pidFile) {
        $raw = (Get-Content -LiteralPath $pidFile -Raw).Trim()
        if ($raw -match '^\d+$' -and (Get-Process -Id ([int]$raw) -ErrorAction SilentlyContinue)) {
            return [int]$raw
        }
    }

    $exePath = Join-Path $PackageDir 'jev-safety-gateway.exe'
    $candidate = Get-Process -Name 'jev-safety-gateway' -ErrorAction SilentlyContinue |
        Where-Object { try { $_.Path -eq $exePath } catch { $false } } |
        Select-Object -First 1
    if ($candidate) { return $candidate.Id }

    return $null
}

$repoRoot = Split-Path -Parent $PSScriptRoot
$buildScript = Join-Path $PSScriptRoot 'build-local-test.ps1'
$pkgDir = if ([System.IO.Path]::IsPathRooted($OutputDir)) {
    [System.IO.Path]::GetFullPath($OutputDir)
} else {
    [System.IO.Path]::GetFullPath((Join-Path $repoRoot $OutputDir))
}

if (-not (Test-Path -LiteralPath $buildScript)) {
    Stop-WithError "未找到构建脚本：$buildScript"
}

# ---------- 结束测试包里正在运行的网关（构建前必须先做，否则 exe 被占用导致 go build 失败） ----------
$runningPid = Get-PackageGatewayPid -PackageDir $pkgDir
if ($null -ne $runningPid) {
    if ($NoRestart) {
        Stop-WithError "测试包里已有网关在运行（PID $runningPid）。请先运行 $pkgDir\stop-jev-safety-gateway.ps1，或去掉 -NoRestart 让本脚本自动重启。"
    }
    Write-Step "停止测试包里正在运行的网关（PID $runningPid）"
    Stop-Process -Id $runningPid -Force -ErrorAction SilentlyContinue
    $deadline = (Get-Date).AddSeconds(5)
    while ((Get-Process -Id $runningPid -ErrorAction SilentlyContinue) -and (Get-Date) -lt $deadline) {
        Start-Sleep -Milliseconds 100
    }
    Remove-Item -LiteralPath (Join-Path $pkgDir 'jev-safety-gateway.pid') -Force -ErrorAction SilentlyContinue
    Write-Info '已停止。'
}

# ---------- 构建 ----------
if (-not $SkipBuild) {
    $buildArgs = @{ OutputDir = $OutputDir }
    if (-not $RefreshEnv) { $buildArgs['KeepEnv'] = $true }
    if ($Clean) { $buildArgs['Clean'] = $true }
    if ($SkipFrontend) { $buildArgs['SkipFrontend'] = $true }
    if ($Offline) { $buildArgs['Offline'] = $true }
    if ($NoZip) { $buildArgs['NoZip'] = $true }

    Write-Step '打包（build-local-test.ps1）'
    & $buildScript @buildArgs
    $buildCode = Get-LastExitCode
    if ($buildCode -ne 0) {
        Stop-WithError "构建失败（退出码 $buildCode），已中止启动。"
    }
    Write-Host ''
}

# ---------- 启动 ----------
$exePath = Join-Path $pkgDir 'jev-safety-gateway.exe'
$startScript = Join-Path $pkgDir 'start-jev-safety-gateway.ps1'

if (-not (Test-Path -LiteralPath $exePath)) {
    if ($SkipBuild) {
        Stop-WithError "测试包里没有 jev-safety-gateway.exe：$exePath。去掉 -SkipBuild 先构建一次。"
    }
    Stop-WithError "构建后仍未找到 $exePath。"
}
if (-not (Test-Path -LiteralPath $startScript)) {
    Stop-WithError "测试包里没有 start-jev-safety-gateway.ps1：$startScript。请重新运行 .\scripts\build-local-test.ps1 组装测试包。"
}

# 数据库统一放仓库根目录。这里显式写进 JEV_DB_PATH：包内启动脚本对该环境变量的优先级高于
# 它自己的默认值，不显式设置的话，shell 里残留的旧值会让本次指定静默失效。
if (-not $DbPath) { $DbPath = Join-Path $repoRoot 'data\jev-safety-gateway.db' }
$env:JEV_DB_PATH = if ([System.IO.Path]::IsPathRooted($DbPath)) {
    [System.IO.Path]::GetFullPath($DbPath)
} else {
    [System.IO.Path]::GetFullPath((Join-Path $repoRoot $DbPath))
}

Write-Info "数据库：$env:JEV_DB_PATH"
Write-Step "启动网关（$startScript）"
& $startScript -ProxyAddr $ProxyAddr -AdminAddr $AdminAddr
exit (Get-LastExitCode)
