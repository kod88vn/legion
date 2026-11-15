@echo off
rem Keeps the dashboard server on (DB06): starts it again whenever it exits. Run from anywhere.
cd /d "%~dp0..\.."
:loop
python tools\dashboard\dashserve.py --port 8099
timeout /t 3 /nobreak >nul
goto loop
