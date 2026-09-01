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
if (-not $Agent.ok -or $Agent.data.readonly) {
    throw "agent-info default readonly state failed"
}
if ($Agent.data.risk_levels -notcontains "mutate" -or $Agent.data.risk_levels -notcontains "destructive") {
    throw "agent-info risk catalog failed"
}

$PreviousReadonly = $env:QQMAILCTL_READONLY
try {
    $env:QQMAILCTL_READONLY = "1"
    $ReadonlyAgent = (& $Binary agent-info | ConvertFrom-Json)
    if (-not $ReadonlyAgent.ok -or -not $ReadonlyAgent.data.readonly) {
        throw "agent-info readonly environment state failed"
    }
}
finally {
    $env:QQMAILCTL_READONLY = $PreviousReadonly
}

$Schema = (& $Binary schema version | ConvertFrom-Json)
if (-not $Schema.'$schema') {
    throw "embedded schema output failed"
}

Write-Output "qqmailctl PowerShell smoke test passed"
