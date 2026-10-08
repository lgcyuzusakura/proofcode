param([string]$Go = '')
$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
if (-not $Go) { $Go = Join-Path $repo '.tools/go/bin/go.exe' }
$generated = Join-Path $repo 'agent-engine/cmd/thesis-mechanism-check'
if (Test-Path -LiteralPath $generated) { throw "Refusing to overwrite $generated" }
$results = Join-Path $repo 'thesis/evidence'
New-Item -ItemType Directory -Path $generated,$results -Force | Out-Null
try {
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'mechanism_bench.go') -Destination (Join-Path $generated 'main.go')
    $env:PATH = (Split-Path -Parent $Go) + ';' + $env:PATH
    & $Go -C (Join-Path $repo 'agent-engine') run ./cmd/thesis-mechanism-check (Join-Path $results 'mechanism-results.json')
    if ($LASTEXITCODE -ne 0) { throw 'Mechanism benchmark failed' }
} finally {
    $resolved = [IO.Path]::GetFullPath($generated)
    $expected = [IO.Path]::GetFullPath((Join-Path $repo 'agent-engine/cmd/thesis-mechanism-check'))
    if ($resolved -eq $expected) { Remove-Item -LiteralPath $generated -Recurse -Force }
}
