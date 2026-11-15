#!/bin/bash
L=/mnt/g/RO/server/openkore-ayla/logs/console_player_2.txt
start=$(stat -c %s $L); end=$((SECONDS+${1:-280}))
while [ $SECONDS -lt $end ]; do
  sleep 5
  hit=$(tail -c +$start $L | grep -a -iE "\[Party\] [^A][^:]*: .*(help|on me|cant find|where r u|come get me|com eget)" | head -1)
  [ -n "$hit" ] && { echo "$hit"; break; }
done
date -u +%T
