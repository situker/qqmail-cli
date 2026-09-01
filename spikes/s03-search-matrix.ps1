<#
.SYNOPSIS
S3 observes SEARCH support for ALL, UNSEEN, SINCE, SUBJECT, FROM, OR, HEADER, and UTF-8 Chinese quoted/literal queries.
.DESCRIPTION
Prerequisites: configured keyring account. The selected mailbox is opened with EXAMINE and no flags are changed.
Expected product: spikes/results/s3-*.json with status and result counts, never message identifiers or content.
#>
[CmdletBinding()]
param([string]$Account = "", [string]$Config = "", [string]$Output = "")
& "$PSScriptRoot\Invoke-QQProbe.ps1" -Spike S3 -Account $Account -Config $Config -Output $Output
