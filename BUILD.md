# 快马 AI CLI - 构建说明

## 📋 目录

- [环境要求](#环境要求)
- [本地构建](#本地构建)
  - [macOS 构建](#macos-构建)
  - [Windows 构建](#windows-构建)
  - [Linux 构建](#linux-构建)
- [跨平台构建](#跨平台构建)
  - [在 Windows 上构建 macOS 应用](#在-windows-上构建-macos-应用)
  - [在 Linux 上构建 macOS 应用](#在-linux-上构建-macos-应用)
- [WebUI 启动](#webui-启动)
- [常见问题](#常见问题)

## 环境要求

### 通用要求
- Go 1.21 或更高版本
- Git

### 平台特定要求
- **macOS**: Xcode Command Line Tools（用于构建应用包）
- **Windows**: 无额外要求
- **Linux**: 无额外要求

## 本地构建

### macOS 构建

#### 构建命令行版本

```bash
# 基础构建
make build

# 或直接使用 go 命令
go build -o dist/kuaima_cli ./cmd/kuaima_cli
```

#### 构建 macOS 应用包（DMG）

```bash
# 使用构建脚本（推荐）
bash scripts/build_macos_dmg.sh

# 或指定版本号
VERSION=1.0.0 bash scripts/build_macos_dmg.sh

# 生成的文件位置
dist/kuaima_cli-macos-{version}.dmg
```

**注意**: macOS 应用包构建需要以下工具：
- `lipo` - 用于创建通用二进制文件
- `sips` - 用于处理图标
- `iconutil` - 用于创建应用图标
- `hdiutil` - 用于创建 DMG 文件

这些工具通常包含在 Xcode Command Line Tools 中，可通过以下命令安装：
```bash
xcode-select --install
```

### Windows 构建

#### 构建命令行版本

```powershell
# 使用 PowerShell
make build

# 或直接使用 go 命令
go build -o dist/kuaima_cli.exe ./cmd/kuaima_cli

# 编译优化版本
make release
```

#### 构建跨平台 macOS 应用

**⚠️ 重要提示**: Windows 无法直接构建 macOS DMG 文件，需要使用 GitHub Actions。

详细说明请参考：[在 Windows 上构建 macOS 应用](#在-windows-上构建-macos-应用)

### Linux 构建

```bash
# 基础构建
make build

# 或直接使用 go 命令
go build -o dist/kuaima_cli ./cmd/kuaima_cli

# 编译优化版本
make release
```

## 跨平台构建

### 在 Windows 上构建 macOS 应用

由于 Windows 无法直接创建 macOS DMG 文件，我们使用 GitHub Actions 进行云端构建。

#### 前置要求

1. **安装 Git**
   ```bash
   # 使用 Windows Package Manager (winget)
   winget install Git.Git

   # 或访问: https://git-scm.com/download/win
   ```

2. **安装 GitHub CLI（推荐）**
   ```bash
   # 使用 winget
   winget install GitHub.cli

   # 或访问: https://cli.github.com/

   # 安装后登录
   gh auth login
   ```

#### 方法一：使用 PowerShell 脚本（推荐）

```powershell
# 进入项目目录
cd kuaima_cli

# 运行构建脚本
.\scripts\build_macos_dmg_win.ps1

# 或指定版本号
.\scripts\build_macos_dmg_win.ps1 -Version "1.0.0"
```

#### 方法二：使用批处理脚本

```cmd
# 进入项目目录
cd kuaima_cli

# 运行构建脚本
scripts\build_macos_dmg_win.bat

# 或指定版本号
scripts\build_macos_dmg_win.bat 1.0.0
```

#### 方法三：手动触发 GitHub Actions

1. **推送代码到 GitHub**
   ```bash
   git add .
   git commit -m "Build macOS DMG"
   git push origin main
   ```

2. **创建 Git 标签**
   ```bash
   # 创建并推送标签（会自动触发构建）
   git tag -a v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

3. **或手动触发**
   - 访问 GitHub Actions 页面
   - 点击 "Build macOS DMG" 工作流
   - 点击 "Run workflow" 并输入版本号

4. **下载构建产物**
   ```bash
   # 使用 GitHub CLI 下载
   gh release view v1.0.0 --json assets -q '.assets[].browser_download_url'

   # 或从 GitHub Releases 页面手动下载
   ```

#### 监控构建状态

```bash
# 查看最近的构建
gh run list --workflow=build-macos-dmg.yml

# 实时监控构建
gh run watch

# 查看构建详情
gh run view
```

### 在 Linux 上构建 macOS 应用

Linux 上构建 macOS 应用的方法与 Windows 类似，需要使用 GitHub Actions。

#### 前置要求

```bash
# 安装 Git
sudo apt install git  # Ubuntu/Debian
sudo yum install git  # CentOS/RHEL

# 安装 GitHub CLI（推荐）
# 下载地址: https://cli.github.com/
# 安装后登录
gh auth login
```

#### 使用方法

```bash
# 克隆仓库
git clone https://github.com/your-username/kuaima_cli.git
cd kuaima_cli

# 创建标签并触发构建
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0

# 监控构建
gh run watch
```

## WebUI 启动

### macOS

```bash
# 方式一：双击应用
# 打开 dist/kuaima_cli-macos-{version}.dmg
# 拖拽应用到 Applications 文件夹
# 双击 kuaima_cli.app 启动

# 方式二：命令行启动
./dist/kuaima_cli webui

# 方式三：使用 Makefile
make run_web
```

### Windows

```powershell
# 命令行启动
.\dist\kuaima_cli.exe webui

# 或使用 Makefile
make run_web
```

### Linux

```bash
# 命令行启动
./dist/kuaima_cli webui
```

### WebUI 访问

启动后，WebUI 会自动在默认浏览器中打开：
- 本地地址: `http://127.0.0.1:8790`

如果浏览器没有自动打开，请手动访问上述地址。

## 常见问题

### Q: macOS 构建失败，提示找不到命令

**A**: 确保已安装 Xcode Command Line Tools
```bash
xcode-select --install
```

### Q: Windows 上无法构建 macOS DMG

**A**: Windows 不支持直接构建 macOS DMG，请使用 GitHub Actions 进行云端构建。

### Q: GitHub Actions 构建失败

**A**: 检查以下几点：
1. 仓库是否正确配置
2. GitHub Actions 是否已启用
3. 工作流文件路径是否正确：`.github/workflows/build-macos-dmg.yml`
4. 构建日志中的错误信息

### Q: WebUI 无法启动或无法访问

**A**: 检查以下几点：
1. 端口 8790 是否被占用
2. 防火墙是否允许连接
3. 是否有多个实例在运行

### Q: 如何查看程序版本

```bash
./dist/kuaima_cli --version
```

### Q: 如何清理构建产物

```bash
# macOS/Linux
rm -rf dist/

# Windows PowerShell
Remove-Item -Recurse -Force dist\
```

### Q: 如何调试构建问题

```bash
# 启用详细输出
go build -v -x -o dist/kuaima_cli ./cmd/kuaima_cli

# 查看环境变量
go env

# 检查 CGO 是否启用
go env CGO_ENABLED
```

## Makefile 命令参考

```bash
# 构建项目
make build

# 发布版本（优化编译）
make release

# 运行程序
make run

# 运行 WebUI
make run_web

# 测试文本生成
make test

# 测试流式生成
make test_stream

# 测试图片生成
make test_img

# 查询余额
make balance

# 充值
make recharge

# 构建 Skill
make build_skill
```

## 构建产物说明

### 命令行版本
- `dist/kuaima_cli` (macOS/Linux)
- `dist/kuaima_cli.exe` (Windows)
- `dist/kuaima_cli-darwin-amd64` (Intel Mac)
- `dist/kuaima_cli-darwin-arm64` (Apple Silicon Mac)

### 应用包版本
- `dist/kuaima_cli-macos-{version}.dmg` (macOS 安装包)
- `dist/macos-dmg/kuaima_cli.app` (macOS 应用包)

## 技术支持

如果遇到构建问题：

1. 检查环境要求是否满足
2. 查看详细构建日志
3. 参考常见问题部分
4. 提交 Issue 到 GitHub

---

**提示**: 首次构建时，建议先使用 `make build` 测试本地环境是否配置正确。