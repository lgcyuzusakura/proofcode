$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$goRoot = if ($env:PROOFCODE_GO_ROOT) { $env:PROOFCODE_GO_ROOT } else { Join-Path $root ".tools\go" }
$wailsBin = if ($env:PROOFCODE_WAILS_BIN) { $env:PROOFCODE_WAILS_BIN } else { Join-Path $root ".tools\bin" }
$env:PATH = (Join-Path $goRoot "bin") + ";" + $wailsBin + ";" + $env:PATH
if (-not $env:GOPROXY) { $env:GOPROXY = "https://goproxy.cn,direct" }
Push-Location (Join-Path $PSScriptRoot "native")
try {
    & (Join-Path $wailsBin "wails.exe") build -clean
    if ($LASTEXITCODE -ne 0) { throw "Native build failed ($LASTEXITCODE)" }
} finally {
    Pop-Location
}
