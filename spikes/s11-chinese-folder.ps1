<#
.SYNOPSIS
S11 creates, renames, verifies, and removes one uniquely named Chinese probe folder using Modified UTF-7.
.DESCRIPTION
Never run on a personal mailbox. Requires QQMAILCTL_E2E_WRITE=1, QQMAILCTL_DEDICATED_TEST_ACCOUNT=1, and -ExecuteWriteProbe. It touches only the folders it creates.
Expected product: spikes/results/s11-*.json with CREATE/RENAME/LIST/cleanup statuses and no folder name.
#>
[CmdletBinding()]
param([string]$Account = "", [string]$Config = "", [string]$Output = "", [switch]$ExecuteWriteProbe)
& "$PSScriptRoot\Invoke-QQProbe.ps1" -Spike S11 -Account $Account -Config $Config -Output $Output -WriteProbe:$ExecuteWriteProbe
