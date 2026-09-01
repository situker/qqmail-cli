$ErrorActionPreference = "Stop"
$ProjectDir = Split-Path -Parent $PSScriptRoot

$Required = @(
    "README.md",
    "README.en.md",
    "LICENSE",
    "NOTICE",
    "SECURITY.md",
    "CONTRIBUTING.md",
    "CODE_OF_CONDUCT.md",
    "docs/index.md",
    "docs/INTRODUCTION.md",
    "docs/USER_GUIDE.md",
    "docs/ARCHITECTURE.md",
    "docs/DEVELOPMENT_SUMMARY.md",
    "docs/TESTING.md",
    "docs/RELEASE_CHECKLIST.md"
)

foreach ($Relative in $Required) {
    $Path = Join-Path $ProjectDir $Relative
    if (-not (Test-Path -LiteralPath $Path)) {
        throw "Required public file is missing: $Relative"
    }
}

$PublicRootMarkdown = @(
    "README.md",
    "README.en.md",
    "CHANGELOG.md",
    "CONTRIBUTING.md",
    "SECURITY.md",
    "CODE_OF_CONDUCT.md",
    "IMPLEMENTATION_STATUS.md"
)

$MarkdownFiles = @(
    $PublicRootMarkdown | ForEach-Object { Get-Item -LiteralPath (Join-Path $ProjectDir $_) }
) + @(
    Get-ChildItem -LiteralPath (Join-Path $ProjectDir "docs") -Recurse -File -Filter "*.md"
)

$Broken = @()
foreach ($File in $MarkdownFiles) {
    $Text = Get-Content -LiteralPath $File.FullName -Raw
    foreach ($Match in [regex]::Matches($Text, '\[[^\]]+\]\(([^)]+)\)')) {
        $Target = $Match.Groups[1].Value.Trim('<', '>')
        if ($Target -match '^(https?://|mailto:|#)') {
            continue
        }
        $Target = ($Target -split '#')[0]
        if (-not $Target) {
            continue
        }
        $Resolved = [System.IO.Path]::GetFullPath((Join-Path $File.DirectoryName $Target))
        if (-not (Test-Path -LiteralPath $Resolved)) {
            $Broken += "$($File.FullName) -> $Target"
        }
    }
}
if ($Broken.Count -gt 0) {
    throw "Broken Markdown links:`n$($Broken -join "`n")"
}

$RelativeTimePattern = '今天|昨天|刚刚|最近|上周|today|yesterday|recently'
$RelativeTimeHits = @()
foreach ($File in $MarkdownFiles) {
    $Matches = Select-String -LiteralPath $File.FullName -Pattern $RelativeTimePattern
    foreach ($Match in $Matches) {
        $RelativeTimeHits += "$($File.FullName):$($Match.LineNumber):$($Match.Line.Trim())"
    }
}
if ($RelativeTimeHits.Count -gt 0) {
    throw "Public documentation contains relative time words; use an absolute date:`n$($RelativeTimeHits -join "`n")"
}

$Disclaimer = "标准 IMAP/SMTP 服务工作"
foreach ($Relative in @("README.md", "docs/index.md", ".goreleaser.yaml")) {
    $Path = Join-Path $ProjectDir $Relative
    if ((Get-Content -LiteralPath $Path -Raw) -notmatch [regex]::Escape($Disclaimer)) {
        throw "Third-party disclaimer is missing from $Relative"
    }
}

Write-Output "qqmailctl documentation checks passed ($($MarkdownFiles.Count) Markdown files)"
