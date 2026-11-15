@echo off
rem Keeps the dashboard server on (DB06): starts it again whenever it exits. Run from anywhere.
rem Exits at once when something already listens on 127.0.0.1:8099, so a second copy never runs next to the first.
rem Exit code 3 from the server (the port or its dashboard lock is held) stops this loop only when 127.0.0.1:8099 is
rem really LISTENING, i.e. another copy is serving. Otherwise the lock is stale (a hard kill) or the bind failed briefly:
rem keep retries every 3 s until it heals, so the always-on dashboard comes back on its own.
cd /d "%~dp0..\.."
:loop
netstat -ano -p tcp | findstr /R /C:"127\.0\.0\.1:8099 .*LISTENING" >nul && (echo dashboard already running on 127.0.0.1:8099 & exit /b 0)
python tools\dashboard\dashserve.py --port 8099
if errorlevel 3 (
  netstat -ano -p tcp | findstr /R /C:"127\.0\.0\.1:8099 .*LISTENING" >nul && (echo another dashboard serves 127.0.0.1:8099; exit /b 0)
)
timeout /t 3 /nobreak >nul
goto loop
