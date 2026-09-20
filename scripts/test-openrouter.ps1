param([Parameter(Mandatory=$true)][string]$BinaryPath)
$ErrorActionPreference = 'Stop'
$root = Join-Path ([IO.Path]::GetTempPath()) ('clother-openrouter-smoke-' + [guid]::NewGuid().ToString('N'))
foreach ($dir in 'config', 'data', 'cache', 'bin', 'claude', 'home') {
    New-Item -ItemType Directory -Force (Join-Path $root $dir) | Out-Null
}
$env:CLOTHER_CONFIG_DIR = Join-Path $root 'config'
$env:CLOTHER_DATA_DIR = Join-Path $root 'data'
$env:CLOTHER_CACHE_DIR = Join-Path $root 'cache'
$env:CLOTHER_BIN = Join-Path $root 'bin'
$env:CLAUDE_CONFIG_DIR = Join-Path $root 'claude'
$env:USERPROFILE = Join-Path $root 'home'
$env:CLOTHER_SKIP_SELF_UPDATE = '1'
$env:CLOTHER_REAL_CLAUDE = Join-Path $root 'claude-stub.exe'
@'
using System;
class Stub {
    static void Main(string[] args) {
        Console.WriteLine("ARGS=" + string.Join("|", args));
        Console.WriteLine("PROFILE=" + Environment.GetEnvironmentVariable("CLOTHER_PROFILE"));
        Console.WriteLine("ENDPOINT=" + Environment.GetEnvironmentVariable("ANTHROPIC_BASE_URL"));
        Console.WriteLine("MODEL=" + Environment.GetEnvironmentVariable("ANTHROPIC_MODEL"));
        Console.WriteLine("FABLE=" + Environment.GetEnvironmentVariable("ANTHROPIC_DEFAULT_FABLE_MODEL"));
        Console.WriteLine("MAX_OUTPUT=" + Environment.GetEnvironmentVariable("CLAUDE_CODE_MAX_OUTPUT_TOKENS"));
        Console.WriteLine("MAX_CONTEXT=" + Environment.GetEnvironmentVariable("CLAUDE_CODE_MAX_CONTEXT_TOKENS"));
        Console.WriteLine("THINKING=" + Environment.GetEnvironmentVariable("MAX_THINKING_TOKENS"));
        Console.WriteLine("SUBAGENT=" + Environment.GetEnvironmentVariable("CLAUDE_CODE_SUBAGENT_MODEL"));
    }
}
'@ | Set-Content -LiteralPath (Join-Path $root 'stub.cs') -Encoding ascii
& C:\Windows\Microsoft.NET\Framework64\v4.0.30319\csc.exe /nologo /target:exe "/out:$env:CLOTHER_REAL_CLAUDE" (Join-Path $root 'stub.cs')
if ($LASTEXITCODE -ne 0) { throw 'Stub compilation failed' }
'OPENROUTER_API_KEY=test-only-no-network' | Set-Content (Join-Path $env:CLOTHER_DATA_DIR 'secrets.env') -Encoding ascii
$binary = (Resolve-Path -LiteralPath $BinaryPath).Path
& $binary install --bin-dir $env:CLOTHER_BIN --no-shim --no-commands --yes
if ($LASTEXITCODE -ne 0) { throw 'Scratch install failed' }
$launcher = Join-Path $env:CLOTHER_BIN 'clother-openrouter.exe'
if (-not (Test-Path -LiteralPath $launcher)) { throw 'OpenRouter launcher missing' }
$manifest = Get-Content (Join-Path $env:CLOTHER_DATA_DIR 'launchers.json') -Raw | ConvertFrom-Json
if ($manifest.launchers -notcontains 'clother-openrouter.exe') { throw 'Launcher missing from manifest' }
# Deterministic metadata: no external network needed for launcher smoke tests.
$models = foreach ($id in 'anthropic/claude-sonnet-5', 'vendor/test-model') {
    @{
        id = $id; context_length = 32768
        top_provider = @{ max_completion_tokens = 2048 }
        architecture = @{ input_modalities = @('text'); output_modalities = @('text') }
        supported_parameters = @('tools')
    }
}
@{ base_url = 'https://openrouter.ai/api'; fetched_at = [DateTime]::UtcNow.ToString('o'); data = @($models) } |
    ConvertTo-Json -Depth 8 | Set-Content (Join-Path $env:CLOTHER_CACHE_DIR 'openrouter-models.json') -Encoding ascii
$cases = @(
    @{ Args = @('--yolo'); Expected = '--dangerously-skip-permissions' },
    @{ Args = @('--yolo', '--model', 'vendor/test-model', '--print', 'hello world'); Expected = '--dangerously-skip-permissions|--model|vendor/test-model|--print|hello world'; Model = 'vendor/test-model' },
    @{ Args = @('--yolo', '--dangerously-skip-permissions'); Expected = '--dangerously-skip-permissions' }
)
foreach ($case in $cases) {
    $forwarded = $case.Args
    $lines = @(& $launcher --no-banner @forwarded)
    if ($LASTEXITCODE -ne 0) { throw 'Launcher failed' }
    $lines | Write-Output
    if ($lines -notcontains ('ARGS=' + $case.Expected)) { throw 'Argument forwarding mismatch' }
    if ($lines -notcontains 'PROFILE=openrouter') { throw 'Wrong provider' }
    if ($lines -notcontains 'ENDPOINT=https://openrouter.ai/api') { throw 'Wrong endpoint' }
    foreach ($expected in 'MAX_OUTPUT=2048', 'MAX_CONTEXT=32768', 'THINKING=0') {
        if ($lines -notcontains $expected) { throw ('Incorrect model limit: ' + $expected) }
    }
    if ($case.Model) {
        foreach ($role in 'MODEL', 'FABLE', 'SUBAGENT') {
            if ($lines -notcontains ($role + '=' + $case.Model)) { throw 'Model role mismatch' }
        }
    }
}
Write-Output 'PASS: install, manifest, provider, endpoint, --yolo, model override, quoting, duplicate flag'

