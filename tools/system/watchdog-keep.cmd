@echo off
rem Keeps the watchdog (tools/system/watchdog.py) running: starts it again 3 s after any exit. Run from anywhere.
rem Single copy: the watchdog holds legion/data/watchdog.lock (refreshed every pass, stale after 90 s). When a live
rem watchdog already holds it, the first start exits at once (exit 0). A restart after a crash waits 3 s and retries
rem until the stale lock is taken over.
cd /d "%~dp0..\.."
set first=1
:loop
python tools\system\watchdog.py
if errorlevel 3 if "%first%"=="1" (echo another watchdog is already running & exit /b 0)
set first=0
timeout /t 3 /nobreak >nul
goto loop
