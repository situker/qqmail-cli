<#
.SYNOPSIS
S7 captures a privacy-preserving mailbox visibility snapshot for comparison across QQ web collection-option changes.
.DESCRIPTION
Run once with -Label before, change exactly one web setting manually, then run with -Label after. The probe never changes web settings or stores folder names/message IDs.
Expected product: two spikes/results/s7-*.json files whose counts and UID-set hashes can be compared.
#>
[CmdletBinding()]
param([string]$Account = "", [string]$Config = "", [string]$Output = "", [string]$Label = "current")
& "$PSScriptRoot\Invoke-QQProbe.ps1" -Spike S7 -Account $Account -Config $Config -Output $Output -Label $Label
