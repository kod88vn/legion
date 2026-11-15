#!/bin/bash
# ayla-give.sh NAME ZENY - Ayla hands zeny to a partymate through a normal in-game trade (OpenKore "deal").
#   wsl -d Ubuntu-24.04 -- bash /mnt/g/RO/legion/tools/rogui/ayla-give.sh Echobunny 4500
# The friend must be on her screen. She steps next to them (the server only trades within 2 cells), opens the
# trade, adds the zeny and confirms; Legion bots accept trades from party and guild mates and walk up by themselves.
# Prints "traded" or "FAILED" with the last trade lines from her log.
f="$1"; z="${2:-3000}"
[ -z "$f" ] && { echo "usage: ayla-give.sh NAME ZENY"; exit 1; }
C=/mnt/g/RO/legion/openkore-ayla/control/commands.txt
L() { docker logs --since "$1" ro-openkore-ayla 2>&1 | sed 's/\x1b\[[0-9;?]*[a-zA-Z]//g; s/\x1b\]2;[^\x07]*\x07//g'; }
printf "conf attackAuto 1\nconf route_randomWalk 0\n" >> $C; sleep 2
ok=0
for try in 1 2 3 4; do
  printf "pl\n" >> $C; sleep 3
  row=$(L 5s | grep -aE "^[0-9]+ +$f " | tail -1)
  [ -z "$row" ] && { sleep 8; continue; }
  idx=$(echo "$row" | awk '{print $1}'); x=$(echo "$row" | awk '{print $(NF-1)}' | tr -d '(,'); y=$(echo "$row" | awk '{print $NF}' | tr -d ')')
  printf "move $x $y\n" >> $C; sleep 4
  printf "deal $idx\n" >> $C
  for w in 1 2 3 4 5 6; do sleep 2; L 4s | grep -aqiE "Engaged Deal" && break; done
  if L 14s | grep -aqiE "Engaged Deal"; then
    printf "deal add z $z\n" >> $C; sleep 2; printf "deal\n" >> $C; sleep 4; printf "deal\n" >> $C; sleep 4
    L 16s | grep -aiE "deal|finaliz" | grep -av cmdfile | tail -5
    L 16s | grep -aq "Deal Complete" && ok=1
    break
  fi
done
printf "conf attackAuto 2\nconf route_randomWalk 1\n" >> $C
[ $ok = 1 ] && echo "traded $z zeny to $f" || echo "FAILED to trade with $f (row: $row)"
