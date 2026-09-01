[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Spike,
    [string]$Account = "",
    [string]$Config = "",
    [string]$Output = "",
    [string]$Label = "current",
    [TimeSpan]$Duration = [TimeSpan]::FromSeconds(30),
    [TimeSpan]$Interval = [TimeSpan]::FromSeconds(15),
    [int]$MaxLogins = 3,
    [switch]$WriteProbe
)

$ErrorActionPreference = "Stop"
$ProjectDir = Split-Path -Parent $PSScriptRoot
$Go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $Go) {
    $Go = "C:\Program Files\Go\bin\go.exe"
}
if (-not (Test-Path -LiteralPath $Go)) {
    throw "Go was not found. Install Go or add it to PATH."
}
if (-not $Output) {
    $Stamp = Get-Date -Format "yyyyMMdd-HHmmss"
    $Output = Join-Path $PSScriptRoot ("results\{0}-{1}.json" -f $Spike.ToLowerInvariant(), $Stamp)
}

$Arguments = @(
    "run", ".\spikes\qqprobe",
    "-spike", $Spike,
    "-output", $Output,
    "-label", $Label,
    "-duration", (([int64]$Duration.TotalMilliseconds).ToString() + "ms"),
    "-interval", (([int64]$Interval.TotalMilliseconds).ToString() + "ms"),
    "-max-logins", $MaxLogins
)
if ($Account) { $Arguments += @("-account", $Account) }
if ($Config) { $Arguments += @("-config", $Config) }
if ($WriteProbe) { $Arguments += "-write" }

Push-Location $ProjectDir
try {
    & $Go @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Probe $Spike failed with exit code $LASTEXITCODE. Do not retry authentication failures."
    }
    Write-Host "Result: $Output"
}
finally {
    Pop-Location
}
