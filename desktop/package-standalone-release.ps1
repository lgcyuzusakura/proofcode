param(
    [Parameter(Mandatory=$true)][string]$Executable,
    [Parameter(Mandatory=$true)][string]$StandaloneDirectory,
    [Parameter(Mandatory=$true)][string]$OutputZip
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.IO.Compression
function Full([string]$path){ return [IO.Path]::GetFullPath($path) }
$exe=Full $Executable; $sidecar=Full $StandaloneDirectory; $out=Full $OutputZip
if(-not (Test-Path -LiteralPath $exe -PathType Leaf)){throw 'Windows executable is missing'}
if(-not (Test-Path -LiteralPath (Join-Path $sidecar 'manifest.json') -PathType Leaf)){throw 'Standalone manifest is missing'}
$manifest=Get-Content -LiteralPath (Join-Path $sidecar 'manifest.json') -Raw | ConvertFrom-Json
if($manifest.version -ne 1 -or -not $manifest.files -or $manifest.files.Count -gt 12000){throw 'Standalone manifest is invalid'}
$entries=[Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
foreach($file in $manifest.files){
  $name=[string]$file.path
  if($name -match '(^|/)(\.git|\.env[^/]*|thesis|credentials[^/]*|auth\.json)(/|$)|论文|論文|[\\:]' -or $name -match '(^|/)\.\.?(/|$)' -or $name.StartsWith('/') -or $name -notmatch '^(java/|git/|search/|control-plane\.jar$|proofcode-runner\.exe$|proofcode-mcp-inspect\.exe$|rg\.exe$)'){throw "Unexpected runtime file: $name"}
  if(-not $entries.Add($name)){throw 'Duplicate runtime path'}
  $path=Join-Path $sidecar $name
  $cursor=$sidecar
  foreach($component in $name.Split('/')){$cursor=Join-Path $cursor $component;if((Get-Item -LiteralPath $cursor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint){throw 'Linked runtime file is not distributable'}}
  if((Get-Item -LiteralPath $path).Length -ne $file.bytes -or (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash -ne $file.sha256){throw "Runtime hash mismatch: $name"}
}
$actual=@(Get-ChildItem -LiteralPath $sidecar -File -Recurse)
if($actual.Count -ne $entries.Count+1){throw 'Unexpected files exist outside the runtime manifest'}
$parent=[IO.Path]::GetDirectoryName($out);[IO.Directory]::CreateDirectory($parent)|Out-Null
$temp=Join-Path $parent ([IO.Path]::GetRandomFileName()+'.zip')
try {
  $stream=[IO.File]::Open($temp,[IO.FileMode]::CreateNew);$archive=[IO.Compression.ZipArchive]::new($stream,[IO.Compression.ZipArchiveMode]::Create)
  try {
    foreach($item in @(@{path='ProofCode.exe';source=$exe})){ $entry=$archive.CreateEntry($item.path,[IO.Compression.CompressionLevel]::Optimal);$input=[IO.File]::OpenRead($item.source);$output=$entry.Open();try{$input.CopyTo($output)}finally{$output.Dispose();$input.Dispose()} }
    foreach($relative in @($entries)+@('manifest.json')){$entry=$archive.CreateEntry(('standalone/'+$relative),[IO.Compression.CompressionLevel]::Optimal);$input=[IO.File]::OpenRead((Join-Path $sidecar $relative));$output=$entry.Open();try{$input.CopyTo($output)}finally{$output.Dispose();$input.Dispose()} }
  } finally {$archive.Dispose();$stream.Dispose()}
  if(Test-Path -LiteralPath $out){Remove-Item -LiteralPath $out -Force};Move-Item -LiteralPath $temp -Destination $out
} finally {if(Test-Path -LiteralPath $temp){Remove-Item -LiteralPath $temp -Force}}
Write-Output "Standalone desktop package: $out"
