$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.IO.Compression
$fixture = Join-Path ([IO.Path]::GetTempPath()) ('proofcode-bundle-test-' + [guid]::NewGuid().ToString('N'))
$source = Join-Path $fixture 'source'
$output = Join-Path $fixture 'output/runtime.zip'
$marker = 'PRIVATE_FIXTURE_MUST_NOT_BE_DISTRIBUTED'
function Write-Fixture([string]$name, [string]$body) {
    $path = Join-Path $source $name
    [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($path)) | Out-Null
    [IO.File]::WriteAllText($path, $body, [Text.UTF8Encoding]::new($false))
}
try {
    foreach ($name in @('.dockerignore','agent-engine/Dockerfile','agent-engine/go.mod','agent-engine/go.sum','agent-engine/runner-entrypoint.sh','control-plane/Dockerfile','control-plane/pom.xml','control-plane/settings.xml','frontend/Dockerfile','frontend/package.json','frontend/package-lock.json','frontend/index.html','frontend/tsconfig.json','frontend/vite.config.ts','frontend/pdf-resources.ts','frontend/nginx.conf','desktop/runtime-template/compose.yml','agent-engine/cmd/main.go','agent-engine/internal/agent/agent.go','agent-engine/pkg/config/config.go','control-plane/src/main/java/App.java','control-plane/src/main/resources/application.yml','frontend/src/App.tsx')) {
        Write-Fixture $name 'public code fixture'
    }
    foreach ($name in @('thesis/output/manuscript.docx','thesis/figures/paper.png','.env','.git/config','frontend/src/local-thesis/secret.ts','frontend/src/.proofcode/secret.ts','frontend/src/figures/paper.pdf','frontend/src/figures/paper.docx','agent-engine/cmd/testdata/private.go','agent-engine/internal/agent/agent_test.go')) {
        Write-Fixture $name $marker
    }
    # Run twice to cover atomic replacement and backup cleanup as well as creation.
    & (Join-Path $PSScriptRoot 'prepare-runtime-bundle.ps1') -SourceRoot $source -OutputPath $output
    & (Join-Path $PSScriptRoot 'prepare-runtime-bundle.ps1') -SourceRoot $source -OutputPath $output
    $stream = [IO.File]::OpenRead($output)
    $zip = [IO.Compression.ZipArchive]::new($stream, [IO.Compression.ZipArchiveMode]::Read)
    try {
        $paths = @()
        foreach ($entry in $zip.Entries) {
            $paths += $entry.FullName
            $reader = [IO.StreamReader]::new($entry.Open())
            try { $body = $reader.ReadToEnd() } finally { $reader.Dispose() }
            if ($body.Contains($marker)) { throw 'Packager included private fixture data' }
            if ($entry.FullName -match 'thesis|\.docx$|\.pdf$|\.git/|\.env|testdata|_test\.go$') { throw 'Packager included a forbidden path' }
        }
        foreach ($required in @('compose.yml','frontend/src/App.tsx','agent-engine/cmd/main.go','control-plane/src/main/java/App.java','runtime-manifest.json')) {
            if ($required -notin $paths) { throw 'Packager omitted a required public fixture' }
        }
    } finally { $zip.Dispose(); $stream.Dispose() }
    $remaining = @(Get-ChildItem -LiteralPath (Split-Path -Parent $output) -Force)
    if ($remaining.Count -ne 1 -or $remaining[0].Name -ne 'runtime.zip') { throw 'Packager left temporary archives or backups' }
    Write-Output 'PASS runtime packager excludes synthetic private documents/history/env/test data and leaves only the requested archive after replacement'
} finally {
    if (Test-Path -LiteralPath $fixture) { Remove-Item -LiteralPath $fixture -Recurse -Force }
}
