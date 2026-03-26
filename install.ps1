# ClientHub Installer for Windows
#
# Usage:
#   irm https://raw.githubusercontent.com/cltx/clienthub/main/install.ps1 | iex
#
#   # Or with parameters:
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/cltx/clienthub/main/install.ps1))) -Channel dev
#
# Parameters:
#   -Channel    stable|dev   (default: stable)
#   -InstallDir PATH         (default: $env:LOCALAPPDATA\clienthub\bin)
#   -Component  all|server|client|hubctl (default: all)

param(
    [ValidateSet("stable", "dev")]
    [string]$Channel = "stable",

    [string]$InstallDir = "$env:LOCALAPPDATA\clienthub\bin",

    [ValidateSet("all", "server", "client", "hubctl")]
    [string]$Component = "all"
)

$ErrorActionPreference = "Stop"
$Repo = "zsai001/clienthub"

function Get-Arch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    switch ($arch) {
        "AMD64" { return "amd64" }
        "ARM64" { return "arm64" }
        default {
            Write-Error "Unsupported architecture: $arch"
            exit 1
        }
    }
}

$Arch = Get-Arch

Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  ClientHub Installer" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Channel:    $Channel"
Write-Host "  OS:         windows"
Write-Host "  Arch:       $Arch"
Write-Host "  Component:  $Component"
Write-Host "  Install to: $InstallDir"
Write-Host ""

# Determine release tag
if ($Channel -eq "dev") {
    $Tag = "dev-latest"
    Write-Host "==> Fetching latest dev build ..."
} else {
    Write-Host "==> Fetching latest stable release ..."
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
        $Tag = $release.tag_name
    } catch {
        Write-Error "Could not find latest stable release. Check https://github.com/$Repo/releases"
        exit 1
    }
}

Write-Host "  Release:    $Tag"
Write-Host ""

$Archive = "clienthub-windows-$Arch.zip"
$DownloadUrl = "https://github.com/$Repo/releases/download/$Tag/$Archive"

$TempDir = Join-Path $env:TEMP "clienthub-install-$(Get-Random)"
New-Item -ItemType Directory -Path $TempDir -Force | Out-Null

try {
    Write-Host "==> Downloading $Archive ..."
    $ArchivePath = Join-Path $TempDir $Archive
    try {
        Invoke-WebRequest -Uri $DownloadUrl -OutFile $ArchivePath -UseBasicParsing
    } catch {
        Write-Host ""
        Write-Error @"
Download failed.
  URL: $DownloadUrl

Possible reasons:
  - No release found for windows/$Arch on the '$Channel' channel
  - Network issue

Available releases: https://github.com/$Repo/releases
"@
        exit 1
    }

    Write-Host "==> Extracting ..."
    Expand-Archive -Path $ArchivePath -DestinationPath $TempDir -Force

    $ExtractedDir = Join-Path $TempDir "clienthub-windows-$Arch"

    Write-Host "==> Installing to $InstallDir ..."
    if (!(Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }

    function Install-Binary {
        param([string]$Name)
        $src = Join-Path $ExtractedDir "$Name.exe"
        if (!(Test-Path $src)) {
            Write-Host "  Warning: $Name.exe not found in archive, skipping" -ForegroundColor Yellow
            return
        }
        $dst = Join-Path $InstallDir "$Name.exe"
        Copy-Item $src $dst -Force
        Write-Host "  Installed: $dst"
    }

    switch ($Component) {
        "all" {
            Install-Binary "hub-server"
            Install-Binary "hub-client"
            Install-Binary "hubctl"
        }
        "server" { Install-Binary "hub-server" }
        "client" { Install-Binary "hub-client" }
        "hubctl" { Install-Binary "hubctl" }
    }

    # Copy example configs
    if ($Component -eq "all") {
        $ConfigDir = Join-Path $env:LOCALAPPDATA "clienthub\config"
        $ExamplesDir = Join-Path $ExtractedDir "examples"
        if ((Test-Path $ExamplesDir) -and !(Test-Path $ConfigDir)) {
            New-Item -ItemType Directory -Path $ConfigDir -Force | Out-Null
            Copy-Item "$ExamplesDir\*" $ConfigDir -Force
            Write-Host "  Examples:  $ConfigDir"
        }
    }

    # Check PATH
    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($UserPath -notlike "*$InstallDir*") {
        Write-Host ""
        Write-Host "  NOTE: $InstallDir is not in your PATH." -ForegroundColor Yellow
        Write-Host "  Adding to user PATH ..."
        $NewPath = "$InstallDir;$UserPath"
        [Environment]::SetEnvironmentVariable("Path", $NewPath, "User")
        $env:Path = "$InstallDir;$env:Path"
        Write-Host "  PATH updated. Restart your terminal for it to take effect."
    }

    # Print version
    Write-Host ""
    $hubctl = Join-Path $InstallDir "hubctl.exe"
    if (Test-Path $hubctl) {
        try {
            $ver = & $hubctl version 2>&1
            Write-Host "  Version: $ver"
        } catch {}
    }

    Write-Host ""
    Write-Host "============================================" -ForegroundColor Green
    Write-Host "  Installation complete!" -ForegroundColor Green
    Write-Host "============================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "  To update, run this script again."
    Write-Host "  To switch channels, use -Channel dev|stable"
    Write-Host ""

} finally {
    Remove-Item -Recurse -Force $TempDir -ErrorAction SilentlyContinue
}
