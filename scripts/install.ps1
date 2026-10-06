# One-line installer for Windows (PowerShell):
#   irm https://raw.githubusercontent.com/M-SaiCharan/autoportal/main/scripts/install.ps1 | iex
#
# Downloads the latest release into your user folder (no admin rights) and
# starts autoportal. Downloads made this way are not marked as "from the
# internet", so SmartScreen does not interrupt.
$ErrorActionPreference = 'Stop'

$repo = 'M-SaiCharan/autoportal'
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$url  = "https://github.com/$repo/releases/latest/download/autoportal-windows-$arch.exe"
$dir  = Join-Path $env:LOCALAPPDATA 'Programs\autoportal'
$exe  = Join-Path $dir 'autoportal.exe'

New-Item -ItemType Directory -Force -Path $dir | Out-Null
Get-Process autoportal -ErrorAction SilentlyContinue | Stop-Process -Force
Write-Host "Downloading autoportal ($arch)..."
Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $exe
Write-Host "Installed to $exe"
Start-Process $exe
Write-Host 'autoportal is starting. Look for its icon in the system tray (click ^ next to the clock).'
