# kuaima_cli

gpt-image-2 的cli生图工具，支持文生图、参考图、涂抹局部编辑

支持webui直接双击启动使用

也可接入各大Agent智能体工作流

<img src="./cmd/kuaima_cli/kuaima_cli.jpg">

## 构建

详细的构建说明请参考：[BUILD.md](./BUILD.md)

### 快速开始

```bash
# macOS/Linux/Windows
make build

# 运行程序
make run

# 启动 WebUI
make run_web
```

### macOS 应用包构建

```bash
# 构建 macOS DMG 安装包
bash scripts/build_macos_dmg.sh

# 或指定版本号
VERSION=1.0.0 bash scripts/build_macos_dmg.sh
```

### Windows 构建

```powershell
# 构建命令行版本
make build

# 在 Windows 上构建 macOS 应用（使用 GitHub Actions）
.\scripts\build_macos_dmg_win.ps1 -Version "1.0.0"
```

## 使用

直接输入提示词并打印返回结果：

```bash
kuaima_cli.exe "背诵一下滕王阁序"
```

从标准输入读取提示词：

```bash
cat prompt.txt | kuaima_cli.exe
```

输入本地文本、图片或图片 URL，`-file` 可以添加多个；提示词里的 `@路径` 也会按文件类型自动处理：

```bash
kuaima_cli.exe -file ./notes.md -file ./photo.png -file "https://example.com/cat.jpg" "结合 @./extra.txt 描述这些内容"
```

返回结果里的图片，默认会保存到当前目录,  也可以指定保存目录

```bash
kuaima_cli.exe -save-images ./outputs "生成一张海报"
```

需要启用图片生成工具时：

```bash
kuaima_cli.exe -image-generation -save-images ./outputs "生成一张白色咖啡杯产品图"
```

图片子命令会自动启用图片生成，并用 `-save-images` 保存响应中的所有图片：

```bash
kuaima_cli.exe image -save-images ./outputs "a clean product render of a white coffee mug"
kuaima_cli.exe image -image-size 1536x1024 -image-quality high -image-count 2 -image-output-format webp -image-output-compression 80 -save-images ./outputs "a clean product render of a white coffee mug"
kuaima_cli.exe image -file ./reference.png -image-mask ./mask.png -save-images ./outputs "replace only the masked area with a ceramic handle"
kuaima_cli.exe image -file ./reference.png -save-images ./outputs "生成这张参考图的产品海报版本"
```

兼容子命令：

```bash
kuaima_cli.exe ask "你好"
kuaima_cli.exe chat
kuaima_cli.exe image "a clean product render of a white coffee mug"
```
