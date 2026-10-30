$ErrorActionPreference = 'Stop'
$projectDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$secretDir = Join-Path $projectDir '.local'
$secretFile = Join-Path $secretDir 'forum-secrets.xml'

function Read-OptionalSecret([string]$prompt) {
    $value = Read-Host $prompt -AsSecureString
    if ($value.Length -eq 0) { return $null }
    return $value
}

Write-Host 'Local Forum setup. Secrets are encrypted for this Windows account.'
Write-Host 'Leave a provider ID blank to disable that sign-in option.'
$sessionHoursInput = (Read-Host 'Session length in hours [24]').Trim()
$sessionHours = 24
if ($sessionHoursInput -and (-not [int]::TryParse($sessionHoursInput, [ref]$sessionHours) -or $sessionHours -lt 1 -or $sessionHours -gt 720)) {
    throw 'Session length must be a whole number from 1 to 720 hours.'
}
$googleID = (Read-Host 'Google Client ID (optional)').Trim()
$googleSecret = $null
if ($googleID) { $googleSecret = Read-OptionalSecret 'Google Client Secret' }

$githubID = (Read-Host 'GitHub Client ID (optional)').Trim()
$githubSecret = $null
if ($githubID) { $githubSecret = Read-OptionalSecret 'GitHub Client Secret' }

$smtpHost = (Read-Host 'SMTP host (smtp.gmail.com for Gmail; blank for local mail files)').Trim()
$smtpPort = '587'
$smtpUser = ''
$smtpFrom = ''
$smtpPassword = $null
if ($smtpHost) {
    $smtpPortInput = (Read-Host 'SMTP port [587]').Trim()
    if ($smtpPortInput) { $smtpPort = $smtpPortInput }
    $smtpUser = (Read-Host 'SMTP username').Trim()
    $smtpFromInput = (Read-Host "From address [$smtpUser]").Trim()
    if ($smtpFromInput) { $smtpFrom = $smtpFromInput } else { $smtpFrom = $smtpUser }
    $smtpPassword = Read-OptionalSecret 'SMTP/app password'
}

New-Item -ItemType Directory -Force -Path $secretDir | Out-Null
[pscustomobject]@{
    SessionTTL = "${sessionHours}h"
    GoogleClientID = $googleID
    GoogleClientSecret = $googleSecret
    GitHubClientID = $githubID
    GitHubClientSecret = $githubSecret
    SMTPHost = $smtpHost
    SMTPPort = $smtpPort
    SMTPUser = $smtpUser
    SMTPFrom = $smtpFrom
    SMTPPassword = $smtpPassword
} | Export-Clixml -LiteralPath $secretFile

Write-Host 'Settings saved to .local/forum-secrets.xml.'
Write-Host 'Run .\start-forum.ps1 to start the forum.'
