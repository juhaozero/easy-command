[CmdletBinding()]
param(
    [string] $ConfigOverride
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$target = 'x86_64-pc-windows-msvc'

if ([string]::IsNullOrWhiteSpace($env:TAURI_SIGNING_PRIVATE_KEY)) {
    throw 'Set TAURI_SIGNING_PRIVATE_KEY to an external key file or inject it from your CI secret store.'
}

$keyPath = $env:TAURI_SIGNING_PRIVATE_KEY
if ([IO.Path]::IsPathRooted($keyPath)) {
    $keyFile = (Resolve-Path -LiteralPath $keyPath).Path
    $rootPrefix = $root.TrimEnd('\') + '\'
    if ($keyFile.StartsWith($rootPrefix, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Keep the updater private key outside the repository.'
    }
    $env:TAURI_SIGNING_PRIVATE_KEY = $keyFile
}

$tauriArgs = @('tauri', 'build', '--ci', '--target', $target, '--bundles', 'nsis')
if ($ConfigOverride) {
    $tauriArgs += @('--config', (Resolve-Path -LiteralPath $ConfigOverride).Path)
}

Push-Location -LiteralPath $root
try {
    pnpm install --frozen-lockfile
    if ($LASTEXITCODE -ne 0) { throw "pnpm install failed with exit code $LASTEXITCODE" }
    & (Join-Path $PSScriptRoot 'build-sidecar.ps1')
    pnpm @tauriArgs
    if ($LASTEXITCODE -ne 0) { throw "Tauri build failed with exit code $LASTEXITCODE" }
    Write-Output (Join-Path $root "src-tauri/target/$target/release/bundle/nsis")
} finally {
    Pop-Location
}
