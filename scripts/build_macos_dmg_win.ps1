# 快马 AI CLI - macOS DMG 打包脚本 (Windows PowerShell)
# 这个脚本会在 GitHub Actions 上触发构建流程
# 打包完成后，你可以从 GitHub Releases 下载 DMG 文件

param(
    [string]$Version = "",
    [string]$RepoUrl = "https://github.com/your-username/kuaima_cli"
)

$ErrorActionPreference = "Stop"
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

function Write-Step {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Cyan
}

function Write-Success {
    param([string]$Message)
    Write-Host "✅ $Message" -ForegroundColor Green
}

function Write-Error {
    param([string]$Message)
    Write-Host "❌ $Message" -ForegroundColor Red
}

function Write-Warning {
    param([string]$Message)
    Write-Host "⚠️  $Message" -ForegroundColor Yellow
}

# Step 1: 检查 Git 环境
Write-Step "[1/5] 检查 Git 环境..."
try {
    git --version | Out-Null
    Write-Success "Git 环境检查通过"
} catch {
    Write-Error "未找到 Git，请先安装 Git"
    Write-Host "下载地址: https://git-scm.com/download/win" -ForegroundColor Gray
    Read-Host "按 Enter 键退出"
    exit 1
}

# Step 2: 检查 GitHub CLI
Write-Step "[2/5] 检查 GitHub CLI..."
$useGh = $false
try {
    gh --version | Out-Null
    try {
        gh auth status | Out-Null
        Write-Success "GitHub CLI 已登录"
        $useGh = $true
    } catch {
        Write-Warning "GitHub CLI 未登录"
        Write-Host "请运行: gh auth login" -ForegroundColor Gray
    }
} catch {
    Write-Warning "未找到 GitHub CLI (gh)"
    Write-Host ""
    Write-Host "建议安装 GitHub CLI 以便自动触发构建和下载" -ForegroundColor Gray
    Write-Host "下载地址: https://cli.github.com/" -ForegroundColor Gray
    Write-Host ""
    Write-Host "安装后运行: gh auth login" -ForegroundColor Gray
}

# Step 3: 获取版本号
Write-Step "[3/5] 获取版本号..."
if ([string]::IsNullOrEmpty($Version)) {
    Push-Location $scriptDir
    try {
        $Version = git describe --tags --always --dirty --match "v*" 2>$null
        if ([string]::IsNullOrEmpty($Version)) {
            $Version = "0.1.0"
        }
    } finally {
        Pop-Location
    }
}
$Version = $Version -replace "^v", ""
Write-Host "版本号: $Version" -ForegroundColor Gray

# Step 4: 创建 Git 标签
Write-Step "[4/5] 创建 Git 标签..."
Push-Location $scriptDir
try {
    if ($useGh) {
        Write-Host "使用 GitHub CLI 自动触发构建..." -ForegroundColor Gray
        try {
            git tag -a "v$Version" -m "Release v$Version" 2>$null | Out-Null
            git push origin "v$Version" 2>$null | Out-Null
            Write-Success "标签已创建并推送，构建将自动开始"
        } catch {
            Write-Warning "标签已存在，跳过创建"
        }
    } else {
        Write-Host ""
        Write-Host "===========================================" -ForegroundColor Yellow
        Write-Host "手动操作指南:" -ForegroundColor Yellow
        Write-Host "===========================================" -ForegroundColor Yellow
        Write-Host ""
        Write-Host "1. 创建标签: git tag -a v$Version -m `"Release v$Version`""
        Write-Host "2. 推送标签: git push origin v$Version"
        Write-Host ""
        Write-Host "或者:"
        Write-Host "1. 访问 GitHub Actions 页面"
        Write-Host "2. 点击 `"Build macOS DMG`" 工作流"
        Write-Host "3. 点击 `"Run workflow`" 并输入版本号: $Version"
        Write-Host ""
        Write-Host "===========================================" -ForegroundColor Yellow
    }
} finally {
    Pop-Location
}

# Step 5: 打包说明
Write-Step "[5/5] 打包说明"
Write-Host ""
Write-Host "===========================================" -ForegroundColor Cyan
Write-Host "📦 macOS DMG 打包说明" -ForegroundColor Cyan
Write-Host "===========================================" -ForegroundColor Cyan
Write-Host ""

if ($useGh) {
    Write-Success "已触发 GitHub Actions 构建"
    Write-Host ""
    Write-Host "监控构建状态:" -ForegroundColor Gray
    Write-Host "  gh run list --workflow=build-macos-dmg.yml" -ForegroundColor White
    Write-Host ""
    Write-Host "查看实时日志:" -ForegroundColor Gray
    Write-Host "  gh run watch" -ForegroundColor White
    Write-Host ""
    Write-Host "构建完成后，DMG 文件将自动发布到 GitHub Releases" -ForegroundColor Gray
    Write-Host ""
    Write-Host "下载 DMG:" -ForegroundColor Gray
    Write-Host "  gh release view v$Version --json assets -q '.assets[].browser_download_url'" -ForegroundColor White
    Write-Host ""
} else {
    Write-Host "📍 GitHub Actions 构建流程已配置" -ForegroundColor Gray
    Write-Host ""
    Write-Host "请按照上述手动操作指南触发构建" -ForegroundColor Gray
    Write-Host ""
    Write-Host "构建完成后:" -ForegroundColor Gray
    Write-Host "1. 访问: $RepoUrl/actions" -ForegroundColor White
    Write-Host "2. 查看构建状态" -ForegroundColor White
    Write-Host "3. 从 GitHub Releases 下载 DMG 文件" -ForegroundColor White
    Write-Host ""
}

Write-Host "===========================================" -ForegroundColor Cyan
Write-Host "版本: $Version" -ForegroundColor Gray
Write-Host "仓库: $RepoUrl" -ForegroundColor Gray
Write-Host "===========================================" -ForegroundColor Cyan
Write-Host ""

Read-Host "按 Enter 键退出"