param(
    [Parameter(Mandatory=$true)][string]$Destination,
    [string]$Package = '',
    [ValidateRange(1024,65535)][int]$ControlPort = 8080,
    [ValidateRange(1024,65535)][int]$WebPort = 3000
)
$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($Package)) { $Package = Split-Path -Parent $MyInvocation.MyCommand.Path }
if ([string]::IsNullOrWhiteSpace($Package)) { throw 'Could not locate the extracted offline package.' }
$packageRoot = [IO.Path]::GetFullPath($Package)
$target = [IO.Path]::GetFullPath($Destination)
if (Test-Path -LiteralPath $target) { throw 'Destination already exists; existing data was not changed.' }
if ($ControlPort -eq $WebPort) { throw 'API and web ports must differ.' }
function Regular-File([string]$name) {
    $path = Join-Path $packageRoot $name
    $item = Get-Item -LiteralPath $path -Force
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw "Regular file required: $name" }
    return $path
}
function Docker-Checked([string[]]$Arguments) {
    $result = & docker @Arguments
    if ($LASTEXITCODE -ne 0) { throw 'Docker operation failed. Check Docker Desktop Linux containers.' }
    return $result
}
function Fresh-Bytes {
    $bytes = New-Object byte[] 32
    $random = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $random.GetBytes($bytes) } finally { $random.Dispose() }
    return ,$bytes
}
function Fresh-Hex { return [BitConverter]::ToString((Fresh-Bytes)).Replace('-','').ToLowerInvariant() }
$markerPath = Regular-File 'proofcode-offline.json'
if ((Get-Item -LiteralPath $markerPath).Length -gt 16384) { throw 'Manifest too large.' }
$manifest = Get-Content -LiteralPath $markerPath -Raw | ConvertFrom-Json
$services = @('artemis','control-plane','postgres','redis','runner','web')
if ($manifest.version -ne 1 -or $manifest.platform -cne 'linux/amd64' -or $manifest.archive.file -cne 'images.tar') { throw 'Unsupported offline package.' }
$names = @($manifest.images.PSObject.Properties.Name | Sort-Object)
if (($names -join ',') -cne ($services -join ',')) { throw 'Unexpected service set.' }
foreach ($name in $services) { if ($manifest.images.$name -cnotmatch '^sha256:[a-f0-9]{64}$') { throw 'Invalid image ID.' } }
$composePath = Regular-File 'compose.yml'
$archivePath = Join-Path $packageRoot 'images.tar'
if ($manifest.composeSha256 -cnotmatch '^[a-f0-9]{64}$' -or (Get-FileHash -LiteralPath $composePath -Algorithm SHA256).Hash.ToLowerInvariant() -cne $manifest.composeSha256) { throw 'Compose checksum mismatch.' }
if ($manifest.archive.sha256 -cnotmatch '^[a-f0-9]{64}$' -or $manifest.archive.size -lt 1 -or $manifest.archive.size -gt 20GB -or @($manifest.archive.parts).Count -lt 1 -or @($manifest.archive.parts).Count -gt 16) { throw 'Invalid image archive manifest.' }
$partTotal = [long]0
foreach ($part in $manifest.archive.parts) {
    if ($part.file -cnotmatch '^images\.tar\.part[0-9]{3}$' -or $part.sha256 -cnotmatch '^[a-f0-9]{64}$' -or $part.size -lt 1 -or $part.size -gt 600MB) { throw 'Invalid image archive part.' }
    $partPath = Regular-File $part.file
    if ((Get-Item -LiteralPath $partPath).Length -ne $part.size -or (Get-FileHash -LiteralPath $partPath -Algorithm SHA256).Hash.ToLowerInvariant() -cne $part.sha256) { throw 'Image archive part checksum mismatch.' }
    $partTotal += $part.size
}
if ($partTotal -ne $manifest.archive.size) { throw 'Image archive parts do not match declared size.' }
if ((Docker-Checked @('version','--format','{{.Server.Os}}/{{.Server.Arch}}')) -cne 'linux/amd64') { throw 'Use Docker Desktop Linux containers (linux/amd64).' }
# Render without interpolation: no builder-machine secrets or absolute paths.
$config = (Docker-Checked @('compose','-f',$composePath,'config','--no-interpolate','--no-normalize','--no-path-resolution','--format','json')) -join "`n" | ConvertFrom-Json
if ((@($config.services.PSObject.Properties.Name | Sort-Object) -join ',') -cne ($services -join ',')) { throw 'Unexpected Compose services.' }
foreach ($name in $services) {
    $service = $config.services.$name
    if ($service.image -cne $manifest.images.$name -or $null -ne $service.build -or $service.pull_policy -cne 'never') { throw 'Offline Compose must use local images without build or pull.' }
}
Write-Output 'Checksums verified. Loading application images (no containers or data volumes)...'
$assembled = $false
if (-not [IO.File]::Exists($archivePath)) {
    $assembly = [IO.File]::Open($archivePath,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None)
    try {
        foreach ($part in $manifest.archive.parts) {
            $input = [IO.File]::OpenRead((Join-Path $packageRoot $part.file))
            try { $input.CopyTo($assembly) } finally { $input.Dispose() }
        }
    } catch { $assembly.Dispose(); Remove-Item -LiteralPath $archivePath -Force -ErrorAction SilentlyContinue; throw }
    $assembly.Dispose()
    $assembled = $true
}
try {
    if ((Get-Item -LiteralPath $archivePath).Length -ne $manifest.archive.size -or (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant() -cne $manifest.archive.sha256) { throw 'Combined image archive checksum mismatch.' }
    Docker-Checked @('load','--input',$archivePath) | Out-Null
} finally {
    if ($assembled -and [IO.File]::Exists($archivePath)) { Remove-Item -LiteralPath $archivePath -Force }
}
foreach ($name in $services) {
    $id = $manifest.images.$name
    $actual = (Docker-Checked @('image','inspect','--format','{{.Id}} {{.Os}}/{{.Architecture}}',$id)) -join ''
    if ($actual -cne "$id linux/amd64") { throw 'Loaded image ID/platform mismatch.' }
}
$parent = [IO.Path]::GetDirectoryName($target)
[IO.Directory]::CreateDirectory($parent) | Out-Null
$staging = Join-Path $parent ('.proofcode-import-' + [Guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($staging) | Out-Null
try {
    # Windows .env inherits only the importing user's access and SYSTEM.
    if ($env:OS -eq 'Windows_NT') {
        $acl = [Security.AccessControl.DirectorySecurity]::new()
        $acl.SetAccessRuleProtection($true,$false)
        foreach ($sid in @([Security.Principal.WindowsIdentity]::GetCurrent().User,[Security.Principal.SecurityIdentifier]::new('S-1-5-18'))) {
            $rule = [Security.AccessControl.FileSystemAccessRule]::new($sid,'FullControl','ContainerInherit,ObjectInherit','None','Allow')
            $acl.AddAccessRule($rule)
        }
        Set-Acl -LiteralPath $staging -AclObject $acl
    }
    Copy-Item -LiteralPath $composePath -Destination (Join-Path $staging 'compose.yml')
    Copy-Item -LiteralPath $markerPath -Destination (Join-Path $staging 'proofcode-offline.json')
    $lines = @('# Generated locally. Keep private; never rotate the data key after use.',
        "DEV_AUTH_TOKEN=$(Fresh-Hex)","RUNNER_TOKEN=$(Fresh-Hex)","POSTGRES_PASSWORD=$(Fresh-Hex)",
        "ARTEMIS_PASSWORD=$(Fresh-Hex)","DATA_SNAPSHOT_KEY=$([Convert]::ToBase64String((Fresh-Bytes)))",
        'RUNNER_ALLOW_WRITE=false','RUNNER_ALLOW_EXEC=false',"PROOFCODE_CONTROL_PORT=$ControlPort","PROOFCODE_WEB_PORT=$WebPort")
    [IO.File]::WriteAllText((Join-Path $staging '.env'),($lines -join "`n") + "`n",[Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText((Join-Path $staging '.gitignore'),".env`n.env.*`n.proofcode/`n",[Text.UTF8Encoding]::new($false))
    Docker-Checked @('compose','--project-directory',$staging,'-f',(Join-Path $staging 'compose.yml'),'config','--quiet') | Out-Null
    [IO.Directory]::Move($staging,$target)
} finally {
    # Only remove this invocation's freshly created staging directory.
    if ([IO.Directory]::Exists($staging)) { Remove-Item -LiteralPath $staging -Recurse -Force }
}
Write-Output "Prepared: $target"
Write-Output 'Open ProofCode.exe > Runtime > Select deployment directory > Start services.'
Write-Output 'No service was started. Keep the generated .env private and backed up.'
