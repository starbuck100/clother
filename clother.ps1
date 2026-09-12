# Bootstrap for clother on Windows, the counterpart of clother.sh.
#
# From a checkout it runs the local installer; otherwise it downloads
# scripts/install.ps1 and runs that. Arguments are passed through verbatim.
#
#   .\clother.ps1 install
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\clother.ps1 status

[CmdletBinding()]
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Arguments
)

$ErrorActionPreference = 'Stop'

try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
} catch {
    # Already modern, or nothing to configure.
}

$localInstaller = Join-Path $PSScriptRoot 'scripts/install.ps1'
if (Test-Path -LiteralPath $localInstaller) {
    & $localInstaller @Arguments
    exit $LASTEXITCODE
}

$bootstrapUrl = if ($env:CLOTHER_BOOTSTRAP_URL) {
    $env:CLOTHER_BOOTSTRAP_URL
} else {
    'https://raw.githubusercontent.com/starbuck100/clother/main/scripts/install.ps1'
}

$tmpDir = Join-Path ([IO.Path]::GetTempPath()) ("clother-bootstrap-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null
try {
    $bootstrapPath = Join-Path $tmpDir 'install.ps1'
    $client = New-Object System.Net.WebClient
    try {
        $client.Headers.Add('User-Agent', 'clother-install')
        $client.DownloadFile($bootstrapUrl, $bootstrapPath)
    } finally {
        $client.Dispose()
    }
    Unblock-File -LiteralPath $bootstrapPath -ErrorAction SilentlyContinue

    & $bootstrapPath @Arguments
    exit $LASTEXITCODE
} finally {
    Remove-Item -LiteralPath $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
}
