#Requires -Version 5.1
<#
.SYNOPSIS
    本地 test 环境打包脚本（Windows）。

.DESCRIPTION
    一条命令完成：环境预检 → 前端构建（npm） → 后端构建（go） → 组装测试包 → 压缩 zip。
    默认输出到 out\jev-safety-gateway\，并生成带日期与短提交号的 zip。

.PARAMETER OutputDir
    测试包输出目录，默认 out\jev-safety-gateway（必须位于仓库目录内）。

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

.PARAMETER KeepEnv
    输出目录里已有 .env 时保留它，不用仓库根目录的 .env.example 覆盖。
    「反复构建 + 启动」的本地循环建议加上，否则每次构建都会把填好的上游地址与密钥重置掉。
#>
[CmdletBinding()]
param(
    [string]$OutputDir = 'out\jev-safety-gateway',
    [switch]$SkipFrontend,
    [switch]$SkipBackend,
    [switch]$Offline,
    [switch]$NoZip,
    [switch]$Clean,
    [switch]$KeepEnv
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
$exeName = 'jev-safety-gateway.exe'

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

# ---------- 构建标识 ----------
# 这一组值同时供两处使用：go build 的 -ldflags（GET /api/version 会报给控制台）
# 和 README.txt 的占位替换。所以只在这里取一次，避免两边算出不同的结果。
$commit = 'nogit'
try {
    $commitOutput = & git -C $repoRoot rev-parse --short HEAD 2>$null
    if ($LASTEXITCODE -eq 0 -and $commitOutput) {
        $commit = ([string]($commitOutput | Select-Object -First 1)).Trim()
    }
} catch { }

$version = 'dev'
if (Test-Path -LiteralPath $frontendManifest) {
    # 必须显式 -Encoding UTF8：package.json 无 BOM 且含中文描述，PowerShell 5.1 会按
    # 本地代码页解码，ConvertFrom-Json 随即报错，version 静默退回 'dev'。
    try { $version = (Get-Content -LiteralPath $frontendManifest -Raw -Encoding UTF8 | ConvertFrom-Json).version } catch { }
}
if (-not $version) { $version = 'dev' }
# 无 git 时不留 "nogit" 字样给控制台，留空即可，前端会只显示版本号。
$buildCommit = if ($commit -eq 'nogit') { '' } else { $commit }
$buildTimeISO = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')

$exePath = Join-Path $pkgDir $exeName
if (-not $SkipBackend) {
    Write-Step '构建后端（go build）'
    Push-Location -LiteralPath $repoRoot
    try {
        # -s -w 去掉符号表；-X 注入上面的构建标识。
        $ldflags = "-s -w -X main.version=$version -X main.commit=$buildCommit -X main.buildTime=$buildTimeISO"
        $goArgs = @('build', '-trimpath', "-ldflags=$ldflags", '-o', $exePath, './cmd/jev-safety-gateway')
        & go @goArgs
        if ($LASTEXITCODE -ne 0) {
            Stop-WithError "go build 失败（退出码 $LASTEXITCODE）。依赖未就绪时可先执行 go mod tidy。"
        }
    } finally {
        Pop-Location
    }
    if (-not (Test-Path -LiteralPath $exePath)) {
        Stop-WithError "go build 未产出 $exePath。"
    }
    Write-Info "后端构建完成：$exePath（版本 $version，提交 $buildCommit）"
} elseif (-not (Test-Path -LiteralPath $exePath)) {
    Write-Notice "已指定 -SkipBackend，但输出目录中没有现成的 $exeName；测试包将不包含可执行文件。"
}

# ---------- 组装测试包 ----------
Write-Step "组装测试包：$pkgDir"

$envTemplate = Join-Path $repoRoot '.env.example'
$envTarget = Join-Path $pkgDir '.env'
$envExists = Test-Path -LiteralPath $envTarget

if ($KeepEnv -and $envExists) {
    Write-Info '.env 已存在，按 -KeepEnv 保留原有配置（未用 .env.example 覆盖）。'
} elseif (Test-Path -LiteralPath $envTemplate) {
    Copy-Item -LiteralPath $envTemplate -Destination $envTarget -Force
    if ($envExists) {
        Write-Notice '.env 已被 .env.example 覆盖（要保留原配置请加 -KeepEnv），请核对上游地址与密钥。'
    } else {
        Write-Info '.env 已由 .env.example 生成，请填入真实上游地址与密钥。'
    }
} elseif ($envExists) {
    Write-Notice '.env 已存在但仓库中没有 .env.example，保留原文件。'
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

# template-*.ps1 / template-README.txt 是测试包内脚本的模板：复制进测试包并去掉 template- 前缀。
# 它们必须和 jev-safety-gateway.exe 同目录才能运行，在仓库里直接跑没有意义。
$packageTemplates = @(
    @{ Source = (Join-Path $PSScriptRoot 'template-start-jev-safety-gateway.ps1'); Target = 'start-jev-safety-gateway.ps1' }
    @{ Source = (Join-Path $PSScriptRoot 'template-stop-jev-safety-gateway.ps1'); Target = 'stop-jev-safety-gateway.ps1' }
    @{ Source = (Join-Path $PSScriptRoot 'template-README.txt'); Target = 'README.txt' }
)
foreach ($template in $packageTemplates) {
    if (-not (Test-Path -LiteralPath $template.Source)) {
        Stop-WithError "打包模板缺失：$($template.Source)"
    }
    Copy-Item -LiteralPath $template.Source -Destination (Join-Path $pkgDir $template.Target) -Force
}
$readmeSource = Join-Path $PSScriptRoot 'template-README.txt'

# $commit / $version 在打包开始时已取过一次（见「构建标识」），README 与二进制
# 用同一组值，这里不再重复计算。

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
    $zipPath = Join-Path $outRoot "jev-safety-gateway-$dateTag-$commit.zip"
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
Write-Host '本机启动（脚本必须和 jev-safety-gateway.exe 同目录，即测试包目录）：'
Write-Host "  $pkgDir\start-jev-safety-gateway.ps1"
Write-Host '构建并直接启动（会自动停掉包内旧进程，并保留已有 .env）：'
Write-Host '  .\scripts\start-local-test.ps1'
Write-Host '在测试机上：解压 → 填写 .env → 运行 .\start-jev-safety-gateway.ps1，浏览器打开 http://127.0.0.1:8081'
exit 0
