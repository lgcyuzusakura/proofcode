$root = Split-Path -Parent $PSScriptRoot
$pidFile = Join-Path $root ".proofcode\desktop-server.pid"
if (Test-Path $pidFile) {
    $serverId = Get-Content $pidFile | Select-Object -First 1
    Stop-Process -Id ([int]$serverId) -Force -ErrorAction SilentlyContinue
    Remove-Item $pidFile -Force -ErrorAction SilentlyContinue
}
