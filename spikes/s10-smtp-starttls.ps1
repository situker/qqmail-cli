<#
.SYNOPSIS
S10 checks smtp.qq.com:587 EHLO, STARTTLS, strict certificate validation, TLS version, and post-TLS capabilities without authenticating or sending mail.
.DESCRIPTION
No account credential is used. This is a transport-only observation and does not measure quota.
Expected product: spikes/results/s10-*.json with public SMTP/TLS capability metadata.
#>
[CmdletBinding()]
param([string]$Output = "")
& "$PSScriptRoot\Invoke-QQProbe.ps1" -Spike S10 -Output $Output
