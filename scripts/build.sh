#!/bin/bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

success() {
    echo -e "${GREEN}✅ $1${NC}"
}

error() {
    echo -e "${RED}❌ $1${NC}"
}

warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

# 检测操作系统
detect_os() {
    case "$(uname -s)" in
        Darwin*)
            echo "macos"
            ;;
        Linux*)
            echo "linux"
            ;;
        MINGW*|MSYS*|CYGWIN*|Windows*)
            echo "windows"
            ;;
        *)
            echo "unknown"
            ;;
    esac
}

# macOS 构建
build_macos() {
    info "检测到 macOS 系统"

    # 检查必要的命令
    local missing_commands=()
    for cmd in go git; do
        if ! command -v "$cmd" >/dev/null 2>&1; then
            missing_commands+=("$cmd")
        fi
    done

    if [ ${#missing_commands[@]} -gt 0 ]; then
        error "缺少必要命令: ${missing_commands[*]}"
        exit 1
    fi

    success "环境检查通过"

    # 检查用户是否想要构建 DMG
    if [ "${BUILD_DMG:-false}" = "true" ]; then
        info "构建 macOS DMG 安装包..."

        if [ -f "$script_dir/build_macos_dmg.sh" ]; then
            bash "$script_dir/build_macos_dmg.sh"
            success "DMG 构建完成"
        else
            error "未找到构建脚本: $script_dir/build_macos_dmg.sh"
            exit 1
        fi
    else
        info "构建命令行版本..."

        cd "$root"
        go build -o "$root/dist/kuaima_cli" "$root/cmd/kuaima_cli"

        if [ $? -eq 0 ]; then
            success "构建完成: $root/dist/kuaima_cli"
            info "运行: $root/dist/kuaima_cli"
            info "启动 WebUI: $root/dist/kuaima_cli webui"
        else
            error "构建失败"
            exit 1
        fi
    fi
}

# Linux 构建
build_linux() {
    info "检测到 Linux 系统"

    # 检查必要的命令
    if ! command -v go >/dev/null 2>&1; then
        error "未找到 Go，请先安装 Go"
        exit 1
    fi

    success "环境检查通过"

    info "构建命令行版本..."

    cd "$root"
    go build -o "$root/dist/kuaima_cli" "$root/cmd/kuaima_cli"

    if [ $? -eq 0 ]; then
        success "构建完成: $root/dist/kuaima_cli"
        info "运行: $root/dist/kuaima_cli"
        info "启动 WebUI: $root/dist/kuaima_cli webui"
    else
        error "构建失败"
        exit 1
    fi
}

# Windows 构建
build_windows() {
    info "检测到 Windows 系统"

    warning "Windows 系统无法直接构建 macOS DMG"
    warning "如需构建 macOS 应用，请使用 GitHub Actions"

    info "构建 Windows 命令行版本..."

    cd "$root"
    go build -o "$root/dist/kuaima_cli.exe" "$root/cmd/kuaima_cli"

    if [ $? -eq 0 ]; then
        success "构建完成: $root/dist/kuaima_cli.exe"
        info "运行: .\\dist\\kuaima_cli.exe"
        info "启动 WebUI: .\\dist\\kuaima_cli.exe webui"

        # 检查是否有 Windows 脚本
        if [ -f "$script_dir/build_macos_dmg_win.ps1" ]; then
            info ""
            info "如需构建 macOS 应用，请运行:"
            info "  powershell.exe -ExecutionPolicy Bypass -File \"$script_dir/build_macos_dmg_win.ps1\" -Version \"1.0.0\""
            info ""
            info "详细信息请参考: $root/BUILD_MACOS_ON_WINDOWS.md"
        fi
    else
        error "构建失败"
        exit 1
    fi
}

# 主函数
main() {
    echo "======================================"
    echo "   快马 AI CLI - 跨平台构建工具"
    echo "======================================"
    echo ""

    local os=$(detect_os)
    local version="${VERSION:-}"

    if [ -n "$version" ]; then
        info "指定版本: $version"
    fi

    case "$os" in
        macos)
            build_macos
            ;;
        linux)
            build_linux
            ;;
        windows)
            build_windows
            ;;
        *)
            error "未知操作系统: $(uname -s)"
            exit 1
            ;;
    esac
}

# 帮助信息
show_help() {
    cat << EOF
使用方法:
  $0 [选项]

选项:
  -v, --version VERSION    指定版本号
  -d, --dmg              构建 macOS DMG (仅 macOS)
  -h, --help             显示帮助信息

环境变量:
  VERSION                设置版本号
  BUILD_DMG              设置为 "true" 构建 DMG

示例:
  # 基础构建
  $0

  # 指定版本
  $0 --version 1.0.0

  # 构建 macOS DMG
  $0 --dmg

  # 等同于 BUILD_DMG=true $0
  BUILD_DMG=true $0

EOF
}

# 解析参数
while [[ $# -gt 0 ]]; do
    case $1 in
        -v|--version)
            VERSION="$2"
            shift 2
            ;;
        -d|--dmg)
            BUILD_DMG=true
            shift
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            error "未知选项: $1"
            show_help
            exit 1
            ;;
    esac
done

# 运行主函数
main
echo ""
success "构建完成！"