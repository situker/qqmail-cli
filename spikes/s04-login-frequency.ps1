<#
.SYNOPSIS
S4 runs a low-rate login ladder and stops at the first failure.
.DESCRIPTION
Do not run on a personal mailbox. Requires QQMAIL_CLI_E2E_WRITE=1 and QQMAIL_CLI_DEDICATED_TEST_ACCOUNT=1 plus -ExecuteProbe.
Expected product: spikes/results/s4-*.json. Authentication failures are never retried; wait 10-15 minutes after any provider throttle signal.
#>
[CmdletBinding()]
param(
    [string]$Account = "", [string]$Config = "", [string]$Output = "",
    [ValidateRange(1, 10)][int]$MaxLogins = 3,
    [TimeSpan]$Interval = [TimeSpan]::FromSeconds(15),
    [switch]$ExecuteProbe
)
& "$PSScriptRoot\Invoke-QQProbe.ps1" -Spike S4 -Account $Account -Config $Config -Output $Output -MaxLogins $MaxLogins -Interval $Interval -WriteProbe:$ExecuteProbe
