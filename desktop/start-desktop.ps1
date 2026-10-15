$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$frontend = Join-Path $root "frontend"
$dist = Join-Path $frontend "dist"
$stateDir = Join-Path $root ".proofcode"
$port = 4173
$url = "http://127.0.0.1:$port/?desktop=1"

if (-not (Test-Path (Join-Path $dist "index.html"))) {
    Push-Location $frontend
    try {
        if (-not (Test-Path (Join-Path $frontend "node_modules"))) {
            npm install --registry=https://registry.npmmirror.com --no-audit --no-fund
        }
        npm run build
    } finally {
        Pop-Location
    }
}

$python = (Get-Command python.exe -ErrorAction SilentlyContinue).Source
if (-not $python) {
    $python = "C:\Users\Administrator\.cache\codex-runtimes\codex-primary-runtime\dependencies\python\python.exe"
}
if (-not (Test-Path $python)) { throw "Python runtime was not found" }

$server = Start-Process -FilePath $python -ArgumentList @("-m", "http.server", "$port", "--directory", $dist) -PassThru -WindowStyle Hidden
New-Item -ItemType Directory -Force -Path $stateDir | Out-Null
Set-Content -Path (Join-Path $stateDir "desktop-server.pid") -Value $server.Id
Start-Sleep -Milliseconds 700

$browser = $null
$edge = "C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe"
$chrome = "C:\Program Files\Google\Chrome\Application\chrome.exe"
if (Test-Path $edge) { $browser = $edge } elseif (Test-Path $chrome) { $browser = $chrome }
if (-not $browser) {
    Stop-Process -Id $server.Id -Force -ErrorAction SilentlyContinue
    throw "Microsoft Edge or Google Chrome was not found"
}

$profile = Join-Path $stateDir "desktop-profile"
New-Item -ItemType Directory -Force -Path $profile | Out-Null
Start-Process -FilePath $browser -ArgumentList @("--app=$url", "--user-data-dir=$profile") | Out-Null
Write-Output "ProofCode desktop preview: $url"
Write-Output "Server PID: $($server.Id)"
