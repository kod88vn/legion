@echo off
rem Keeps the dashboard server on (DB06): starts it again whenever it exits. Run from anywhere.
rem Exits at once when something already listens on 127.0.0.1:8099, so a second copy never runs next to the first.
rem Exit code 3 from the server means the port or its dashboard lock is held by another copy: the loop stops then
rem (no retry every 3 s). Any other exit is a crash: wait 3 s and start again.
cd /d "%~dp0..\.."
:loop
netstat -ano -p tcp | findstr /R /C:"127\.0\.0\.1:8099 .*LISTENING" >nul && (echo dashboard already running on 127.0.0.1:8099 & exit /b 0)
python tools\dashboard\dashserve.py --port 8099
if errorlevel 3 (echo dashboard: another copy holds the port or its lock; keep loop stops & exit /b 0)
timeout /t 3 /nobreak >nul
goto loop
