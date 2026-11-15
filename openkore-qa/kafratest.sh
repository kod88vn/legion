#!/bin/bash
# kafratest.sh: warp next to every teleporting Kafra and use the destination named by portals.txt (first and last
# index); print expected vs. actual landing map.
Q=/g/RO/legion/openkore-qa/qa.sh
P=/g/RO/legion/openkore-qa/tables/portals.txt
sed -n '/^# Kafra teleports/,/^# end Kafra/p' $P | grep -v '^#' | awk '{k=$1" "$2" "$3; n[k]++; if(n[k]==1) first[k]=$0; last[k]=$0} END{for(k in first){print first[k]; if(last[k]!=first[k]) print last[k]}}' | sort |
while read src x y dst dx dy cost t seq; do
  $Q -w 2 "c @warp $src $x $((y-2))" > /dev/null
  out=$($Q -w 7 "talknpc $x $y $seq")
  got=$(echo "$out" | grep -a "Map Change" | head -1 | sed 's/.*Map Change: \([a-z_0-9]*\)\.gat (\([0-9]*\), \([0-9]*\)).*/\1 \2 \3/')
  err=$(echo "$out" | grep -a "Could not find\|not enough zeny\|steps to be done" | head -1 | cut -c27-)
  st=FAIL; [ "${got%% *}" = "$dst" ] && st=ok
  echo "$st  $src $x,$y [$seq] -> want $dst $dx,$dy got ${got:-none} $err"
done
