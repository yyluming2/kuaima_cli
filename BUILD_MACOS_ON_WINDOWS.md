# Windows 上打包 macOS DMG 应用

由于 Windows 无法直接创建 macOS DMG 文件，本项目使用 **GitHub Actions** 在云端进行跨平台构建。

## 🎯 使用方法

### 方式一：自动构建（推荐）

#### 前置要求

1. **安装 Git**
   - 下载地址: https://git-scm.com/download/win

2. **安装 GitHub CLI（可选但推荐）**
   - 下载地址: https://cli.github.com/
   - 安装后运行: `gh auth login`

#### 使用 PowerShell 脚本

```powershell
# 进入项目目录
cd kuaima_cli

# 运行构建脚本
.\scripts\build_macos_dmg_win.ps1

# 或指定版本号
.\scripts\build_macos_dmg_win.ps1 -Version "1.0.0"
```

#### 使用批处理脚本

```cmd
# 进入项目目录
cd kuaima_cli

# 运行构建脚本
scripts\build_macos_dmg_win.bat

# 或指定版本号
scripts\build_macos_dmg_win.bat 1.0.0
```

### 方式二：手动触发

1. **推送代码到 GitHub**
   ```bash
   git push origin main
   ```

2. **创建 Git 标签**
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

3. **或手动触发 GitHub Actions**
   - 访问 GitHub Actions 页面
   - 点击 "Build macOS DMG" 工作流
   - 点击 "Run workflow" 并输入版本号

4. **下载 DMG 文件**
   - 从 GitHub Releases 页面下载
   - 或使用 GitHub CLI:
     ```bash
     gh release view v1.0.0 --json assets -q '.assets[].browser_download_url'
     ```

## 📦 构建产物

构建成功后，会生成以下文件：

- `kuaima_cli-macos-{version}.dmg` - macOS 安装包
- 通用二进制文件（支持 Intel 和 Apple Silicon）
- 完整的 macOS 应用程序包

## 🔧 配置说明

### 修改仓库地址

如果你的 GitHub 仓库地址不是默认的，请在脚本中修改：

**PowerShell 脚本:**
```powershell
[CmdletBinding()]
param(
    [string]$Version = "",
    [string]$RepoUrl = "https://github.com/your-username/kuaima_cli"  # 修改这里
)
```

**批处理脚本:**
```cmd
set "REPO_URL=https://github.com/your-username/kuaima_cli"  REM 修改这里
```

### GitHub Actions 工作流

工作流配置文件位于 `.github/workflows/build-macos-dmg.yml`，包含：

- **自动触发**: 推送带 `v` 前缀的标签时自动构建
- **手动触发**: 可在 GitHub Actions 页面手动触发
- **版本控制**: 支持自定义版本号
- **自动发布**: 构建成功后自动发布到 GitHub Releases

## 🚀 功能特性

- ✅ Windows 兼容
- ✅ 自动版本检测
- ✅ GitHub CLI 集成
- ✅ 构建状态监控
- ✅ 自动下载管理
- ✅ 通用二进制支持（Intel + Apple Silicon）

## 📋 常见问题

### Q: 为什么要使用 GitHub Actions？

A: macOS DMG 文件只能在 macOS 上创建，GitHub Actions 提供了免费的 macOS 环境进行构建。

### Q: 构建需要多长时间？

A: 通常需要 2-5 分钟，取决于网络和 GitHub Actions 的负载。

### Q: 如何监控构建进度？

A: 使用 GitHub CLI:
```bash
# 查看最近的构建
gh run list --workflow=build-macos-dmg.yml

# 实时监控构建
gh run watch
```

### Q: GitHub CLI 未登录怎么办？

A: 运行 `gh auth login` 按提示登录你的 GitHub 账户。

### Q: 可以在 macOS 上直接构建吗？

A: 可以，直接运行 `bash scripts/build_macos_dmg.sh` 即可。

## 📞 技术支持

如果遇到问题，请检查：

1. Git 是否正确安装
2. GitHub CLI 是否已登录（如果使用）
3. 网络连接是否正常
4. GitHub 仓库是否正确配置

---

**提示**: 首次使用时，建议先在 GitHub 上配置好仓库信息，然后按照上述步骤操作。