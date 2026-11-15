#!/bin/bash
L=/mnt/g/RO/server/openkore-ayla/logs
S=/mnt/g/RO/server/tools/watch
( tail -n0 -F $L/console_player_2.txt | grep -a --line-buffered -E "You have died|processDead|argh|DEAL|UNPAID|TOPUP|no answer|getRoute\] Called|town run|come back to|crowded|Teleporting|escaping|Queueing teleport|in queue|safe_reload|Fly Wing \(|Orange Potion|Buy completed|plan:|change of plan|goto|rally:|fetching|going back|going to .* to get|From:|Disconnect|equipping|Storage Item|\[Party\] [^:]*: .*(help|on me|cant find|where r u|come get me)" | grep -a --line-buffered -vE "Item Appeared|\] #[0-9]+: |Inventory Item Removed" ) &
( tail -n0 -F $L/deals.log | sed -u 's/^/DEALS.LOG /' ) &
while true; do sleep 180; k=$(tail -c 3000000 $L/console_player_2.txt | grep -a "$(date -u -d '-3 min' '+%H:%M')\|$(date -u -d '-2 min' '+%H:%M')\|$(date -u -d '-1 min' '+%H:%M')" | grep -ac "Target Monster.*died"); echo "KILLS last3min=$k"; docker exec -i ro-openkore-ayla python3 - < $S/snap.py 2>&1 | tail -1; done
