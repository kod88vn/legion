#!/bin/bash
# qa.sh [-w SECONDS] "cmd" ["cmd" ...]: send OpenKore commands to the QA char (Trailmark) and print the new log lines.
W=6; if [ "$1" = "-w" ]; then W=$2; shift 2; fi
D=/g/RO/legion/openkore-qa
[ -d $D ] || D=/mnt/g/RO/legion/openkore-qa
L=$D/logs/console_qatest_0.txt
n=$(wc -l < $L)
for c in "$@"; do
  # cmdfile reads, then truncates the file: append only when it is empty and retry if the line never reached the log
  for t in 1 2 3; do
    while [ -s $D/control/commands.txt ]; do sleep 0.2; done
    printf '%s\n' "$c" >> $D/control/commands.txt
    sleep 2.5
    tail -n 300 $L | grep -aqF "[cmdfile] > $c" && break
  done
done
sleep $W
tail -n +$((n+1)) $L | grep -av "\[dist=unknown\] \[\(Global\|Trade\)\]\|NPC Exists\|Player .* is casting\|uses .* on\|Avoiding out of sight\|Processing map"
