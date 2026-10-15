$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $root "desktop\native\build\bin\native.exe"
if (-not (Test-Path $exe)) {
    powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $root "desktop\build-native.ps1")
}
Start-Process -FilePath $exe | Out-Null
