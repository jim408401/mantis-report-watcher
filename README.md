# Mantis Report Watcher

This tool reads a configured Mantis worklist, records the issue IDs that have already been seen, and shows a Windows notification when a new issue appears.

It can also generate an HTML report for quickly reviewing the current worklist.

## First-Time Setup

```powershell
cd C:\Users\A25228\Documents\Mantis
Copy-Item .\config.sample.json .\config.json
powershell -ExecutionPolicy Bypass -File .\scripts\Set-MantisCredential.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\Check-MantisNewItems.ps1 -InitializeOnly
```

`Set-MantisCredential.ps1` will ask for your Mantis username and password.  
`-InitializeOnly` marks the current issues as already seen, so existing issues will not trigger notifications.

## Daily Use

Check for new issues:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\Check-MantisNewItems.ps1
```

Generate the HTML report:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\Check-MantisNewItems.ps1 -HtmlReport
```

Report path:

```text
C:\Users\A25228\Documents\Mantis\reports\mantis-items.html
```

Test Windows notification:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\Check-MantisNewItems.ps1 -TestNotification
```

Register a scheduled check every 10 minutes:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\Register-MantisWatcherTask.ps1 -Minutes 10
```

## Shortcut Files

- `MantisReport.cmd`: update the HTML report and open it.
- `MantisReportTask.cmd`: update the HTML report in the background; suitable for Windows Task Scheduler.

## Common Files

- `config.json`: local Mantis URL and tool settings.
- `data\mantis-seen-items.json`: issue IDs that have already been seen.
- `reports\mantis-items.html`: generated HTML report.
- `logs\mantis-watcher.log`: execution log.
- `secrets\mantis-credential.xml`: locally encrypted Mantis credential.

## Reset

If the Mantis filter URL changes, reset the seen issue baseline:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\Check-MantisNewItems.ps1 -InitializeOnly
```

If the credential is wrong, re-enter it:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\Set-MantisCredential.ps1
```

## Note

`secrets\mantis-credential.xml` is encrypted for the current Windows user on the current machine. Do not share it.
