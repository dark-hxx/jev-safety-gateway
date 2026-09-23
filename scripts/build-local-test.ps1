#Requires -Version 5.1
<#
.SYNOPSIS
    本地 test 环境打包脚本（Windows）。

.DESCRIPTION
    一条命令完成：环境预检 → 前端构建（npm） → 后端构建（go） → 组装测试包 → 压缩 zip。
    默认输出到 out\local-test\，并生成带日期与短提交号的 zip。

.PARAMETER OutputDir
    测试包输出目录，默认 out\local-test（必须位于仓库目录内）。

.PARAMETER SkipFrontend
    跳过前端构建，只构建后端。

.PARAMETER SkipBackend
    跳过后端构建，只构建前端。

.PARAMETER Offline
    不访问网络：缺少前端依赖时直接报错退出，不执行 npm ci。

.PARAMETER NoZip
    不生成 zip 压缩包。

.PARAMETER Clean
    构建前清空输出目录。
#>
[CmdletBinding()]
param(
    [string]$OutputDir = 'out\local-test',
    [switch]$SkipFrontend,
    [switch]$SkipBackend,
    [switch]$Offline,
    [switch]$NoZip,
    [switch]$Clean
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }
$OutputEncoding = [System.Text.Encoding]::UTF8

function Write-Step { param([string]$Message) Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Info { param([string]$Message) Write-Host "    $Message" }
function Write-Notice { param([string]$Message) Write-Host "提示：$Message" -ForegroundColor Yellow }
function Stop-WithError { param([string]$Message) Write-Host "错误：$Message" -ForegroundColor Red; exit 1 }

$repoRoot = Split-Path -Parent $PSScriptRoot
$webDir = Join-Path $repoRoot 'web'
$packageScriptDir = Join-Path $PSScriptRoot 'local-test'
$exeName = 'gateway.exe'

$repoRootFull = [System.IO.Path]::GetFullPath($repoRoot)
$pkgDir = if ([System.IO.Path]::IsPathRooted($OutputDir)) {
    [System.IO.Path]::GetFullPath($OutputDir)
} else {
    [System.IO.Path]::GetFullPath((Join-Path $repoRoot $OutputDir))
}
$outRoot = Split-Path -Parent $pkgDir

if (-not $pkgDir.StartsWith($repoRootFull, [System.StringComparison]::OrdinalIgnoreCase)) {
    Stop-WithError "-OutputDir 必须位于仓库目录内，当前解析为：$pkgDir"
}
if ($SkipFrontend -and $SkipBackend) {
    Stop-WithError '不能同时指定 -SkipFrontend 与 -SkipBackend：没有任何需要构建的内容，不会产出测试包。'
}

$goCommand = Get-Command go -ErrorAction SilentlyContinue
$nodeCommand = Get-Command node -ErrorAction SilentlyContinue
$npmCommand = Get-Command npm -ErrorAction SilentlyContinue

if (-not $SkipBackend -and -not $goCommand) {
    Stop-WithError '未找到 go 命令。请安装 Go 1.23+ 并确保它在 PATH 中。'
}

# ---------- 前端构建 ----------
$frontendManifest = Join-Path $webDir 'package.json'
$frontendBuilt = $false

if (-not $SkipFrontend) {
    if (-not (Test-Path -LiteralPath $frontendManifest)) {
        Write-Notice '未找到 web\package.json，跳过前端构建；测试包将使用仓库中已提交的前端资源。'
    } else {
        if (-not $nodeCommand -or -not $npmCommand) {
            Stop-WithError 'web\package.json 存在，但未找到 node/npm 命令。请安装 Node.js 18+ 后重试。'
        }
        $nodeVersion = (& node --version) 2>&1
        $npmVersion = (& npm --version) 2>&1
        Write-Info "node $nodeVersion / npm $npmVersion"

        $nodeModulesDir = Join-Path $webDir 'node_modules'
        $lockFile = Join-Path $webDir 'package-lock.json'
        $installNeeded = -not (Test-Path -LiteralPath $nodeModulesDir)
        if (-not $installNeeded -and (Test-Path -LiteralPath $lockFile)) {
            $installNeeded = (Get-Item -LiteralPath $lockFile).LastWriteTimeUtc -gt (Get-Item -LiteralPath $nodeModulesDir).LastWriteTimeUtc
        }
        if ($installNeeded -and $Offline) {
            Stop-WithError '-Offline 模式下不联网安装前端依赖，但 web\node_modules 缺失或已过期。请先在有网络时执行 npm ci。'
        }

        Push-Location -LiteralPath $webDir
        try {
            if ($installNeeded) {
                Write-Step '安装前端依赖（npm ci）'
                & npm ci
                if ($LASTEXITCODE -ne 0) { Stop-WithError "npm ci 失败（退出码 $LASTEXITCODE）。" }
            } else {
                Write-Info '前端依赖已存在且未过期，跳过 npm ci。'
            }
            Write-Step '构建前端（npm run build）'
            & npm run build
            if ($LASTEXITCODE -ne 0) { Stop-WithError "npm run build 失败（退出码 $LASTEXITCODE）。" }
        } finally {
            Pop-Location
        }

        $distIndex = Join-Path $webDir 'dist\index.html'
        if (-not (Test-Path -LiteralPath $distIndex)) {
            Stop-WithError "前端构建未产出 $distIndex，构建结果不可用。"
        }
        $frontendBuilt = $true
        Write-Info "前端构建完成：$distIndex"
    }
}

# ---------- 后端构建 ----------
if ($Clean -and (Test-Path -LiteralPath $pkgDir)) {
    Write-Step "清空输出目录：$pkgDir"
    Remove-Item -LiteralPath $pkgDir -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $pkgDir | Out-Null

$exePath = Join-Path $pkgDir $exeName
if (-not $SkipBackend) {
    Write-Step '构建后端（go build）'
    Push-Location -LiteralPath $repoRoot
    try {
        & go build -trimpath -ldflags '-s -w' -o $exePath ./cmd/gateway
        if ($LASTEXITCODE -ne 0) {
            Stop-WithError "go build 失败（退出码 $LASTEXITCODE）。依赖未就绪时可先执行 go mod tidy。"
        }
    } finally {
        Pop-Location
    }
    if (-not (Test-Path -LiteralPath $exePath)) {
        Stop-WithError "go build 未产出 $exePath。"
    }
    Write-Info "后端构建完成：$exePath"
} elseif (-not (Test-Path -LiteralPath $exePath)) {
    Write-Notice "已指定 -SkipBackend，但输出目录中没有现成的 $exeName；测试包将不包含可执行文件。"
}

# ---------- 组装测试包 ----------
Write-Step "组装测试包：$pkgDir"

$envTemplate = Join-Path $repoRoot '.env.example'
$envTarget = Join-Path $pkgDir '.env'
if (Test-Path -LiteralPath $envTemplate) {
    Copy-Item -LiteralPath $envTemplate -Destination $envTarget -Force
    Write-Info '.env 已由 .env.example 生成，请填入真实上游地址与密钥。'
} else {
    Set-Content -LiteralPath $envTarget -Encoding UTF8 -Value @(
        '# 首次启动的一次性初始化变量',
        'JEV_UPSTREAM_URL=',
        'JEV_BASE_URL=',
        'JEV_API_KEY=',
        'JEV_ADMIN_PASSWORD='
    )
    Write-Notice '未找到 .env.example，已生成空白 .env 模板。'
}

$startScriptSource = Join-Path $packageScriptDir 'start-gateway.ps1'
$stopScriptSource = Join-Path $packageScriptDir 'stop-gateway.ps1'
$readmeSource = Join-Path $packageScriptDir 'README.txt'
foreach ($required in @($startScriptSource, $stopScriptSource, $readmeSource)) {
    if (-not (Test-Path -LiteralPath $required)) {
        Stop-WithError "打包模板缺失：$required"
    }
}
Copy-Item -LiteralPath $startScriptSource -Destination $pkgDir -Force
Copy-Item -LiteralPath $stopScriptSource -Destination $pkgDir -Force

$commit = 'nogit'
try {
    $commitOutput = & git -C $repoRoot rev-parse --short HEAD 2>$null
    if ($LASTEXITCODE -eq 0 -and $commitOutput) {
        $commit = ([string]($commitOutput | Select-Object -First 1)).Trim()
    }
} catch { }

$version = 'dev'
if (Test-Path -LiteralPath $frontendManifest) {
    try { $version = (Get-Content -LiteralPath $frontendManifest -Raw | ConvertFrom-Json).version } catch { }
}
if (-not $version) { $version = 'dev' }

if ($frontendBuilt) {
    $frontendState = '本次打包已重新构建（npm run build）'
} elseif (-not $SkipFrontend) {
    $frontendState = '仓库未包含前端工程，沿用已提交的前端资源'
} else {
    $frontendState = '按 -SkipFrontend 跳过，沿用仓库中已提交的前端资源'
}

$buildTime = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'
$readmeText = Get-Content -LiteralPath $readmeSource -Raw -Encoding UTF8
$readmeText = $readmeText.Replace('@@BUILD_TIME@@', $buildTime)
$readmeText = $readmeText.Replace('@@COMMIT@@', $commit)
$readmeText = $readmeText.Replace('@@VERSION@@', [string]$version)
$readmeText = $readmeText.Replace('@@FRONTEND@@', $frontendState)
Set-Content -LiteralPath (Join-Path $pkgDir 'README.txt') -Value $readmeText -Encoding UTF8

# ---------- 压缩 ----------
$zipPath = $null
if (-not $NoZip) {
    $dateTag = Get-Date -Format 'yyyyMMdd'
    $zipPath = Join-Path $outRoot "jev-gateway-local-test-$dateTag-$commit.zip"
    if (Test-Path -LiteralPath $zipPath) { Remove-Item -LiteralPath $zipPath -Force }
    Write-Step "压缩测试包：$zipPath"
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    [System.IO.Compression.ZipFile]::CreateFromDirectory($pkgDir, $zipPath, [System.IO.Compression.CompressionLevel]::Optimal, $false)
}

Write-Host ''
Write-Host '打包完成。' -ForegroundColor Green
Write-Host "  测试包目录：$pkgDir"
if ($zipPath) { Write-Host "  压缩包：    $zipPath" }
Write-Host ''
Write-Host '在测试机上：解压 → 填写 .env → 运行 .\start-gateway.ps1，浏览器打开 http://127.0.0.1:8081'
exit 0
