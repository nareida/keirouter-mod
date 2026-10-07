#Requires -Version 5.1
<#
.SYNOPSIS
    KeiRouter installer for Windows.

.DESCRIPTION
    Downloads the latest prebuilt KeiRouter release, extracts the binary and
    dashboard assets into a per-user directory, and adds it to your PATH so you
    can run `keirouter` from any new terminal.

    No Go or Node.js required — this installs a prebuilt binary.

.PARAMETER Version
    Release version to install (e.g. "v0.1.18"). Defaults to the latest release.

.PARAMETER InstallDir
    Where to install. Defaults to %LOCALAPPDATA%\KeiRouter.

.EXAMPLE
    irm https://raw.githubusercontent.com/mydisha/keirouter/main/scripts/install.ps1 | iex

.EXAMPLE
    # Pin a version and a custom directory:
    $env:KEIROUTER_VERSION = "v0.1.18"; $env:KEIROUTER_DIR = "D:\Tools\KeiRouter"
    irm https://raw.githubusercontent.com/mydisha/keirouter/main/scripts/install.ps1 | iex
#>
[CmdletBinding()]
param(
    [string]$Version = $env:KEIROUTER_VERSION,
    [string]$InstallDir = $env:KEIROUTER_DIR
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"  # speeds up Invoke-WebRequest downloads

$Repo = "mydisha/keirouter"

function Write-Info($msg)  { Write-Host "> $msg" -ForegroundColor Cyan }
function Write-Ok($msg)    { Write-Host "OK $msg" -ForegroundColor Green }
function Write-Warn($msg)  { Write-Host "! $msg"  -ForegroundColor Yellow }
function Die($msg)         { Write-Host "ERROR $msg" -ForegroundColor Red; exit 1 }

# ── Banner ───────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "  KeiRouter - Windows installer" -ForegroundColor Green
Write-Host ""

# ── Detect architecture ───────────────────────────────────────────────────────
# Under a 32-bit (WOW64) process on a 64-bit OS, PROCESSOR_ARCHITECTURE reports
# "x86" while PROCESSOR_ARCHITEW6432 holds the real architecture.
$rawArch = $env:PROCESSOR_ARCHITECTURE
if ($env:PROCESSOR_ARCHITEW6432) { $rawArch = $env:PROCESSOR_ARCHITEW6432 }
switch ($rawArch) {
    "AMD64" { $arch = "amd64" }
    "ARM64" { $arch = "arm64" }
    "x86"   { Die "32-bit Windows is not supported. KeiRouter ships amd64 and arm64 builds only." }
    default { Die "Unsupported architecture: $rawArch" }
}
Write-Info "Architecture: $arch"

# ── Resolve version ────────────────────────────────────────────────────────────
if (-not $Version) {
    Write-Info "Resolving latest release..."
    try {
        $headers = @{ "User-Agent" = "keirouter-installer" }
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers $headers
        $Version = $release.tag_name
    } catch {
        Die "Could not resolve the latest release. Set `$env:KEIROUTER_VERSION (e.g. v0.1.18) and retry. ($($_.Exception.Message))"
    }
}
if ($Version -notmatch '^v') { $Version = "v$Version" }
Write-Ok "Version: $Version"

# ── Resolve install dir ────────────────────────────────────────────────────────
if (-not $InstallDir) {
    $InstallDir = Join-Path $env:LOCALAPPDATA "KeiRouter"
}
Write-Info "Install directory: $InstallDir"

# ── Download archive ───────────────────────────────────────────────────────────
$asset = "keirouter_${Version}_windows_${arch}.zip"
$url = "https://github.com/$Repo/releases/download/$Version/$asset"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("keirouter_" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
$zipPath = Join-Path $tmp $asset

Write-Info "Downloading $asset..."
try {
    Invoke-WebRequest -Uri $url -OutFile $zipPath -UseBasicParsing
} catch {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    Die "Download failed from $url`n  ($($_.Exception.Message))"
}
Write-Ok "Downloaded"

# ── Extract ────────────────────────────────────────────────────────────────────
Write-Info "Extracting..."
$extractDir = Join-Path $tmp "extract"
try {
    Expand-Archive -Path $zipPath -DestinationPath $extractDir -Force
} catch {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    Die "Failed to extract archive: $($_.Exception.Message)"
}

# The archive contains a top-level "keirouter/" folder holding keirouter.exe and frontend/.
$payload = Join-Path $extractDir "keirouter"
if (-not (Test-Path (Join-Path $payload "keirouter.exe"))) {
    # Fallback: some archives may not nest under keirouter/.
    if (Test-Path (Join-Path $extractDir "keirouter.exe")) {
        $payload = $extractDir
    } else {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
        Die "keirouter.exe not found in the downloaded archive."
    }
}

# ── Install (stop a running instance first so the .exe isn't locked) ───────────
Get-Process -Name "keirouter" -ErrorAction SilentlyContinue | ForEach-Object {
    Write-Warn "Stopping running keirouter process (PID $($_.Id))"
    try { $_.Kill() } catch {}
}

Write-Info "Installing to $InstallDir..."
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
# Clear previous binary + assets, keep any user data that might live alongside.
Remove-Item (Join-Path $InstallDir "keirouter.exe") -Force -ErrorAction SilentlyContinue
Remove-Item (Join-Path $InstallDir "frontend") -Recurse -Force -ErrorAction SilentlyContinue
Copy-Item -Path (Join-Path $payload "*") -Destination $InstallDir -Recurse -Force

Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
Write-Ok "Installed keirouter.exe and dashboard assets"

# ── Add to PATH (user scope) ───────────────────────────────────────────────────
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
$paths = @()
if ($userPath) { $paths = $userPath.Split(';') | Where-Object { $_ -ne "" } }
if ($paths -notcontains $InstallDir) {
    Write-Info "Adding $InstallDir to your user PATH"
    $newPath = (($paths + $InstallDir) -join ';')
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    # Make it available in the current session too.
    $env:Path = "$env:Path;$InstallDir"
    Write-Ok "PATH updated (open a new terminal for it to take effect everywhere)"
} else {
    Write-Ok "PATH already contains the install directory"
}

# ── Done ───────────────────────────────────────────────────────────────────────
Write-Host ""
Write-Ok "KeiRouter $Version installed."
Write-Host ""
Write-Host "Quick start:" -ForegroundColor Cyan
Write-Host "  keirouter -bootstrap     # create your first API key (shown once)"
Write-Host "  keirouter start          # start the server on :20180"
Write-Host ""
Write-Host "Dashboard: http://localhost:20180  (default password: keirouter)"
Write-Host ""
Write-Warn "Open a NEW terminal window before running 'keirouter' so the PATH change applies."
