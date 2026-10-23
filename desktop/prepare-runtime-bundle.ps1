param(
    [string]$SourceRoot = (Split-Path -Parent $PSScriptRoot),
    [string]$OutputPath = (Join-Path $PSScriptRoot "native\runtime-assets\runtime.zip")
)
$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.IO.Compression
$source = [IO.Path]::GetFullPath($SourceRoot)
$output = [IO.Path]::GetFullPath($OutputPath)
$files = [Collections.Generic.SortedDictionary[string,string]]::new([StringComparer]::Ordinal)

function Relative-Source([string]$path) {
    $full = [IO.Path]::GetFullPath($path)
    $prefix = $source.TrimEnd('\','/') + [IO.Path]::DirectorySeparatorChar
    if (-not $full.StartsWith($prefix,[StringComparison]::OrdinalIgnoreCase)) { throw "Source escaped repository" }
    return $full.Substring($prefix.Length).Replace('\','/')
}
function Source-Hash([byte[]]$bytes) {
    $algorithm = [Security.Cryptography.SHA256]::Create()
    try { return [BitConverter]::ToString($algorithm.ComputeHash($bytes)).Replace('-','').ToLowerInvariant() } finally { $algorithm.Dispose() }
}
function Assert-NoLinks([string]$path) {
    $relative = Relative-Source $path
    if ($relative.StartsWith('../') -or [IO.Path]::IsPathRooted($relative)) { throw "Source escaped repository" }
    $cursor = $source
    foreach ($part in $relative.Split('/')) {
        $cursor = Join-Path $cursor $part
        if ((Get-Item -LiteralPath $cursor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Linked source rejected: $relative" }
    }
}
function Add-Source([string]$relative, [string]$entry = $relative) {
    $path = Join-Path $source $relative
    Assert-NoLinks $path
    if ((Get-Item -LiteralPath $path -Force).PSIsContainer) { throw "Source must be a regular file: $relative" }
    $files.Add($entry.Replace('\','/'), $path)
}
foreach ($name in @('.dockerignore','agent-engine/Dockerfile','agent-engine/.dockerignore','agent-engine/go.mod','agent-engine/go.sum','agent-engine/runner-entrypoint.sh','control-plane/Dockerfile','control-plane/pom.xml','control-plane/settings.xml','frontend/Dockerfile','frontend/.dockerignore','frontend/package.json','frontend/package-lock.json','frontend/index.html','frontend/tsconfig.json','frontend/vite.config.ts','frontend/pdf-resources.ts','frontend/nginx.conf')) {
    if ((Test-Path -LiteralPath (Join-Path $source $name)) -or $name -notlike '*/.dockerignore') { Add-Source $name }
}
Add-Source 'desktop/runtime-template/compose.yml' 'compose.yml'
$trees = @{
    'agent-engine/cmd' = @('.go'); 'agent-engine/internal' = @('.go'); 'agent-engine/pkg' = @('.go')
    'control-plane/src/main/java' = @('.java'); 'control-plane/src/main/resources' = @('.sql','.yml','.yaml','.properties','.xml')
    'frontend/src' = @('.ts','.tsx','.css','.svg')
}
foreach ($tree in $trees.Keys) {
    $directory = Join-Path $source $tree
    $pending = [Collections.Generic.Stack[string]]::new()
    $pending.Push($directory)
    while ($pending.Count -gt 0) {
        $current = $pending.Pop()
        Assert-NoLinks $current
        foreach ($item in Get-ChildItem -LiteralPath $current -Force) {
            if ($item.Name -match '^(\.|node_modules$|target$|build$|dist$|testdata$|fixtures$)' -or $item.Name -match 'thesis|论文|論文') { continue }
            if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Linked source rejected" }
            if ($item.PSIsContainer) { $pending.Push($item.FullName); continue }
            if ($item.Extension -notin $trees[$tree] -or $item.Name -match '(_test\.go|\.test\.tsx?|\.spec\.tsx?)$') { continue }
            $relative = Relative-Source $item.FullName
            Add-Source $relative
        }
    }
}
if ([IO.Path]::GetExtension($output) -ne '.zip') { throw "Bundle output must be a .zip file" }
if ($files.Count -gt 8192) { throw "Runtime source file count exceeds limit" }
$entries = @()
[long]$total = 0
foreach ($entry in $files.GetEnumerator()) {
    $bytes = [IO.File]::ReadAllBytes($entry.Value)
    if ($bytes.Length -gt 8MB) { throw "Runtime source file exceeds size limit: $($entry.Key)" }
    $total += $bytes.Length
    $entries += @{ path = $entry.Key; size = $bytes.Length; sha256 = (Source-Hash $bytes) }
}
if ($total -gt 64MB) { throw "Runtime source exceeds 64 MB" }
$manifest = @{ version = 1; release = '0.4.0'; files = $entries } | ConvertTo-Json -Depth 5 -Compress
$parent = [IO.Path]::GetDirectoryName($output)
[IO.Directory]::CreateDirectory($parent) | Out-Null
$temporary = Join-Path $parent ([IO.Path]::GetRandomFileName() + '.zip')
$backup = Join-Path $parent ([IO.Path]::GetRandomFileName() + '.old')
try {
    $stream = [IO.File]::Open($temporary, [IO.FileMode]::CreateNew)
    $archive = [IO.Compression.ZipArchive]::new($stream, [IO.Compression.ZipArchiveMode]::Create)
    try {
        foreach ($entry in $files.GetEnumerator()) {
            # Re-read after hashing and reject a changing source before publishing.
            $bytes = [IO.File]::ReadAllBytes($entry.Value)
            $expected = $entries | Where-Object { $_.path -eq $entry.Key } | Select-Object -First 1
            if ((Source-Hash $bytes) -ne $expected.sha256) { throw "Runtime source changed during packaging" }
            $destination = $archive.CreateEntry($entry.Key, [IO.Compression.CompressionLevel]::Optimal).Open()
            try { $destination.Write($bytes, 0, $bytes.Length) } finally { $destination.Dispose() }
        }
        $destination = $archive.CreateEntry('runtime-manifest.json').Open()
        try { $bytes = [Text.UTF8Encoding]::new($false).GetBytes($manifest); $destination.Write($bytes, 0, $bytes.Length) } finally { $destination.Dispose() }
    } finally { $archive.Dispose(); $stream.Dispose() }
    if ([IO.File]::Exists($output)) { [IO.File]::Replace($temporary, $output, $backup) } else { [IO.File]::Move($temporary, $output) }
} finally {
    if ([IO.File]::Exists($temporary)) { [IO.File]::Delete($temporary) }
    if ([IO.File]::Exists($backup)) { [IO.File]::Delete($backup) }
}
Write-Output "Prepared runtime source bundle: $($files.Count) files, $total bytes; $output"
