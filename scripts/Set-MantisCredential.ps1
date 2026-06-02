param(
    [string]$CredentialPath = ".\secrets\mantis-credential.xml"
)

$ErrorActionPreference = "Stop"

$resolvedPath = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($CredentialPath)
$dir = Split-Path -Parent $resolvedPath
if (-not (Test-Path -LiteralPath $dir)) {
    New-Item -ItemType Directory -Path $dir | Out-Null
}

do {
    $userName = Read-Host "Mantis username"
} while ([string]::IsNullOrWhiteSpace($userName))

$password = Read-Host "Mantis password" -AsSecureString
$credential = [pscredential]::new($userName, $password)
$credential | Export-Clixml -LiteralPath $resolvedPath

Write-Host "Mantis credential saved to: $resolvedPath"
Write-Host "This file can only be decrypted by the current Windows user on this computer."
