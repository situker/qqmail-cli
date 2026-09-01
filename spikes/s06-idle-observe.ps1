<#
.SYNOPSIS
S6 observes whether IDLE is accepted and survives for a bounded interval, then exits with DONE.
.DESCRIPTION
Prerequisites: configured keyring account. Read-only. A short success does not justify using IDLE in production; watch remains polling-only.
Expected product: spikes/results/s6-*.json with duration, untagged event count, and DONE status.
#>
[CmdletBinding()]
param([string]$Account = "", [string]$Config = "", [string]$Output = "", [TimeSpan]$Duration = [TimeSpan]::FromSeconds(30))
& "$PSScriptRoot\Invoke-QQProbe.ps1" -Spike S6 -Account $Account -Config $Config -Output $Output -Duration $Duration
