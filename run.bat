@echo off
REM ============================================================
REM  Run Java Watchdog in foreground (no admin required).
REM  Launches in a minimized cmd window, same pattern as your
REM  other Java services.
REM
REM  Double-click this file, or use it as a Task Scheduler action.
REM ============================================================

cd /d "%~dp0"

REM Start ourselves minimized so the cmd window doesn't get in the way.
REM The "" is the (empty) title argument required by `start`.
if not defined WATCHDOG_RELAUNCH (
    set WATCHDOG_RELAUNCH=1
    start "JavaWatchdog" /MIN cmd /c "%~f0"
    exit /b 0
)

title JavaWatchdog
watchdog.exe -foreground -config "%~dp0config.json"