$ErrorActionPreference = "Stop"
$ProjectDir = Split-Path -Parent $PSScriptRoot
$Binary = Join-Path $ProjectDir "bin\qqmailctl.exe"

if (-not (Test-Path -LiteralPath $Binary)) {
    throw "Build bin\qqmailctl.exe before running this smoke test."
}

[Console]::OutputEncoding = [Text.Encoding]::UTF8
$Version = (& $Binary version --json | ConvertFrom-Json)
if (-not $Version.ok -or $Version.schema_version -ne "1") {
    throw "version JSON contract failed"
}

$Agent = (& $Binary agent-info | ConvertFrom-Json)
if (-not $Agent.ok -or -not $Agent.data.readonly) {
    throw "agent-info readonly contract failed"
}

$Schema = (& $Binary schema version | ConvertFrom-Json)
if (-not $Schema.'$schema') {
    throw "embedded schema output failed"
}

Write-Output "qqmailctl PowerShell smoke test passed"

