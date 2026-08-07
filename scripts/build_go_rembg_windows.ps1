param([string]$OutputDirectory = "dist\rembg-engine")

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$output = [IO.Path]::GetFullPath((Join-Path $root $OutputDirectory))
$dist = [IO.Path]::GetFullPath((Join-Path $root "dist"))
$runtimeVersion = (Get-Content -LiteralPath (Join-Path $PSScriptRoot "onnxruntime-version.txt") -Raw).Trim()
if (-not $output.StartsWith($dist + [IO.Path]::DirectorySeparatorChar)) {
    throw "The rembg engine output must stay inside dist: $output"
}
if (-not (Get-Command gcc -ErrorAction SilentlyContinue)) { throw "MinGW-w64 GCC is required." }

$stage = "$output-go-stage"
if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }

# Once the Python bundle has been migrated, rebuild only the small Go EXE.
# Models can be multiple gigabytes and must not be duplicated on every build.
$existing = if (Test-Path -LiteralPath $output) { Get-Item -LiteralPath $output -Force } else { $null }
if ($existing -and -not $existing.LinkType -and
    (Test-Path -LiteralPath (Join-Path $output "onnxruntime.dll"))) {
    New-Item -ItemType Directory -Force -Path (Join-Path $output "models") | Out-Null
    $newEngine = Join-Path $output "rembg-engine.new.exe"
    $env:CGO_ENABLED = "1"
    Push-Location $root
    try {
        go build -buildvcs=false -trimpath -ldflags "-s -w" -o $newEngine ./cmd/rembg_engine
        if ($LASTEXITCODE -ne 0) { throw "Go cutout engine build failed." }
    } finally { Pop-Location }
    Move-Item -LiteralPath $newEngine -Destination (Join-Path $output "rembg-engine.exe") -Force
    Write-Host "Go ONNX cutout engine: $output"
    exit 0
}

New-Item -ItemType Directory -Force -Path (Join-Path $stage "models") | Out-Null

$runtimeCandidates = @(
    (Join-Path $output "onnxruntime.dll"),
    (Join-Path $output "_internal\onnxruntime\capi\onnxruntime.dll"),
    (Join-Path $dist "rembg-build-windows-go\onnxruntime-win-x64-$runtimeVersion\lib\onnxruntime.dll")
)
$runtimeDLL = $runtimeCandidates | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
if (-not $runtimeDLL) { throw "onnxruntime.dll was not found. Run 'make offline_windows' once or place it in $output." }
Copy-Item -LiteralPath $runtimeDLL -Destination (Join-Path $stage "onnxruntime.dll") -Force

$oldModels = Join-Path $output "models"
if (Test-Path -LiteralPath $oldModels) {
    Get-ChildItem -LiteralPath $oldModels -Filter "*.onnx" -File -ErrorAction SilentlyContinue |
        Copy-Item -Destination (Join-Path $stage "models") -Force
}

$env:CGO_ENABLED = "1"
Push-Location $root
try {
    go build -buildvcs=false -trimpath -ldflags "-s -w" -o (Join-Path $stage "rembg-engine.exe") ./cmd/rembg_engine
    if ($LASTEXITCODE -ne 0) { throw "Go cutout engine build failed." }
} finally { Pop-Location }

# The old output may be a symlink to a PyInstaller directory. Removing a
# reparse point removes only the link; a normal directory is replaced wholly.
if (Test-Path -LiteralPath $output) {
    $oldOutput = Get-Item -LiteralPath $output -Force
    if ($oldOutput.LinkType) { Remove-Item -LiteralPath $output -Force }
    else { Remove-Item -LiteralPath $output -Recurse -Force }
}
Move-Item -LiteralPath $stage -Destination $output
Write-Host "Go ONNX cutout engine: $output"
