#!/bin/bash
S=/mnt/g/RO/legion/openkore-ayla/logs/status.json
end=$((SECONDS+${2:-240}))
while [ $SECONDS -lt $end ]; do
  grep -q ",\"map\":\"$1\"" $S && break; sleep 5; done
date -u +%T; cat $S
