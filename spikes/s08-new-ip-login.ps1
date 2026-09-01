<#
.SYNOPSIS
S8 records whether login succeeds from the host/IP on which this script is run.
.DESCRIPTION
Run on the intended new-IP or VPS host; a run on the existing workstation cannot simulate a different source IP. No retries are performed.
Expected product: spikes/results/s8-*.json with a redacted login status and a reminder when further host coverage is needed.
#>
[CmdletBinding()]
param([string]$Account = "", [string]$Config = "", [string]$Output = "")
& "$PSScriptRoot\Invoke-QQProbe.ps1" -Spike S8 -Account $Account -Config $Config -Output $Output
