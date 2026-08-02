# Installer for sv-memory (Windows PowerShell 5.1+)
# Downloads a prebuilt binary from GitHub Releases and installs it to
# %LOCALAPPDATA%\sv-memory, adding it to the user PATH.
#
# Usage:
#   iwr -useb https://raw.githubusercontent.com/svtech-code/sv-memory/main/install.ps1 | iex
#
# Options (environment variables):
#   $env:SV_MEMORY_VERSION      pin a release tag instead of "latest" (e.g. v0.1.0)
#   $env:SV_MEMORY_INSTALL_DIR  override the install directory (default %LOCALAPPDATA%\sv-memory)
$ErrorActionPreference = "Stop"

$Repo = "svtech-code/sv-memory"
$Binary = "sv-memory.exe"
$Version = if ($env:SV_MEMORY_VERSION) { $env:SV_MEMORY_VERSION } else { "latest" }
$InstallDir = if ($env:SV_MEMORY_INSTALL_DIR) { $env:SV_MEMORY_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "sv-memory" }

# Detect architecture (use the 64-bit architecture even under WOW64)
$RawArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
switch ($RawArch) {
    "AMD64" { $Arch = "amd64" }
    "ARM64" {
        Write-Host "⚠️  Windows ARM64 builds are not published yet. Install from source instead:" -ForegroundColor Yellow
        Write-Host "   go install github.com/svtech-code/sv-memory/cmd/sv-memory@latest"
        exit 1
    }
    default {
        Write-Host "❌ Unsupported architecture: $RawArch" -ForegroundColor Red
        exit 1
    }
}

if ($Version -eq "latest") {
    $Url = "https://github.com/$Repo/releases/latest/download/sv-memory_windows_${Arch}.zip"
} else {
    $Url = "https://github.com/$Repo/releases/download/${Version}/sv-memory_windows_${Arch}.zip"
}

Write-Host "📥 Downloading sv-memory (windows/$Arch)${Version}..."
$Zip = Join-Path $env:TEMP "sv-memory-$([guid]::NewGuid()).zip"
try {
    Invoke-WebRequest -Uri $Url -OutFile $Zip -UseBasicParsing
} catch {
    Write-Host "❌ Failed to download $Url" -ForegroundColor Red
    Write-Host "   Make sure the release exists, or install from source:"
    Write-Host "   go install github.com/svtech-code/sv-memory/cmd/sv-memory@latest"
    exit 1
}

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Expand-Archive -Path $Zip -DestinationPath $InstallDir -Force
Remove-Item $Zip -Force

# Add to the user PATH if missing (takes effect in new terminals)
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$InstallDir*") {
    $NewPath = if ([string]::IsNullOrEmpty($UserPath)) { $InstallDir } else { "$UserPath;$InstallDir" }
    [Environment]::SetEnvironmentVariable("Path", $NewPath, "User")
    Write-Host "➕ Added $InstallDir to your user PATH (new terminals only)." -ForegroundColor Green
}

Write-Host ""
Write-Host "✅ sv-memory installed to $InstallDir"
Write-Host "   Open a new terminal and run 'sv-memory --help' to get started."
