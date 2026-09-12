# Clother installer for Windows.
#
# Mirrors scripts/install.sh: same environment variables, same default command,
# same checksum asymmetry. Run it from cmd.exe or PowerShell:
#
#   powershell -NoProfile -ExecutionPolicy Bypass -Command "irm https://raw.githubusercontent.com/starbuck100/clother/main/scripts/install.ps1 | iex"
#
# or from a checkout, where it builds and runs from source instead:
#
#   .\scripts\install.ps1 -BinDir "$env:USERPROFILE\bin"
#
# The exit code is clother's own.

[CmdletBinding()]
param(
    # Install the provider launchers but leave `claude` exactly as it is.
    [switch]$SkipClaudeShim,

    # Where to put the launchers. Spelled out as its own parameter because a
    # `--bin-dir` sitting in $Arguments depends on how PowerShell's binder
    # classifies a double-dashed token, and that is not something the caller
    # should have to reason about.
    [string]$BinDir,

    # Everything else goes to clother verbatim. Quoting $env:CLOTHER_BIN is the
    # way to set this for the one-liner, which has no script to pass a parameter
    # to.
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Arguments
)

$ErrorActionPreference = 'Stop'

# PowerShell 5.1 still negotiates TLS 1.0 by default in some configurations,
# and GitHub refuses that. Older .NET has no such default to correct, and
# PowerShell 7 uses the OS default, where this is a no-op.
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
} catch {
    # Nothing to do: the runtime already negotiates something modern.
}

$Repo = if ($env:CLOTHER_REPO) { $env:CLOTHER_REPO } else { 'starbuck100/clother' }
$Version = if ($env:CLOTHER_VERSION) { $env:CLOTHER_VERSION } else { 'latest' }
$ReleaseBaseUrl = $env:CLOTHER_RELEASE_BASE_URL
$InstallMode = if ($env:CLOTHER_INSTALL_MODE) { $env:CLOTHER_INSTALL_MODE } else { 'auto' }

if (-not $Arguments -or $Arguments.Count -eq 0) {
    $Arguments = @('install')
}
if ($SkipClaudeShim -and $Arguments -notcontains '--no-shim') {
    $Arguments += '--no-shim'
}
if ($BinDir -and $Arguments -notcontains '--bin-dir') {
    $Arguments += @('--bin-dir', $BinDir)
}

function Get-DownloadUrl {
    param([string]$Asset)
    if ($ReleaseBaseUrl) {
        return "$($ReleaseBaseUrl.TrimEnd('/'))/$Asset"
    }
    if ($Version -eq 'latest') {
        return "https://github.com/$Repo/releases/latest/download/$Asset"
    }
    return "https://github.com/$Repo/releases/download/$Version/$Asset"
}

function Get-RemoteFile {
    param([string]$Url, [string]$Path)
    # WebClient rather than Invoke-WebRequest: it does not depend on the IE
    # engine (which fails outright on Server Core), it is markedly faster
    # because it does not buffer the response for the progress bar, and it
    # follows the redirect to the release CDN by default.
    $client = New-Object System.Net.WebClient
    try {
        $client.Headers.Add('User-Agent', 'clother-install')
        $client.DownloadFile($Url, $Path)
    } finally {
        $client.Dispose()
    }
}

function Get-ExpectedChecksum {
    param([string]$ChecksumsPath, [string]$Asset)
    foreach ($line in [IO.File]::ReadAllLines($ChecksumsPath)) {
        $fields = @($line -split '\s+' | Where-Object { $_ -ne '' })
        if ($fields.Count -lt 2) { continue }
        # sha256sum marks binary mode with a leading asterisk.
        $name = [IO.Path]::GetFileName($fields[-1].TrimStart('*'))
        if ($name -eq $Asset) { return $fields[0] }
    }
    return $null
}

# From a checkout, build and run the working tree instead of a release. Guarded
# on $PSScriptRoot because the one-liner pipes this file into Invoke-Expression,
# where there is no script path to resolve. CLOTHER_INSTALL_MODE=release skips it.
if ($InstallMode -ne 'release' -and $PSScriptRoot) {
    $sourceDir = Split-Path -Parent $PSScriptRoot
    if ((Test-Path (Join-Path $sourceDir 'go.mod')) -and (Test-Path (Join-Path $sourceDir 'cmd/clother'))) {
        if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
            throw 'go is required to build clother from source'
        }
        Push-Location $sourceDir
        try {
            & go run ./cmd/clother @Arguments
            exit $LASTEXITCODE
        } finally {
            Pop-Location
        }
    }
}

# PROCESSOR_ARCHITEW6432 is what a 32-bit process sees on 64-bit Windows, and
# it is checked first so that a 32-bit PowerShell reports the real architecture
# rather than the emulated one.
$archRaw = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
switch ($archRaw) {
    'AMD64' { $arch = 'amd64' }
    'ARM64' { $arch = 'arm64' }
    default { throw "unsupported architecture: $archRaw" }
}

$asset = "clother_windows_${arch}.zip"
$checksums = 'checksums.txt'

$tmpDir = Join-Path ([IO.Path]::GetTempPath()) ("clother-install-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null

try {
    $assetPath = Join-Path $tmpDir $asset
    $checksumsPath = Join-Path $tmpDir $checksums

    Get-RemoteFile -Url (Get-DownloadUrl $asset) -Path $assetPath
    Get-RemoteFile -Url (Get-DownloadUrl $checksums) -Path $checksumsPath

    # The same asymmetry as install.sh: a missing checksum *tool* is a warning,
    # because the download can still be run; a missing checksum *entry* is fatal,
    # because it means the release is not the one we think we are installing.
    if (-not (Get-Command Get-FileHash -ErrorAction SilentlyContinue)) {
        Write-Warning 'no checksum tool found, skipping verification'
    } else {
        $expected = Get-ExpectedChecksum -ChecksumsPath $checksumsPath -Asset $asset
        if (-not $expected) {
            throw "checksum for $asset not found in $checksums"
        }
        $actual = (Get-FileHash -LiteralPath $assetPath -Algorithm SHA256).Hash
        if ($actual -ne $expected.ToUpperInvariant()) {
            throw "checksum mismatch for $asset"
        }
    }

    $extractDir = Join-Path $tmpDir 'extract'
    Expand-Archive -LiteralPath $assetPath -DestinationPath $extractDir -Force

    $exe = Join-Path $extractDir 'clother.exe'
    if (-not (Test-Path -LiteralPath $exe)) {
        throw "clother.exe not found in $asset"
    }
    # The download carries the mark-of-the-web, which would otherwise make
    # Windows hesitate over running it.
    Unblock-File -LiteralPath $exe -ErrorAction SilentlyContinue

    # The archive above is what was checksum-verified. Letting `clother install`
    # fetch a release of its own would replace it with a binary nobody verified
    # here, so the self-update is suppressed for this one process.
    $previousSkipUpdate = $env:CLOTHER_SKIP_SELF_UPDATE
    $env:CLOTHER_SKIP_SELF_UPDATE = '1'
    try {
        & $exe @Arguments
        $exitCode = $LASTEXITCODE
    } finally {
        $env:CLOTHER_SKIP_SELF_UPDATE = $previousSkipUpdate
    }
} finally {
    Remove-Item -LiteralPath $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
}

exit $exitCode
