$ErrorActionPreference = 'Stop'

$repo = Split-Path -Parent $PSScriptRoot
$outputDir = Join-Path $repo 'src-tauri/binaries'
$outputFile = Join-Path $outputDir 'easy-command-engine-x86_64-pc-windows-msvc.exe'
$temporaryFile = Join-Path $outputDir "easy-command-engine-$PID.tmp"
$envNames = 'GOOS', 'GOARCH', 'CGO_ENABLED'
$previous = @{}
foreach ($name in $envNames) {
    $previous[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

New-Item -ItemType Directory -Path $outputDir -Force | Out-Null
Push-Location (Join-Path $repo 'sidecar')
try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    go build -trimpath -o $temporaryFile ./cmd/easy-command-engine
    if ($LASTEXITCODE -ne 0) { throw "Go build failed with exit code $LASTEXITCODE" }
    Move-Item -LiteralPath $temporaryFile -Destination $outputFile -Force
    Write-Output $outputFile
} finally {
    Pop-Location
    foreach ($name in $envNames) {
        [Environment]::SetEnvironmentVariable($name, $previous[$name], 'Process')
    }
    if (Test-Path -LiteralPath $temporaryFile) {
        Remove-Item -LiteralPath $temporaryFile
    }
}
