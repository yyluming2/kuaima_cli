param(
    [string]$SkillName = "kuaima-gpt-image-2",
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"
$skillSource = Join-Path (Join-Path $root "skill") $SkillName
$version = $Version
if ([string]::IsNullOrWhiteSpace($version)) {
    $version = $env:VERSION
}

if ([string]::IsNullOrWhiteSpace($version)) {
    $version = "0.1.0"
}
if ($version.StartsWith("v")) {
    $version = $version.Substring(1)
}
$cliOutput = Join-Path $dist "kuaima_cli.exe"
$cliDarwinArm64 = Join-Path $dist "kuaima_cli-darwin-arm64"
$cliDarwinAmd64 = Join-Path $dist "kuaima_cli-darwin-amd64"
$cliLinuxArm64 = Join-Path $dist "kuaima_cli-linux-arm64"
$cliLinuxAmd64 = Join-Path $dist "kuaima_cli-linux-amd64"

function Invoke-GoBuild {
    param(
        [string]$GoOS,
        [string]$GoArch,
        [string]$Output,
        [string]$PackagePath
    )

    $previousGoOS = $env:GOOS
    $previousGoArch = $env:GOARCH
    $previousCgoEnabled = $env:CGO_ENABLED
    try {
        if ($GoOS -eq "") { Remove-Item Env:GOOS -ErrorAction SilentlyContinue } else { $env:GOOS = $GoOS }
        if ($GoArch -eq "") { Remove-Item Env:GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH = $GoArch }
        $env:CGO_ENABLED = "0"
        go build -buildvcs=false -trimpath -ldflags "-s -w -X kuaima_cli/internal/app.AppVersion=$version" -o $Output $PackagePath
    }
    finally {
        if ($null -eq $previousGoOS) { Remove-Item Env:GOOS -ErrorAction SilentlyContinue } else { $env:GOOS = $previousGoOS }
        if ($null -eq $previousGoArch) { Remove-Item Env:GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH = $previousGoArch }
        if ($null -eq $previousCgoEnabled) { Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue } else { $env:CGO_ENABLED = $previousCgoEnabled }
    }
}

function Reset-Directory {
    param([string]$Path)
    if (Test-Path -LiteralPath $Path) {
        Remove-Item -LiteralPath $Path -Recurse -Force
    }
    New-Item -ItemType Directory -Force -Path $Path | Out-Null
}

function Copy-ItemToDirectory {
    param(
        [string]$Source,
        [string]$DestinationDirectory,
        [string]$DestinationName
    )
    Copy-Item -LiteralPath $Source -Destination (Join-Path $DestinationDirectory $DestinationName) -Force
}

if (-not (Test-Path -LiteralPath $skillSource)) {
    throw "Skill source directory not found: $skillSource"
}

New-Item -ItemType Directory -Force -Path $dist | Out-Null

Push-Location $root
try {
    Invoke-GoBuild "" "" $cliOutput "./cmd/kuaima_cli"
    Invoke-GoBuild "darwin" "arm64" $cliDarwinArm64 "./cmd/kuaima_cli"
    Invoke-GoBuild "darwin" "amd64" $cliDarwinAmd64 "./cmd/kuaima_cli"
    Invoke-GoBuild "linux" "arm64" $cliLinuxArm64 "./cmd/kuaima_cli"
    Invoke-GoBuild "linux" "amd64" $cliLinuxAmd64 "./cmd/kuaima_cli"
}
finally {
    Pop-Location
}

$distFull = [System.IO.Path]::GetFullPath($dist)
$legacySkillOutput = Join-Path $dist $SkillName
$legacyZipOutput = Join-Path $dist "$SkillName.zip"
if (Test-Path -LiteralPath $legacySkillOutput) { Remove-Item -LiteralPath $legacySkillOutput -Recurse -Force }
if (Test-Path -LiteralPath $legacyZipOutput) { Remove-Item -LiteralPath $legacyZipOutput -Force }

function New-SkillPackage {
    param(
        [string]$StagePath,
        [string]$ZipPath,
        [string]$PlatformName,
        [string]$SkillMarkdown,
        [string[]]$ScriptFiles,
        [hashtable[]]$BinaryFiles
    )

    $stageFull = [System.IO.Path]::GetFullPath($StagePath)
    if (-not $stageFull.StartsWith($distFull + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to write a skill package outside dist: $stageFull"
    }

    if (Test-Path -LiteralPath $ZipPath) {
        Remove-Item -LiteralPath $ZipPath -Force
    }
    Reset-Directory -Path $stageFull

    Copy-Item -LiteralPath (Join-Path $skillSource "agents") -Destination (Join-Path $stageFull "agents") -Recurse -Force

    $scriptDir = Join-Path $stageFull "scripts"
    New-Item -ItemType Directory -Force -Path $scriptDir | Out-Null
    foreach ($script in $ScriptFiles) {
        Copy-Item -LiteralPath (Join-Path $skillSource $script) -Destination $scriptDir -Force
    }

    $binDir = Join-Path $stageFull "bin"
    New-Item -ItemType Directory -Force -Path $binDir | Out-Null
    foreach ($binary in $BinaryFiles) {
        Copy-ItemToDirectory -Source $binary.Source -DestinationDirectory $binDir -DestinationName $binary.Name
    }

    Copy-Item -LiteralPath (Join-Path $skillSource $SkillMarkdown) -Destination (Join-Path $stageFull "SKILL.md") -Force
    Compress-Archive -LiteralPath $stageFull -DestinationPath $ZipPath -Force
    Write-Host "Skill package [$PlatformName]: $stageFull"
    Write-Host "Skill archive [$PlatformName]: $ZipPath"
}

$platformPackages = @(
    @{
        PlatformName = "Windows"
        Stage = Join-Path $dist "$SkillName-windows"
        Zip = Join-Path $dist "$SkillName-windows.zip"
        SkillMarkdown = "SKILL.windows.md"
        Scripts = @("scripts/kuaima-image.ps1", "scripts/kuaima-recharge.ps1")
        Binaries = @(
            @{ Source = $cliOutput; Name = "kuaima_cli.exe" }
        )
    }
    @{
        PlatformName = "macOS"
        Stage = Join-Path $dist "$SkillName-darwin"
        Zip = Join-Path $dist "$SkillName-darwin.zip"
        SkillMarkdown = "SKILL.darwin.md"
        Scripts = @("scripts/kuaima-image.sh", "scripts/kuaima-recharge.sh")
        Binaries = @(
            @{ Source = $cliDarwinArm64; Name = "kuaima_cli-darwin-arm64" }
            @{ Source = $cliDarwinAmd64; Name = "kuaima_cli-darwin-amd64" }
        )
    }
    @{
        PlatformName = "Linux"
        Stage = Join-Path $dist "$SkillName-linux"
        Zip = Join-Path $dist "$SkillName-linux.zip"
        SkillMarkdown = "SKILL.linux.md"
        Scripts = @("scripts/kuaima-image.sh", "scripts/kuaima-recharge.sh")
        Binaries = @(
            @{ Source = $cliLinuxArm64; Name = "kuaima_cli-linux-arm64" }
            @{ Source = $cliLinuxAmd64; Name = "kuaima_cli-linux-amd64" }
        )
    }
)

foreach ($package in $platformPackages) {
    New-SkillPackage -StagePath $package.Stage -ZipPath $package.Zip -PlatformName $package.PlatformName -SkillMarkdown $package.SkillMarkdown -ScriptFiles $package.Scripts -BinaryFiles $package.Binaries
}
