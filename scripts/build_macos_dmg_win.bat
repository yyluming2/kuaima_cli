@echo off
chcp 65001 >nul
setlocal enabledelayedexpansion

REM ============================================
REM 快马 AI CLI - macOS DMG 打包脚本 (Windows)
REM ============================================
REM 这个脚本会在 GitHub Actions 上触发构建流程
REM 打包完成后，你可以从 GitHub Releases 下载 DMG 文件
REM ============================================

set "SCRIPT_DIR=%~dp0"
set "REPO_URL=https://github.com/your-username/kuaima_cli"

:check_git
echo [1/5] 检查 Git 环境...
where git >nul 2>&1
if errorlevel 1 (
    echo ❌ 错误: 未找到 Git，请先安装 Git
    echo 下载地址: https://git-scm.com/download/win
    pause
    exit /b 1
)
echo ✅ Git 环境检查通过

:check_gh
echo [2/5] 检查 GitHub CLI...
gh --version >nul 2>&1
if errorlevel 1 (
    echo ⚠️  警告: 未找到 GitHub CLI (gh)
    echo.
    echo 建议安装 GitHub CLI 以便自动触发构建和下载
    echo 下载地址: https://cli.github.com/
    echo.
    echo 安装后运行: gh auth login
    echo.
    set "USE_GH=0"
) else (
    echo ✅ GitHub CLI 检查通过
    gh auth status >nul 2>&1
    if errorlevel 1 (
        echo ⚠️  警告: GitHub CLI 未登录
        echo 请运行: gh auth login
        set "USE_GH=0"
    ) else (
        echo ✅ GitHub CLI 已登录
        set "USE_GH=1"
    )
)

:get_version
echo [3/5] 获取版本号...
set "VERSION=%1"
if "%VERSION%"=="" (
    for /f "tokens=*" %%i in ('cd /d "%SCRIPT_DIR%" ^&^& git describe --tags --always --dirty --match "v*" 2^>nul') do set "VERSION=%%i"
    if "!VERSION!"=="" (
        set "VERSION=0.1.0"
    )
)
set "VERSION=!VERSION:v=!"
echo 版本号: !VERSION!

:create_tag
echo [4/5] 创建 Git 标签...
cd /d "%SCRIPT_DIR%"
if "%USE_GH%"=="1" (
    echo 使用 GitHub CLI 自动触发构建...
    git tag -a "v!VERSION!" -m "Release v!VERSION!" 2>nul
    if errorlevel 1 (
        echo ⚠️  标签已存在，跳过创建
    ) else (
        git push origin "v!VERSION!"
        echo ✅ 标签已创建并推送，构建将自动开始
    )
) else (
    echo.
    echo ===========================================
    echo 手动操作指南:
    echo ===========================================
    echo 1. 创建标签: git tag -a v!VERSION! -m "Release v!VERSION!"
    echo 2. 推送标签: git push origin v!VERSION!
    echo.
    echo 或者:
    echo 1. 访问 GitHub Actions 页面
    echo 2. 点击 "Build macOS DMG" 工作流
    echo 3. 点击 "Run workflow" 并输入版本号: !VERSION!
    echo ===========================================
)

:instructions
echo.
echo [5/5] 打包说明
echo ============================================
echo 📦 macOS DMG 打包说明
echo ============================================
echo.
if "%USE_GH%"=="1" (
    echo ✅ 已触发 GitHub Actions 构建
    echo.
    echo 监控构建状态:
    echo   gh run list --workflow=build-macos-dmg.yml
    echo.
    echo 查看实时日志:
    echo   gh run watch
    echo.
    echo 构建完成后，DMG 文件将自动发布到 GitHub Releases
    echo.
    echo 下载 DMG:
    echo   gh release view v!VERSION! --json assets -q '.assets[].browser_download_url'
    echo.
) else (
    echo 📍 GitHub Actions 构建流程已配置
    echo.
    echo 请按照上述手动操作指南触发构建
    echo.
    echo 构建完成后:
    echo 1. 访问: %REPO_URL%/actions
    echo 2. 查看构建状态
    echo 3. 从 GitHub Releases 下载 DMG 文件
    echo.
)

echo ============================================
echo 版本: !VERSION!
echo 仓库: %REPO_URL%
echo ============================================
echo.

pause