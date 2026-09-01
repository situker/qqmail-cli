<#
.SYNOPSIS
S5 observes UIDVALIDITY across two sessions and, only behind both safety gates, tests COPYUID and UID EXPUNGE on a synthetic message in probe-created folders.
.DESCRIPTION
The read-only phase runs by default. The write phase requires QQMAILCTL_E2E_WRITE=1, QQMAILCTL_DEDICATED_TEST_ACCOUNT=1, and -ExecuteWriteProbe.
Expected product: spikes/results/s5-*.json. The write phase never sends bare EXPUNGE and cleans only uniquely named folders it created.
#>
[CmdletBinding()]
param([string]$Account = "", [string]$Config = "", [string]$Output = "", [switch]$ExecuteWriteProbe)
& "$PSScriptRoot\Invoke-QQProbe.ps1" -Spike S5 -Account $Account -Config $Config -Output $Output -WriteProbe:$ExecuteWriteProbe
