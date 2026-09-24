#Requires -Version 5.1
<#
.SYNOPSIS
    把 JEV 安全网关注册成 Windows 服务（开机自启 + 崩溃自动重启）。

.DESCRIPTION
    用 sc.exe 把 jev-safety-gateway.exe 注册为原生 Windows 服务。二进制自身带服务支持
    （见 cmd\jev-safety-gateway\entry_windows.go），所以不需要 nssm 之类的外部包装器。

    幂等：重复执行即为升级——停服务、换二进制、再起。

    绝不触碰运行态：数据库默认在 $env:ProgramData\jev-safety-gateway\data\，
    和仓库里的 data\ 是两回事。本脚本从不删除数据目录，升级时原样保留。

    运行账号默认是 LocalSystem。要换成受限账号（如虚拟账号 NT SERVICE\jev-safety-gateway
    或域账号）请用 -ServiceAccount，并自行确保该账号对 -InstallDir 有读写权限。

.PARAMETER ExePath
    待安装的 jev-safety-gateway.exe。默认取仓库测试包 out\jev-safety-gateway\jev-safety-gateway.exe。

.PARAMETER InstallDir
    安装目录，默认 $env:ProgramData\jev-safety-gateway。二进制、data\、logs\ 都在这里。

.PARAMETER ServiceAccount
    服务运行账号，默认 LocalSystem。

.PARAMETER ProxyAddr
    代理监听地址，默认 :8080。

.PARAMETER AdminAddr
    控制台监听地址，默认 127.0.0.1:8081（只绑本机）。

.PARAMETER EnvFile
    可选：一次性初始化用的 KEY=VALUE 文件（上游地址、初始 JEV 密钥、初始管理员口令）。
    内容会被写入服务的注册表 Environment 值。注意注册表对管理员与 SYSTEM 可读——
    不填此项则不含任何密钥，改在控制台里配置，推荐生产环境这样做。

.EXAMPLE
    .\scripts\install-service.ps1 -ExePath .\out\jev-safety-gateway\jev-safety-gateway.exe
#>
[CmdletBinding()]
param(
    [string]$ExePath = '',
    [string]$InstallDir = '',
    [string]$ServiceAccount = 'LocalSystem',
    [string]$ProxyAddr = ':8080',
    [string]$AdminAddr = '127.0.0.1:8081',
    [string]$EnvFile = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }

$serviceName = 'jev-safety-gateway'
$displayName = 'JEV Safety Gateway'
$description = 'JEV 安全网关：LLM API 内容安全过滤反向代理（代理 :8080 / 控制台仅本机 :8081）'

