$ErrorActionPreference = 'Stop'
$projectDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$secretFile = Join-Path $projectDir '.local\forum-secrets.xml'

if (-not (Test-Path -LiteralPath $secretFile)) {
    throw "No saved local settings. Run .\setup-forum.ps1 first."
}

$settings = Import-Clixml -LiteralPath $secretFile

function Unprotect-LocalSecret($value) {
    if ($null -eq $value) { return '' }
    return [System.Net.NetworkCredential]::new('', $value).Password
}

$env:FORUM_HTTP = '1'
$env:PUBLIC_BASE_URL = 'http://localhost:4000'
if (-not $env:SESSION_TTL) {
    $env:SESSION_TTL = if ($settings.SessionTTL) { [string]$settings.SessionTTL } else { '24h' }
}
$env:GOOGLE_CLIENT_ID = [string]$settings.GoogleClientID
$env:GOOGLE_CLIENT_SECRET = Unprotect-LocalSecret $settings.GoogleClientSecret
$env:GITHUB_CLIENT_ID = [string]$settings.GitHubClientID
$env:GITHUB_CLIENT_SECRET = Unprotect-LocalSecret $settings.GitHubClientSecret
$env:SMTP_PORT = if ($settings.SMTPPort) { [string]$settings.SMTPPort } else { '587' }
$env:SMTP_USER = [string]$settings.SMTPUser
$env:SMTP_FROM = [string]$settings.SMTPFrom
$env:SMTP_PASSWORD = Unprotect-LocalSecret $settings.SMTPPassword
if ($settings.SMTPHost) {
    $env:SMTP_HOST = [string]$settings.SMTPHost
    Remove-Item Env:FORUM_DEV_MAIL -ErrorAction SilentlyContinue
} else {
    Remove-Item Env:SMTP_HOST -ErrorAction SilentlyContinue
    $env:FORUM_DEV_MAIL = '1'
}

Set-Location -LiteralPath $projectDir
$app = Join-Path $projectDir 'forum-v7.exe'
if (-not (Test-Path -LiteralPath $app)) {
    throw "forum-v7.exe not found. Build it first: `$env:CGO_ENABLED = '1'; go build -o forum-v7.exe ./cmd"
}
& $app
