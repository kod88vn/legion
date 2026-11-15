@echo off
rem tell-ayla.cmd - give Ayla a plain-words order (rule AY20). Her helper carries it out within about 30 s.
rem   tell-ayla "do a town run with the party"      tell-ayla go farm gl_church
rem Result: tools\rogui\ayla.sh orders  (or openkore-ayla\logs\owner_orders.log)
if "%~1"=="" (
  echo usage: tell-ayla "plain words for Ayla"
  exit /b 1
)
wsl -d Ubuntu-24.04 --cd G:/RO/legion -- bash tools/rogui/ayla.sh tell %*
