#!/bin/bash
# One-line snapshot of Ayla's party every 5 min for the main session to observe (read-only).
# kpm and exp/h: statusout.pl's 10-min figures in status.json (kills from her exp gains); rank: the server leaderboard
# (tools/system/leaderboard.py --brief: ro-db, read-only SELECT).
cd /g/RO/legion
while true; do
  python - <<'P'
import json,time,os,subprocess,re
try:
  s=json.load(open('openkore-ayla/logs/status.json')); age=int(time.time()-os.path.getmtime('openkore-ayla/logs/status.json'))
except Exception as e: s={}; age=-1
log=subprocess.run(['wsl','-d','Ubuntu-24.04','--','bash','-c',"docker logs --since 5m ro-openkore-ayla 2>&1 | sed 's/\x1b\[[0-9;]*[mK]//g'"],capture_output=True,text=True,errors='replace').stdout
exp=sum(int(m) for m in re.findall(r'You have gained (\d+)/',log))
kills=len(re.findall(r'Target Monster .* died',log)); deaths=log.count('You have died')
wd=[l.split('[watchdog]')[1][:70] for l in log.splitlines() if '[watchdog]' in l][-2:]
maps=len(re.findall(r'Map Change',log))
try:
  d=json.load(urlopen:=__import__('urllib.request').request.urlopen('http://127.0.0.1:8090/api/parties/detail',timeout=8))
  p=[x for x in d if x.get('human')][0]; mem=' '.join(f"{m.get('name','?')[:6]}:{m.get('dist_lead','?')}" for m in p['members'] if not m.get('human'))
except Exception as e: mem='api?'
try:
  rank=subprocess.run(['python','tools/system/leaderboard.py','--brief'],capture_output=True,text=True,timeout=90).stdout.strip() or 'rank ?'
except Exception as e: rank='rank ?'
print(f"{time.strftime('%H:%M')} Ayla {s.get('map')} {s.get('x')},{s.get('y')} lock={s.get('lock_map')} | kpm {s.get('kpm','?')} exp/h {s.get('exp_h','?')} | {rank} | 5m: exp {exp} kills {kills} deaths {deaths} mapchg {maps} | {str(s.get('intent'))[:60]} | dist {mem} | wd {wd} | status {age}s",flush=True)
P
  sleep 300
done
