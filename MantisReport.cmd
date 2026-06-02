@echo off
setlocal

cd /d "%~dp0"

powershell -NoProfile -ExecutionPolicy Bypass -File ".\scripts\Check-MantisNewItems.ps1" -HtmlReport
if errorlevel 1 (
    echo.
    echo Failed to update Mantis report.
    pause
    exit /b 1
)

start "" "%~dp0reports\mantis-items.html"
exit /b 0
