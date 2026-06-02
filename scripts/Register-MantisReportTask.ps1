param(
    [string]$TaskName = "MantisReport",
    [int]$Minutes = 10
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$cmdPath = Join-Path $repoRoot "MantisReport.cmd"

if (-not (Test-Path -LiteralPath $cmdPath)) {
    throw "MantisReport.cmd not found: $cmdPath"
}

$action = New-ScheduledTaskAction `
    -Execute $cmdPath `
    -WorkingDirectory $repoRoot

$trigger = New-ScheduledTaskTrigger `
    -Once `
    -At (Get-Date).AddMinutes(1) `
    -RepetitionInterval (New-TimeSpan -Minutes $Minutes) `
    -RepetitionDuration (New-TimeSpan -Days 3650)

$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries

Register-ScheduledTask `
    -TaskName $TaskName `
    -Action $action `
    -Trigger $trigger `
    -Settings $settings `
    -Description "Update and open the Mantis HTML report" `
    -Force | Out-Null

Write-Host "Scheduled task created: $TaskName. Runs MantisReport.cmd every $Minutes minute(s)."
