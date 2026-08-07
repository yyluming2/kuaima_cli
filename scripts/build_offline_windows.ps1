param([string]$Version = "0.2.0")

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"
$work = Join-Path $dist "rembg-build-windows-go"
$stage = Join-Path $dist "kuaima-cli-windows-offline"
$engine = Join-Path $stage "rembg-engine"
$models = Join-Path $engine "models"
$usageGuideName = (-join [char[]](0x5FEB, 0x9A6C)) + "AI" + (-join [char[]](0x4F7F, 0x7528, 0x8BF4, 0x660E)) + ".txt"
$usageGuide = Join-Path $root $usageGuideName
$runtimeVersion = "1.28.0"
$runtimeArchive = Join-Path $work "onnxruntime-win-x64-$runtimeVersion.zip"
$runtimeDir = Join-Path $work "onnxruntime-win-x64-$runtimeVersion"
$localRuntimeDLL = Join-Path $dist "rembg-engine\onnxruntime.dll"
$modelURLs = @{
#    "isnet-general-use.onnx" = "https://github.com/danielgatis/rembg/releases/download/v0.0.0/isnet-general-use.onnx"
#    "birefnet-general-lite.onnx" = "https://github.com/danielgatis/rembg/releases/download/v0.0.0/birefnet-general-lite.onnx"
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw "Go is required." }
if (-not (Get-Command gcc -ErrorAction SilentlyContinue)) {
    throw "A MinGW-w64 GCC is required to build the Go ONNX Runtime binding on Windows."
}
New-Item -ItemType Directory -Force -Path $dist, $work | Out-Null

if (Test-Path -LiteralPath $localRuntimeDLL) {
    $runtimeDLL = $localRuntimeDLL
} else {
    if (-not (Test-Path -LiteralPath $runtimeArchive)) {
        Invoke-WebRequest "https://github.com/microsoft/onnxruntime/releases/download/v$runtimeVersion/onnxruntime-win-x64-$runtimeVersion.zip" -OutFile $runtimeArchive
    }
    if (-not (Test-Path -LiteralPath $runtimeDir)) { Expand-Archive -LiteralPath $runtimeArchive -DestinationPath $work -Force }
    $runtimeDLL = Join-Path $runtimeDir "lib\onnxruntime.dll"
}
if (-not (Test-Path -LiteralPath $runtimeDLL)) { throw "ONNX Runtime DLL not found: $runtimeDLL" }

$modelCache = Join-Path $work "models"
New-Item -ItemType Directory -Force -Path $modelCache | Out-Null
foreach ($item in $modelURLs.GetEnumerator()) {
    $target = Join-Path $modelCache $item.Key
    if (-not (Test-Path -LiteralPath $target)) { Invoke-WebRequest $item.Value -OutFile $target }
}

if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
New-Item -ItemType Directory -Force -Path $engine, $models | Out-Null
$env:CGO_ENABLED = "1"
go build -buildvcs=false -trimpath -ldflags "-s -w" -o (Join-Path $engine "rembg-engine.exe") ./cmd/rembg_engine
if ($LASTEXITCODE -ne 0) { throw "Go cutout engine build failed." }
go build -buildvcs=false -trimpath -ldflags "-s -w -H=windowsgui -X kuaima_cli/internal/app.AppVersion=$Version" -o (Join-Path $stage "kuaima_cli.exe") ./cmd/kuaima_cli
if ($LASTEXITCODE -ne 0) { throw "Application build failed." }
Copy-Item -LiteralPath $runtimeDLL -Destination (Join-Path $engine "onnxruntime.dll") -Force
Copy-Item -Path (Join-Path $modelCache "*.onnx") -Destination $models -Force
Copy-Item -LiteralPath $usageGuide -Destination $stage -Force

$archive = Join-Path $dist "kuaima-cli-v$Version-windows-amd64-offline.zip"
if (Test-Path -LiteralPath $archive) { Remove-Item -LiteralPath $archive -Force }
Compress-Archive -Path (Join-Path $stage "*") -DestinationPath $archive -CompressionLevel Optimal
Write-Host "Offline customer package: $archive"
