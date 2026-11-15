#!/bin/bash
# one QA cycle: uptime, class sample (flags only, known ones filtered), quick steady line
P=/c/Users/kod/AppData/Local/Programs/Python/Python312/python
cd /c/Users/kod/AppData/Local/Temp/claude/9b0368b215c2c0f3/40046184-90b0-47ad-be6b-88bc200a3437/scratchpad
date +%H:%M
curl -s -m 8 http://127.0.0.1:8090/api/overview | $P -c "import json,sys;o=json.load(sys.stdin);print('uptime',o['runtime']['uptime_s'],'connected',o['runtime']['connected'],'deaths/min m5',o['activity']['m5']['per_min']['deaths'],'kills/min',o['activity']['m5']['per_min']['kills'])" || echo "API DOWN"
$P qa2_cycle.py 24 2>&1 | grep -v "^ok" | grep -v "AYLA PT\|PARTIES with\|pt: \[" | grep -v "FLAGS: NO-ARROWS[^|]*$\|FLAGS: SP-FULL[^|]*$" > qa2_last.txt
# print only blocks that still have FLAGS lines after filtering
awk '/^## /{blk=$0; next} /^   /{blk=blk"\n"$0; if ($0 ~ /FLAGS:/) print blk; next} {print}' qa2_last.txt | head -60