function Write-Step { param([string]$Message) Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Info { param([string]$Message) Write-Host "    $Message" }
function Write-Notice { param([string]$Message) Write-Host "提示：$Message" -ForegroundColor Yellow }
function Stop-WithError { param([string]$Message) Write-Host "错误：$Message" -ForegroundColor Red; exit 1 }

# ---------- 前置检查 ----------
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object Security.Principal.WindowsPrincipal($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Stop-WithError '需要管理员权限。请以管理员身份打开 PowerShell 后重试。'
}

$repoRoot = Split-Path -Parent $PSScriptRoot
if (-not $ExePath) {
    $ExePath = Join-Path $repoRoot 'out\jev-safety-gateway\jev-safety-gateway.exe'
}
$ExePath = [System.IO.Path]::GetFullPath($ExePath)
if (-not (Test-Path -LiteralPath $ExePath)) {
    Stop-WithError "未找到二进制：$ExePath`n先运行 .\scripts\build-local-test.ps1 生成测试包，或用 -ExePath 指定。"
}

if (-not $InstallDir) {
    $InstallDir = Join-Path $env:ProgramData 'jev-safety-gateway'
}
$InstallDir = [System.IO.Path]::GetFullPath($InstallDir)

$dataDir = Join-Path $InstallDir 'data'
$logDir = Join-Path $InstallDir 'logs'
$installedExe = Join-Path $InstallDir 'jev-safety-gateway.exe'
$dbPath = Join-Path $dataDir 'jev-safety-gateway.db'
$logPath = Join-Path $logDir 'gateway.log'

# 安装目录不能落在仓库里：仓库里的 data\ 是开发机的运行态，两者混在一起备份口径就乱了。
if ($InstallDir.StartsWith([System.IO.Path]::GetFullPath($repoRoot), [System.StringComparison]::OrdinalIgnoreCase)) {
    Write-Notice "安装目录位于仓库内（$InstallDir）。生产部署建议用默认的 $env:ProgramData\jev-safety-gateway。"
}

# ---------- 服务环境变量 ----------
# Windows 服务没有 shell，环境变量从注册表读：
#   HKLM\SYSTEM\CurrentControlSet\Services\<服务名>\Environment  (REG_MULTI_SZ)
# 这里只放基础设施变量。密钥与口令留给控制台配置，避免落到注册表里。
$serviceEnv = @(
    "JEV_DB_PATH=$dbPath",
    "JEV_LOG_FILE=$logPath",
    "JEV_PROXY_ADDR=$ProxyAddr",
    "JEV_ADMIN_ADDR=$AdminAddr"
)

if ($EnvFile) {
    $EnvFile = [System.IO.Path]::GetFullPath($EnvFile)
    if (-not (Test-Path -LiteralPath $EnvFile)) {
        Stop-WithError "未找到 -EnvFile 指定的文件：$EnvFile"
    }
    Write-Notice "-EnvFile 中的值会写入服务注册表 Environment（管理员与 SYSTEM 可读），请自行评估密钥暴露面。"
    foreach ($line in Get-Content -LiteralPath $EnvFile -Encoding UTF8) {
        $trimmed = $line.Trim()
        if ($trimmed.Length -eq 0 -or $trimmed.StartsWith('#')) { continue }
        $separator = $trimmed.IndexOf('=')
        if ($separator -lt 1) { continue }
        $name = $trimmed.Substring(0, $separator).Trim()
        $value = $trimmed.Substring($separator + 1).Trim()
        if ($value.Length -eq 0) { continue }
        if ($name -in @('JEV_DB_PATH', 'JEV_LOG_FILE', 'JEV_PROXY_ADDR', 'JEV_ADMIN_ADDR')) {
            Write-Notice "$name 由本脚本管理，忽略 -EnvFile 中的该行。"
            continue
        }
        $serviceEnv += "$name=$value"
    }
}

# ---------- 停止并移除旧服务（升级路径） ----------
$existing = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if ($existing) {
    Write-Step "已存在服务 $serviceName，按升级处理"
    if ($existing.Status -ne 'Stopped') {
        Write-Info '停止服务（等待优雅退出，在途请求会被放完）'
        & sc.exe stop $serviceName | Out-Null
        $deadline = (Get-Date).AddSeconds(30)
        while ((Get-Service -Name $serviceName).Status -ne 'Stopped' -and (Get-Date) -lt $deadline) {
            Start-Sleep -Milliseconds 500
        }
        if ((Get-Service -Name $serviceName).Status -ne 'Stopped') {
            Stop-WithError '服务在 30 秒内未能停止。请检查 logs\gateway.log 是否有卡住的请求，或先手工排查。'
        }
    }
    Write-Info '删除旧服务注册（数据与日志目录不受影响）'
    & sc.exe delete $serviceName | Out-Null
    Start-Sleep -Milliseconds 500
}

# ---------- 安装目录与文件 ----------
Write-Step "准备安装目录：$InstallDir"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
# 数据与日志目录只创建、不清空：升级时这里的库就是全部历史配置与审计日志。
New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
New-Item -ItemType Directory -Force -Path $logDir | Out-Null

Write-Step '复制二进制'
if ((Test-Path -LiteralPath $installedExe) -and
    [System.IO.Path]::GetFullPath($ExePath) -ne [System.IO.Path]::GetFullPath($installedExe)) {
    $stamp = Get-Date -Format 'yyyyMMddHHmmss'
    Copy-Item -LiteralPath $installedExe -Destination "$installedExe.$stamp.bak" -Force
    Write-Info "旧二进制已备份为 jev-safety-gateway.exe.$stamp.bak"
}
if ([System.IO.Path]::GetFullPath($ExePath) -ne [System.IO.Path]::GetFullPath($installedExe)) {
    Copy-Item -LiteralPath $ExePath -Destination $installedExe -Force
} else {
    Write-Info '源与目标相同，跳过复制。'
}

# ---------- 注册服务 ----------
Write-Step "注册服务 $serviceName"
$createArgs = @(
    'create', $serviceName,
    "binPath= $installedExe",
    'start= auto',
    "DisplayName= $displayName",
    "obj= $ServiceAccount"
)
& sc.exe @createArgs | Out-Null
if ($LASTEXITCODE -ne 0) { Stop-WithError "sc.exe create 失败（退出码 $LASTEXITCODE）。" }

& sc.exe description $serviceName $description | Out-Null

# 崩溃自动重启：5 秒后重试，最多 3 次，计数器 24 小时复位。
& sc.exe failure $serviceName reset= 86400 actions= restart/5000/restart/5000/restart/5000 | Out-Null
if ($LASTEXITCODE -ne 0) { Write-Notice '设置失败重启策略未成功（sc.exe failure 返回非零），服务本身已注册。' }

# ---------- 写入服务环境变量 ----------
Write-Step '写入服务环境变量（注册表 Environment）'
$regPath = "HKLM:\SYSTEM\CurrentControlSet\Services\$serviceName"
New-ItemProperty -Path $regPath -Name 'Environment' -PropertyType MultiString -Value $serviceEnv -Force | Out-Null
foreach ($entry in $serviceEnv) {
    $name = $entry.Substring(0, $entry.IndexOf('='))
    if ($name -in @('JEV_API_KEY', 'JEV_ADMIN_PASSWORD', 'JEV_UPSTREAM_URL', 'JEV_BASE_URL')) {
        Write-Info "$name=<已设置>"
    } else {
        Write-Info $entry
    }
}

# ---------- 启动 ----------
Write-Step '启动服务'
& sc.exe start $serviceName | Out-Null
Start-Sleep -Seconds 2

$svc = Get-Service -Name $serviceName
if ($svc.Status -ne 'Running') {
    Write-Host ''
    Write-Host "服务未能保持运行（当前状态：$($svc.Status)）。" -ForegroundColor Red
    Write-Host "请查看日志：$logPath" -ForegroundColor Red
    Write-Host '常见原因：端口被占用、数据目录不可写、服务账号权限不足。' -ForegroundColor Red
    exit 1
}

Write-Host ''
Write-Host '安装完成，服务已启动。' -ForegroundColor Green
Write-Host ''
Write-Host "  服务名：    $serviceName"
Write-Host "  安装目录：  $InstallDir"
Write-Host "  数据库：    $dbPath"
Write-Host "  日志：      $logPath"
Write-Host "  代理监听：  $ProxyAddr"
Write-Host "  控制台：    $AdminAddr  →  http://127.0.0.1:8081"
Write-Host ''
Write-Host '  下一步：浏览器打开 http://127.0.0.1:8081 完成首次设置（管理员口令、上游地址、JEV 密钥）。'
Write-Host '  若在远程机器上，用 SSH 隧道或直接在本机浏览器访问。'
Write-Host ''
Write-Host '  常用命令：'
Write-Host "    Get-Service $serviceName"
Write-Host "    sc.exe stop $serviceName      # 优雅停止，在途请求会放完"
Write-Host "    sc.exe start $serviceName"
Write-Host "    Get-Content '$logPath' -Wait -Tail 50"
Write-Host ''
Write-Host "  卸载：.\scripts\uninstall-service.ps1"
exit 0
