param(
    [string]$SourceRoot = (Split-Path -Parent $PSScriptRoot),
    [string]$OutputPath = (Join-Path $PSScriptRoot 'native\mcp-assets\template.zip')
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.IO.Compression
$source = [IO.Path]::GetFullPath((Join-Path $SourceRoot 'mcp-service'))
$output = [IO.Path]::GetFullPath($OutputPath)
if ([IO.Path]::GetExtension($output) -ne '.zip') { throw 'Template output must be ZIP' }
# This list never traverses the user knowledge directory or local configuration.
$paths = @('package.json','package-lock.json','.gitignore','README.md','config.example.json','src/config.mjs','src/service.mjs','src/http.mjs','src/cli.mjs','src/init.mjs','tools/knowledge.mjs','knowledge/.gitkeep','tests/service.test.mjs','templates/custom.mjs')
$parent = [IO.Path]::GetDirectoryName($output)
[IO.Directory]::CreateDirectory($parent) | Out-Null
$temporary = Join-Path $parent ([IO.Path]::GetRandomFileName() + '.zip')
$backup = Join-Path $parent ([IO.Path]::GetRandomFileName() + '.old')
$entries = @()
[long]$total = 0
try {
    $stream = [IO.File]::Open($temporary, [IO.FileMode]::CreateNew)
    $archive = [IO.Compression.ZipArchive]::new($stream, [IO.Compression.ZipArchiveMode]::Create)
    try {
        foreach ($name in $paths) {
            $cursor = $source
            if ((Get-Item -LiteralPath $cursor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Linked template source rejected' }
            foreach ($part in $name.Split('/')) {
                $cursor = Join-Path $cursor $part
                if ((Get-Item -LiteralPath $cursor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Linked template source rejected' }
            }
            if ((Get-Item -LiteralPath $cursor -Force).PSIsContainer) { throw 'Template source must be a regular file' }
            $bytes = [IO.File]::ReadAllBytes($cursor)
            $total += $bytes.Length
            if ($bytes.Length -gt 8MB -or $total -gt 16MB) { throw 'Template source exceeds bundle limit' }
            $algorithm = [Security.Cryptography.SHA256]::Create()
            try { $hash = [BitConverter]::ToString($algorithm.ComputeHash($bytes)).Replace('-','').ToLowerInvariant() } finally { $algorithm.Dispose() }
            $entries += @{ path = $name; size = $bytes.Length; sha256 = $hash }
            $destination = $archive.CreateEntry($name, [IO.Compression.CompressionLevel]::Optimal).Open()
            try { $destination.Write($bytes, 0, $bytes.Length) } finally { $destination.Dispose() }
        }
        $manifest = @{ version = 1; release = '0.1.0'; files = $entries } | ConvertTo-Json -Depth 5 -Compress
        $bytes = [Text.UTF8Encoding]::new($false).GetBytes($manifest)
        $destination = $archive.CreateEntry('runtime-manifest.json').Open()
        try { $destination.Write($bytes, 0, $bytes.Length) } finally { $destination.Dispose() }
    } finally { $archive.Dispose(); $stream.Dispose() }
    if ([IO.File]::Exists($output)) { [IO.File]::Replace($temporary, $output, $backup) } else { [IO.File]::Move($temporary, $output) }
} finally {
    if ([IO.File]::Exists($temporary)) { [IO.File]::Delete($temporary) }
    if ([IO.File]::Exists($backup)) { [IO.File]::Delete($backup) }
}
Write-Output "Prepared MCP template: $($paths.Count) files, $total bytes; $output"
