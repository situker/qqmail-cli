<#
.SYNOPSIS
S2 compares LOGIN with AUTHENTICATE PLAIN and observes RFC 2971 ID behavior.
.DESCRIPTION
Prerequisites: a configured account and keyring credential. It performs two authentication observations without retry; stop if the provider rejects either attempt.
Expected product: spikes/results/s2-*.json containing status codes only, with credentials and account addresses excluded.
#>
[CmdletBinding()]
param([string]$Account = "", [string]$Config = "", [string]$Output = "")
& "$PSScriptRoot\Invoke-QQProbe.ps1" -Spike S2 -Account $Account -Config $Config -Output $Output
