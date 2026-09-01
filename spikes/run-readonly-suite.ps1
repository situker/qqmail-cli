<#
.SYNOPSIS
Runs the safe local read-only observations with minimal QQ login churn.
.DESCRIPTION
Reuses one LOGIN for S1, S2-ID, S3, S6, and S7; uses one AUTHENTICATE observation and one second LOGIN for S5. Also runs unauthenticated S10 STARTTLS. No write gate is accepted.
Expected product: spikes/results/readonly-*.json. Use this for routine local evidence instead of launching each read-only script back-to-back.
#>
[CmdletBinding()]
param([string]$Account = "", [string]$Config = "", [string]$Output = "", [TimeSpan]$IdleDuration = [TimeSpan]::FromSeconds(30))
& "$PSScriptRoot\Invoke-QQProbe.ps1" -Spike READONLY -Account $Account -Config $Config -Output $Output -Duration $IdleDuration
