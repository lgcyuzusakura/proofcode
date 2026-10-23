param(
    [Parameter(Mandatory=$true)][string]$JdkHome,
    [Parameter(Mandatory=$true)][string]$MavenBin,
    [Parameter(Mandatory=$true)][string]$GitArchive,
    [Parameter(Mandatory=$true)][ValidatePattern('^[a-fA-F0-9]{64}$')][string]$GitArchiveSha256,
    [Parameter(Mandatory=$true)][string]$RipgrepArchive,
    [Parameter(Mandatory=$true)][ValidatePattern('^[a-fA-F0-9]{64}$')][string]$RipgrepArchiveSha256,
    [string]$OutputDirectory = (Join-Path $PSScriptRoot 'native\build\standalone')
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$target = [IO.Path]::GetFullPath($OutputDirectory)
if (Test-Path -LiteralPath $target) { throw 'Output directory already exists. Choose a new build directory; existing files are preserved.' }
if ((Get-FileHash -LiteralPath $GitArchive -Algorithm SHA256).Hash -ne $GitArchiveSha256) { throw 'MinGit archive SHA-256 mismatch' }
if ((Get-FileHash -LiteralPath $RipgrepArchive -Algorithm SHA256).Hash -ne $RipgrepArchiveSha256) { throw 'ripgrep archive SHA-256 mismatch' }
$go = if ($env:PROOFCODE_GO_ROOT) { Join-Path $env:PROOFCODE_GO_ROOT 'bin\go.exe' } else { (Get-Command go.exe -ErrorAction Stop).Source }
$env:JAVA_HOME = [IO.Path]::GetFullPath($JdkHome)
$env:PATH = (Join-Path $env:JAVA_HOME 'bin') + ';' + $env:PATH
Push-Location (Join-Path $root 'control-plane')
try {
    & $MavenBin -q -DskipTests package
    if ($LASTEXITCODE -ne 0) { throw 'Control plane build failed' }
} finally { Pop-Location }
New-Item -ItemType Directory -Path $target | Out-Null
Copy-Item -LiteralPath (Join-Path $root 'control-plane\target\control-plane-0.4.0.jar') -Destination (Join-Path $target 'control-plane.jar')
$modules = 'java.se,jdk.crypto.ec,jdk.unsupported,jdk.management,jdk.naming.dns,jdk.zipfs,jdk.charsets'
& (Join-Path $env:JAVA_HOME 'bin\jlink.exe') --add-modules $modules --strip-debug --no-header-files --no-man-pages --compress=2 --output (Join-Path $target 'java')
if ($LASTEXITCODE -ne 0) { throw 'Embedded Java runtime build failed' }
# MinGit is an unmodified upstream binary distribution, with its licenses.
# No files from an installed Git profile, .git history or working directory are copied.
Expand-Archive -LiteralPath $GitArchive -DestinationPath (Join-Path $target 'git')
if (-not (Test-Path -LiteralPath (Join-Path $target 'git\cmd\git.exe'))) { throw 'Expected the standard Windows x64 MinGit ZIP' }
Expand-Archive -LiteralPath $RipgrepArchive -DestinationPath (Join-Path $target 'search')
$search = @(Get-ChildItem -LiteralPath (Join-Path $target 'search') -Filter rg.exe -File -Recurse)
if ($search.Count -ne 1) { throw 'Expected one ripgrep executable' }
Copy-Item -LiteralPath $search[0].FullName -Destination (Join-Path $target 'rg.exe')
Push-Location (Join-Path $root 'agent-engine')
try {
    & $go build -trimpath -ldflags '-s -w' -o (Join-Path $target 'proofcode-runner.exe') ./cmd/runner
    if ($LASTEXITCODE -ne 0) { throw 'Agent runner build failed' }
    & $go build -trimpath -ldflags '-s -w' -o (Join-Path $target 'proofcode-mcp-inspect.exe') ./cmd/proofcode-mcp-inspect
    if ($LASTEXITCODE -ne 0) { throw 'MCP helper build failed' }
} finally { Pop-Location }
$files = @(Get-ChildItem -LiteralPath $target -File -Recurse | ForEach-Object {
    @{ path = $_.FullName.Substring($target.Length + 1).Replace('\','/'); bytes = $_.Length; sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() }
})
$manifest = @{ version = 1; gitArchiveSha256 = $GitArchiveSha256.ToLowerInvariant(); ripgrepArchiveSha256 = $RipgrepArchiveSha256.ToLowerInvariant(); files = $files }
$manifest | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $target 'manifest.json') -Encoding UTF8
Write-Output "Standalone runtime prepared: $target"
