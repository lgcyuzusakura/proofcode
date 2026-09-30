$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$goRoot = Join-Path $root ".tools\go"
$wailsBin = Join-Path $root ".tools\bin"
$env:PATH = (Join-Path $goRoot "bin") + ";" + $wailsBin + ";" + $env:PATH
$env:GOPROXY = "https://goproxy.cn,direct"
$env:NPM_CONFIG_REGISTRY = "https://registry.npmmirror.com"
Push-Location (Join-Path $PSScriptRoot "native")
try {
    & (Join-Path $wailsBin "wails.exe") build -clean
} finally {
    Pop-Location
}
