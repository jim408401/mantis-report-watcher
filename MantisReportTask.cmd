@echo off
setlocal

cd /d "%~dp0"

if not exist "%~dp0logs" mkdir "%~dp0logs"

echo [%date% %time%] Start updating Mantis report. >> "%~dp0logs\mantis-report-task.log"

powershell -NoProfile -ExecutionPolicy Bypass -File ".\scripts\Check-MantisNewItems.ps1" -HtmlReport >> "%~dp0logs\mantis-report-task.log" 2>&1
if errorlevel 1 (
    echo [%date% %time%] Failed to update Mantis report. >> "%~dp0logs\mantis-report-task.log"
    exit /b 1
)

echo [%date% %time%] Mantis report updated successfully. >> "%~dp0logs\mantis-report-task.log"
exit /b 0
