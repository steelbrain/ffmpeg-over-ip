# install-client.ps1 — Install the ffmpeg-over-ip client in the current directory.
#
# Usage:
#   irm https://ffmpeg-over-ip.com/install-client.ps1 | iex
#
# Re-runs are idempotent: each step (download, ffprobe.exe, config) is
# skipped if its output already exists. Set $env:FOIP_FORCE='1' to re-download.

$ErrorActionPreference = 'Stop'

$Repo   = 'steelbrain/ffmpeg-over-ip'
$Role   = 'client'
$Binary = "ffmpeg-over-ip-$Role.exe"
$Config = "ffmpeg-over-ip.$Role.jsonc"

# --- Platform detection -----------------------------------------------------

# Gate on OS bitness, not process bitness — we never execute the downloaded binary here.
if (-not [Environment]::Is64BitOperatingSystem) {
    throw "Unsupported operating system: 32-bit Windows. Only 64-bit Windows is supported."
}
$Platform = 'windows-amd64'

# --- Helpers ----------------------------------------------------------------

function Read-FromHost {
    param([string]$Prompt)
    try {
        return Read-Host $Prompt
    } catch {
        throw "Unable to prompt user for interactive input. Create $Config manually using the template at https://github.com/$Repo/blob/main/template.ffmpeg-over-ip.$Role.jsonc"
    }
}

function Read-Default {
    param([string]$Label, [string]$Default)
    $value = Read-FromHost "$Label [default=$Default]"
    if ([string]::IsNullOrWhiteSpace($value)) { return $Default }
    return $value
}

function Read-Required {
    param([string]$Label)
    while ($true) {
        $value = Read-FromHost $Label
        if (-not [string]::IsNullOrWhiteSpace($value)) { return $value }
        Write-Host 'Value is required.'
    }
}

# --- Download + extract -----------------------------------------------------

if ((Test-Path $Binary) -and -not $env:FOIP_FORCE) {
    Write-Host "$Binary already exists, skipping download. Set `$env:FOIP_FORCE='1' to overwrite."
} else {
    $url = "https://github.com/$Repo/releases/latest/download/$Platform-ffmpeg-over-ip-$Role.zip"
    $tmpZip = Join-Path ([System.IO.Path]::GetTempPath()) "foip-$([guid]::NewGuid()).zip"
    $tmpExtract = Join-Path ([System.IO.Path]::GetTempPath()) "foip-extract-$([guid]::NewGuid())"
    try {
        Write-Host "Downloading $Platform-ffmpeg-over-ip-$Role.zip ..."
        $ProgressPreference = 'SilentlyContinue'
        Invoke-WebRequest -Uri $url -OutFile $tmpZip -UseBasicParsing
        Write-Host 'Extracting...'
        Expand-Archive -Path $tmpZip -DestinationPath $tmpExtract -Force
        # Older releases nest files in a top-level wrapper dir; newer ones are flat.
        $extractRoot = $tmpExtract
        $wrapper = Join-Path $tmpExtract "$Platform-ffmpeg-over-ip-$Role"
        if (Test-Path -LiteralPath $wrapper -PathType Container) {
            $extractRoot = $wrapper
        }
        Get-ChildItem -Path $extractRoot -Force | Move-Item -Destination . -Force
    } finally {
        Remove-Item -Path $tmpZip -Force -ErrorAction SilentlyContinue
        Remove-Item -Path $tmpExtract -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# Verify the extraction produced what we expect — guards against a malformed
# release artifact or a partial download.
if (-not (Test-Path $Binary)) {
    throw "Expected $Binary in current directory but it's missing. The release zip may be malformed; retry with `$env:FOIP_FORCE='1'."
}

# Strip the Mark-of-the-Web (Zone.Identifier ADS) Expand-Archive carries over
# from the downloaded zip — parallels the macOS quarantine xattr step.
Unblock-File -Path $Binary

# --- ffprobe.exe (copy on Windows) -----------------------------------------

if (Test-Path 'ffprobe.exe') {
    Write-Host 'ffprobe.exe already exists, leaving alone.'
} else {
    Copy-Item -Path $Binary -Destination 'ffprobe.exe'
    Write-Host "Created ffprobe.exe (copy of $Binary)."
}

# --- Config -----------------------------------------------------------------

if (Test-Path $Config) {
    Write-Host "$Config already exists, leaving alone."
} else {
    Write-Host ''
    Write-Host 'Configuring client. These values must match the server.'
    $serverHost = Read-Required 'Server host or IP'
    $serverPort = Read-Default 'Server port' '5050'
    $authSecret = Read-Required 'Auth secret (must match the server)'

    $config = [ordered]@{
        log        = 'stdout'
        address    = "${serverHost}:${serverPort}"
        authSecret = $authSecret
    }
    $json = $config | ConvertTo-Json
    # PS 5.1 escapes <, >, &, ' as \uXXXX in string values. Functionally fine
    # but ugly when users open the file. Convert them back to literals.
    $json = $json `
        -replace '\\u003c', '<' `
        -replace '\\u003e', '>' `
        -replace '\\u0026', '&' `
        -replace '\\u0027', "'"

    $configPath = Join-Path -Path (Get-Location).Path -ChildPath $Config
    $utf8NoBom = New-Object System.Text.UTF8Encoding $false
    [System.IO.File]::WriteAllText($configPath, $json, $utf8NoBom)
    Write-Host "Wrote $Config."
}

Write-Host ''
Write-Host 'Done. Verify with:'
Write-Host "  .\$Binary -version"
