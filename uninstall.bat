@echo off
REM ============================================================
REM  Uninstall Java Watchdog
REM  RUN THIS AS ADMINISTRATOR
REM ============================================================

cd /d "%~dp0"

echo Stopping service...
watchdog.exe -stop

echo Uninstalling service...
watchdog.exe -uninstall
if errorlevel 1 (
    echo Uninstall FAILED. Are you running as Administrator?
    pause
    exit /b 1
)

echo.
echo Done. Service removed.
pause