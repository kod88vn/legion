#!/bin/bash
# Seed /openkore/control on first start, then run OpenKore in console mode.
cd /openkore
for f in control.default/*; do
  b=$(basename "$f"); [ -e "control/$b" ] || cp -r "$f" "control/$b"
done
setcfg() { # setcfg key value  -> rewrite (or append) a key in config.txt
  if grep -qE "^$1( |$)" control/config.txt; then
    sed -i -E "s|^$1( .*)?$|$1 $2|" control/config.txt
  else echo "$1 $2" >> control/config.txt; fi
}
if [ ! -e control/.configured ]; then
  setcfg master "rAthena Local"
  setcfg server 0
  setcfg username "${KORE_USER:-bot1}"
  setcfg password "${KORE_PASS:-bot1}"
  setcfg char 0
  setcfg sex 1
  setcfg lockMap prt_fild08
  setcfg attackAuto 2
  setcfg route_randomWalk 1
  setcfg itemsTakeAuto 2
  setcfg itemsGatherAuto 2
  setcfg statsAddAuto 1
  setcfg statsAddAuto_list "10 dex, 10 agi, 10 str, 99 dex"
  setcfg skillsAddAuto 1
  setcfg skillsAddAuto_list "NV_BASIC 9"
  setcfg sitAuto_hp_lower 40
  setcfg sitAuto_hp_upper 100
  setcfg autoTalkCont 1
  setcfg dcOnDeath 0
  setcfg message_length_max 255
  touch control/.configured
fi
exec perl openkore.pl --interface=Console "$@"
