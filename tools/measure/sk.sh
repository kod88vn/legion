#!/bin/bash
# usage: sk.sh SECONDS bot1 bot2 ... ; traces bots, counts skill sends / fails / states
q() { python -c "import urllib.parse,sys;print(urllib.parse.quote(sys.argv[1]))" "$1"; }
S=$1; shift
for b in "$@"; do curl -s "127.0.0.1:8090/api/trace?bot=$(q "$b")&on=1" >/dev/null; done
sleep $S
for b in "$@"; do
  L=$(wsl -e bash -lc "docker logs --since ${S}s ro-legion 2>&1 | grep -F 'TRACE $b '")
  sk=$(echo "$L" | grep -oE 'SEND (ground )?skill [0-9]+' | awk '{print $NF}' | sort | uniq -c | tr '\n' ' ')
  fl=$(echo "$L" | grep -o 'skill fail skill=[0-9]* result=[0-9]*' | sort | uniq -c | tr '\n' ' ')
  st=$(echo "$L" | grep -o 'state=[a-zA-Z ]*' | cut -c1-28 | sort | uniq -c | sort -rn | head -3 | tr '\n' ' ')
  echo "$b | skills: $sk | fails: $fl | $st"
  curl -s "127.0.0.1:8090/api/trace?bot=$(q "$b")&on=0" >/dev/null
done
