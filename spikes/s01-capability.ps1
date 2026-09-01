<#
.SYNOPSIS
S1 captures greeting/pre-login/post-login CAPABILITY, unsupported-command BAD behavior, and a metadata-only UID FETCH response shape.
.DESCRIPTION
Prerequisites: a configured account with its authorization code already in the OS keyring. This probe is read-only.
Expected product: spikes/results/s1-*.json. Review and redact conclusions into docs/compat; never commit the result file.
#>
[CmdletBinding()]
param([string]$Account = "", [string]$Config = "", [string]$Output = "")
& "$PSScriptRoot\Invoke-QQProbe.ps1" -Spike S1 -Account $Account -Config $Config -Output $Output
