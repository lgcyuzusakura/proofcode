$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.IO.Compression
$fixture = Join-Path ([IO.Path]::GetTempPath()) ('proofcode-mcp-template-test-' + [guid]::NewGuid().ToString('N'))
$source = Join-Path $fixture 'source'
$output = Join-Path $fixture 'output/template.zip'
$marker = 'SYNTHETIC_PRIVATE_DATA_MUST_NOT_BE_BUNDLED'
$allowed = @('package.json','package-lock.json','.gitignore','README.md','config.example.json','src/config.mjs','src/service.mjs','src/http.mjs','src/cli.mjs','src/init.mjs','tools/knowledge.mjs','knowledge/.gitkeep','tests/service.test.mjs','templates/custom.mjs')
function Write-Fixture([string]$name, [string]$body) {
    $path = Join-Path (Join-Path $source 'mcp-service') $name
    [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($path)) | Out-Null
    [IO.File]::WriteAllText($path, $body, [Text.UTF8Encoding]::new($false))
}
try {
    foreach ($name in $allowed) { Write-Fixture $name 'public fixture' }
    foreach ($name in @('.env','config.local.json','.git/config','knowledge/private.md','knowledge/paper.pdf','thesis/manuscript.docx','src/secret.mjs','node_modules/private.json')) { Write-Fixture $name $marker }
    & (Join-Path $PSScriptRoot 'prepare-mcp-template.ps1') -SourceRoot $source -OutputPath $output
    & (Join-Path $PSScriptRoot 'prepare-mcp-template.ps1') -SourceRoot $source -OutputPath $output
    $stream = [IO.File]::OpenRead($output)
    $zip = [IO.Compression.ZipArchive]::new($stream, [IO.Compression.ZipArchiveMode]::Read)
    try {
        $paths = @()
        foreach ($entry in $zip.Entries) {
            $paths += $entry.FullName
            if ($entry.FullName -notin ($allowed + @('runtime-manifest.json'))) { throw 'Unexpected private path in template' }
            $reader = [IO.StreamReader]::new($entry.Open())
            try { $body = $reader.ReadToEnd() } finally { $reader.Dispose() }
            if ($body.Contains($marker)) { throw 'Template contains synthetic private data' }
        }
        if ($paths.Count -ne 15) { throw 'Template omitted public files or duplicated paths' }
    } finally { $zip.Dispose(); $stream.Dispose() }
    $remaining = @(Get-ChildItem -LiteralPath (Split-Path -Parent $output) -Force)
    if ($remaining.Count -ne 1 -or $remaining[0].Name -ne 'template.zip') { throw 'Template packaging left backups or temporary archives' }
    Write-Output 'PASS MCP template includes only allowlisted code and no synthetic knowledge, secrets, manuscripts, history or temporary backups'
} finally {
    $resolved = [IO.Path]::GetFullPath($fixture)
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\') + '\'
    if (-not $resolved.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase)) { throw 'Unsafe fixture cleanup target' }
    if (Test-Path -LiteralPath $resolved) { Remove-Item -LiteralPath $resolved -Recurse -Force }
}
