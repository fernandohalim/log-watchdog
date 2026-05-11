@echo off
REM ============================================================
REM  Install Java Watchdog as a Windows service
REM  RUN THIS AS ADMINISTRATOR (right-click -> Run as administrator)
REM ============================================================

cd /d "%~dp0"

if not exist "watchdog.exe" (
    echo ERROR: watchdog.exe not found in %~dp0
    echo Build it first with: GOOS=windows GOARCH=amd64 go build -o watchdog.exe
    pause
    exit /b 1
)

if not exist "config.json" (
    echo ERROR: config.json not found in %~dp0
    pause
    exit /b 1
)

echo Installing service...
watchdog.exe -install -config "%~dp0config.json"
if errorlevel 1 (
    echo Install FAILED. Are you running as Administrator?
    pause
    exit /b 1
)

echo Starting service...
watchdog.exe -start
if errorlevel 1 (
    echo Start FAILED. Check Event Viewer ^> Windows Logs ^> Application for "JavaWatchdog".
    pause
    exit /b 1
)

echo.
echo Done. Service "JavaWatchdog" is installed and running.
echo View status:  sc query JavaWatchdog
pause