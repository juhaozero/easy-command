[CmdletBinding()]
param(
    [ValidateSet('nsis', 'msi', 'all')]
    [string] $Bundle = 'nsis',
    [string] $ConfigOverride
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$target = 'x86_64-pc-windows-msvc'

$bundles = switch ($Bundle) {
    'all' { @('nsis', 'msi') }
    default { @($Bundle) }
}

$tauriArgs = @('tauri', 'build', '--ci', '--target', $target, '--bundles') + ($bundles -join ',')
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
    foreach ($name in $bundles) {
        Write-Output (Join-Path $root "src-tauri/target/$target/release/bundle/$name")
    }
} finally {
    Pop-Location
}
