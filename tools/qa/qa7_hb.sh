# heartbeat + container status; arg: message
cd /g/RO/legion && /c/Users/kod/AppData/Local/Programs/Python/Python312/python tools/legionctl.py think qa "$1" 2>&1 | tail -1
MSYS_NO_PATHCONV=1 wsl -d Ubuntu-24.04 -- docker ps --format '{{.Names}} {{.Status}}' 2>&1 | grep -E 'ro-legion|rathena'
date +%H:%M
