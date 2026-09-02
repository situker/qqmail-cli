<#
.SYNOPSIS
S9 sends a very low-rate sequence of synthetic self-addressed messages through the v0.3 send command.
.DESCRIPTION
Never run on a personal mailbox. Requires QQMAIL_CLI_E2E_WRITE=1, QQMAIL_CLI_DEDICATED_TEST_ACCOUNT=1, QQMAIL_CLI_TEST_RECIPIENT equal to the configured account, and interactive confirmation for every send.
Expected product: terminal statuses to summarize manually in docs/compat. Stop immediately on any rate-limit or authentication response; never retry.
#>
[CmdletBinding()]
param([ValidateRange(1, 5)][int]$Count = 3, [ValidateRange(30, 600)][int]$IntervalSeconds = 60)

$ErrorActionPreference = "Stop"
if ($env:QQMAIL_CLI_E2E_WRITE -ne "1" -or $env:QQMAIL_CLI_DEDICATED_TEST_ACCOUNT -ne "1") {
    Write-Output "S9 pending: set both write safety gates only for a dedicated test account."
    exit 0
}
if (-not $env:QQMAIL_CLI_TEST_RECIPIENT) {
    throw "QQMAIL_CLI_TEST_RECIPIENT is required and must be the dedicated account itself."
}
$ProjectDir = Split-Path -Parent $PSScriptRoot
$Binary = Join-Path $ProjectDir "bin\qqmail-cli.exe"
if (-not (Test-Path -LiteralPath $Binary)) { throw "Build bin\qqmail-cli.exe first." }
$utf8 = New-Object System.Text.UTF8Encoding($false)
[Console]::OutputEncoding = $utf8
$OutputEncoding = $utf8
$Accounts = & $Binary account list --json | ConvertFrom-Json
$Default = $Accounts.data.accounts | Where-Object { $_.is_default } | Select-Object -First 1
if (-not $Default -or $Default.email -ne $env:QQMAIL_CLI_TEST_RECIPIENT) {
    throw "Recipient must exactly match the configured default dedicated test account."
}
for ($Index = 1; $Index -le $Count; $Index++) {
    $Stamp = Get-Date -Format "yyyyMMdd-HHmmss"
    & $Binary send --to $env:QQMAIL_CLI_TEST_RECIPIENT --subject "qqmail-cli S9 synthetic probe $Stamp" --body "Synthetic self-addressed quota probe $Index of $Count." --execute
    if ($LASTEXITCODE -ne 0) { throw "S9 stopped at attempt $Index. Do not retry." }
    if ($Index -lt $Count) { Start-Sleep -Seconds $IntervalSeconds }
}
