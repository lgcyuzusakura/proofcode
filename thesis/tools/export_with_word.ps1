param(
    [string]$DocumentPath = (Join-Path $PSScriptRoot '../output/ProofCode_本科毕业设计论文_初稿.docx'),
    [string]$PdfPath = (Join-Path $PSScriptRoot '../output/ProofCode_本科毕业设计论文_初稿.pdf')
)
$ErrorActionPreference = 'Stop'
$source = [IO.Path]::GetFullPath($DocumentPath)
$pdf = [IO.Path]::GetFullPath($PdfPath)
$word = $null
$doc = $null
try {
    $word = New-Object -ComObject Word.Application
    $word.Visible = $false
    $word.DisplayAlerts = 0
    $doc = $word.Documents.Open($source, $false, $false)
    $findRange = $doc.Content.Duplicate
    $found = $findRange.Find.Execute('TOC_INSERT_POINT')
    if ($found) {
        $location = $findRange.Start
        $findRange.Text = ''
        $tocRange = $doc.Range($location, $location)
        $null = $doc.TablesOfContents.Add($tocRange, $true, 1, 2, $false, '', $true, $true, '', $true, $true, $false)
    }
    $doc.Repaginate()
    foreach ($toc in $doc.TablesOfContents) { $toc.Update() }
    $null = $doc.Fields.Update()
    $doc.Repaginate()
    foreach ($toc in $doc.TablesOfContents) { $toc.UpdatePageNumbers() }
    $doc.Save()
    $doc.ExportAsFixedFormat($pdf, 17)
    $pages = $doc.ComputeStatistics(2)
    $characters = $doc.ComputeStatistics(3)
    Write-Output "Word export complete: $pages pages, $characters characters"
} finally {
    if ($doc) { $doc.Close(0); [Runtime.InteropServices.Marshal]::FinalReleaseComObject($doc) | Out-Null }
    if ($word) { $word.Quit(); [Runtime.InteropServices.Marshal]::FinalReleaseComObject($word) | Out-Null }
}

# Word records the local editing account on save. Clear it before publication.
Add-Type -AssemblyName System.IO.Compression.FileSystem
$archive = [IO.Compression.ZipFile]::Open($source, [IO.Compression.ZipArchiveMode]::Update)
try {
    $entry = $archive.GetEntry('docProps/core.xml')
    if ($entry) {
        $reader = [IO.StreamReader]::new($entry.Open())
        try { [xml]$properties = $reader.ReadToEnd() } finally { $reader.Dispose() }
        $namespaces = [Xml.XmlNamespaceManager]::new($properties.NameTable)
        $namespaces.AddNamespace('dc', 'http://purl.org/dc/elements/1.1/')
        $namespaces.AddNamespace('cp', 'http://schemas.openxmlformats.org/package/2006/metadata/core-properties')
        foreach ($property in $properties.SelectNodes('//dc:creator | //cp:lastModifiedBy', $namespaces)) {
            $property.InnerText = ''
        }
        $metadata = [Text.Encoding]::UTF8.GetBytes($properties.OuterXml)
        $stream = $entry.Open()
        try {
            $stream.SetLength(0)
            $stream.Write($metadata, 0, $metadata.Length)
        } finally { $stream.Dispose() }
    }
} finally { $archive.Dispose() }
Write-Output 'Document author metadata cleared.'
