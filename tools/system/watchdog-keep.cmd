@echo off
rem Keeps the watchdog (tools/system/watchdog.py) running: starts it again 3 s after any exit. Run from anywhere.
rem Single copy: the watchdog holds legion/data/watchdog.lock (refreshed every pass, stale after 90 s). When a live
rem watchdog already holds it, the first start exits at once (exit 0). A restart after a crash waits 3 s and retries
rem until the stale lock is taken over.
rem A dead watchdog cannot report its own death, so this loop files every exit other than the lock refusal (exit 3)
rem to the bug queue as an outage (key watchdog:self) before it starts the watchdog again.
cd /d "%~dp0..\.."
set first=1
:loop
python tools\system\watchdog.py
set rc=%errorlevel%
if "%rc%"=="3" if "%first%"=="1" (echo another watchdog is already running & exit /b 0)
if not "%rc%"=="3" python tools\system\bugq.py add --from watchdog-keep --priority outage --key watchdog:self --text "OUTAGE: the watchdog stopped (exit code %rc%); the keep loop starts it again in 3 s"
set first=0
timeout /t 3 /nobreak >nul
goto loop
