@echo off
rem Keeps the dashboard server on (DB06): starts it again whenever it exits. Run from anywhere.
rem Exits at once when something already listens on 127.0.0.1:8099, so a second copy never runs next to the first.
cd /d "%~dp0..\.."
:loop
netstat -ano -p tcp | findstr /R /C:"127\.0\.0\.1:8099 .*LISTENING" >nul && (echo dashboard already running on 127.0.0.1:8099 & exit /b 0)
python tools\dashboard\dashserve.py --port 8099
timeout /t 3 /nobreak >nul
goto loop
