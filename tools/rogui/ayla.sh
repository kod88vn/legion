#!/bin/bash
# ayla.sh - Ayla's "mind" interface to the OpenKore instance that plays her (container ro-openkore-ayla).
# Call from PowerShell:  wsl -d Ubuntu-24.04 -- bash /mnt/g/RO/legion/tools/rogui/ayla.sh <action> [args]
#   log [N]        last N lines of OpenKore's console (default 40): fights, loot, level, chat, errors
#   chat [N]       only chat lines from the last N log lines (default 400): public, party, whispers
#   status         ask OpenKore for status (s) + inventory summary, then show the answer
#   cmd TEXT       run any OpenKore console command, e.g.  cmd p hi guys   |  cmd c hello   |  cmd lockmap prt_fild06
#   say TEXT       public chat (OpenKore "c")         party TEXT  party chat ("p")      whisper NAME TEXT  ("pm")
#   map NAME       hunt on another map (partylead goto NAME + conf lockMap: cancels any rally/hold; the party follows)
#   rally cancel   drop a Kafra rally at once (no new one for 30 min)  |  rally TOWN [MAP]  meet + save at TOWN's Kafra,
#                  then the party goes to MAP (no MAP: they wait at the Kafra for a "map" order; the old lockMap is dropped)
#   lead           what the party-lead plugin is doing (holding / rally / fetching / lockMap)
#   guild-create NAME   found her guild (OpenKore `guild create`; no Emperium needed on this server)
#   recruit [N]    invite up to N (default 3) recruits Legion suggests (GET /api/guild-recruits: her party friends, then
#                  one unguilded player per job the guild lacks) via plugin guildrecruit.pl `ginvite <aid> <name>`
#   recruits       show the suggestions without inviting (guild size, role gaps)
#   guild          Starveil at a glance for the leader (GET /api/human-guild): members, roles, alchemist, week goal and
#                  the ACTIONS list (each with its command) - read it every cycle
#   global TEXT    say it on the server channel #global (rAthena channel = a whisper to "#global"); trade TEXT -> #trade
#                  (#global is for recruiting / LFG / trade only - one line every 20-30 min at most)
#   guildchat TEXT guild chat (OpenKore "g")
#   guild-tax M [L]  EXP tax M% on member positions, L% on the master's (plugin guildrecruit.pl gtax; kept in
#                  control/guildtax.txt and re-applied every 10 min);  guild-tax off stops the re-check
#   guild-skill [auto|off|GD_NAME]  spend guild skill points (auto: Guild Extension first, every 10 min)
#   guild-info     guild level, EXP, members/max, skill points, tax per position
#   give NAME ZENY [WHY]   Ayla mails zeny to a party friend (RODEX, 2% fee; plugin aylagear.pl) and says it in party
#                  chat ("sent Echobunny 22k for the Bastard Sword"); names with spaces work: give "Silent rogue" 15000 hammer
#   deals [N]      last N #trade deals (offers accepted/refused, items taken from mail, payments) from logs/deals.log
#   gear           Ayla's gear plan (aylagear.pl): owned / next piece / where it sells
#   guild-fund NAME AMOUNT [WHY]   guild mates mail zeny to a member (POST /api/human-guild/fund; 5k max each, hourly)
#   tell TEXT     the owner's plain-words order for Ayla (rule AY20), e.g. tell "do a town run with the party": queued in
#                  legion/data/ayla_orders.jsonl; her helper wakes within ~30 s and carries it out ahead of everything
#   orders [N]     open owner orders + the last N results ("[owner] <order> -> <action>", also logs/owner_orders.log)
#   done ID TEXT   (her helper) close owner order ID with what was done
# The plugin (partylead.pl) obeys these orders at once: the mind's explicit order wins over its own decisions.
CMDS=/mnt/g/RO/legion/openkore-ayla/control/commands.txt
HERE=$(dirname "$(readlink -f "$0")")
clean() { sed -e 's/\x1b\][0-9];[^\x07]*\x07//g' -e 's/\x1b\[[0-9;?]*[a-zA-Z]//g' -e 's/\]2;[^]]*OpenKore//g' | grep -v '^\s*$'; }
# one console line per send: a CR/LF inside an argument must never smuggle in a second (free) console command (G05)
send() { local l="$*"; l=${l//$'\r'/ }; l=${l//$'\n'/ }; printf '%s\n' "$l" >> "$CMDS"; }
ORDERS="python3 $HERE/../puppet/orders.py"
a=$1; shift
case "$a" in
  log) docker logs --tail "${1:-40}" ro-openkore-ayla 2>&1 | clean | tail -n "${1:-40}";;
  chat) docker logs --tail "${1:-400}" ro-openkore-ayla 2>&1 | clean | grep -E '^\[(Party|Guild)\]|^\(From:|^\(To:|^[^[(][^:]{1,30} : ' | tail -n 30;;
  status) send s; send i eq; sleep 2.5; docker logs --tail 60 ro-openkore-ayla 2>&1 | clean | sed -n '/Status/,$p' | head -45;;
  cmd) send "$*"; echo "sent: $*";;
  say) send "c $*"; echo ok;;
  party) send "p $*"; echo ok;;
  whisper) to=$1; shift; send "pm \"$to\" $*"; echo ok;;
  tell) [ -n "$*" ] || { echo 'usage: ayla.sh tell "<plain text>"'; exit 1; }; $ORDERS add "$*";;
  orders) $ORDERS list "${1:-10}";;
  done) $ORDERS done "$@";;
  map) [[ "$1" =~ ^[A-Za-z0-9_@.-]+$ ]] || { echo "usage: ayla.sh map MAPNAME"; exit 1; }; send "partylead goto $1"; send "conf lockMap $1"; echo "heading to $1";;
  rally) send "partylead rally $*"; echo "sent: partylead rally $*";;
  guild-create) [ -n "$*" ] || { echo "usage: ayla.sh guild-create NAME"; exit 1; }; send "guild create $*"; sleep 3; docker logs --tail 20 ro-openkore-ayla 2>&1 | clean | grep -i guild | tail -3;;
  guild) curl -s -m 8 "http://127.0.0.1:8090/api/human-guild" | python3 "$HERE/ayla_guild.py" guild;;
  guild-fund) [ -n "$2" ] || { echo "usage: ayla.sh guild-fund NAME AMOUNT [WHY]"; exit 1; }; python3 -c 'import json,sys; print(json.dumps({"to":sys.argv[1],"amount":int(sys.argv[2]),"why":" ".join(sys.argv[3:]) or "gear"}))' "$@" | curl -s -m 8 -H "Content-Type: application/json" -d @- "http://127.0.0.1:8090/api/human-guild/fund"; echo;;
  recruits) curl -s -m 8 "http://127.0.0.1:8090/api/guild-recruits?n=${1:-5}" | python3 "$HERE/ayla_guild.py" recruits;;
  recruit) curl -s -m 8 "http://127.0.0.1:8090/api/guild-recruits?mark=1&n=${1:-3}" | python3 "$HERE/ayla_guild.py" recruit | while IFS= read -r l; do case "$l" in ginvite*) send "${l%%	#*}"; echo "invited: ${l#ginvite }";; *) echo "$l";; esac; done;;
  guild-tax) [ -n "$1" ] || { echo "usage: ayla.sh guild-tax MEMBER% [MASTER%] | off"; exit 1; }; send "gtax $*"; sleep 4; docker logs --tail 40 ro-openkore-ayla 2>&1 | clean | grep "\[guildrecruit\]" | tail -3;;
  guild-skill) send "gskill ${*:-auto}"; sleep 4; docker logs --tail 40 ro-openkore-ayla 2>&1 | clean | grep "\[guildrecruit\]" | tail -3;;
  guild-info) send ginfo; sleep 4; docker logs --tail 60 ro-openkore-ayla 2>&1 | clean | grep "\[guildrecruit\]" | tail -5;;
  global) send "pm \"#global\" $*"; echo "sent to #global: $*";;
  trade) send "pm \"#trade\" $*"; echo "sent to #trade: $*";;
  guildchat) send "g $*"; echo ok;;
  give) [ -n "$2" ] || { echo 'usage: ayla.sh give NAME ZENY [WHY]  (quote names with spaces)'; exit 1; }; to=$1; z=$2; shift 2; send "give \"$to\" $z $*"; sleep 12; docker logs --tail 80 ro-openkore-ayla 2>&1 | clean | grep -aE "\[aylagear\]|rodex mail|Party\] Ayla : sent" | tail -4;;
  deals) tail -n "${1:-15}" /mnt/g/RO/legion/openkore-ayla/logs/deals.log 2>/dev/null || echo "(no deals yet)";;
  gear) send "gear ${*}"; sleep 3; docker logs --tail 60 ro-openkore-ayla 2>&1 | clean | grep -a "\[aylagear\]" | tail -15;;
  lead) send "partylead status"; sleep 1.5; docker logs --tail 30 ro-openkore-ayla 2>&1 | clean | grep "\[partylead\]" | tail -3;;
  *) echo "usage: ayla.sh log|chat|status|cmd|say|party|whisper|map|rally|lead|guild|guild-create|guild-fund|recruit|recruits|global|trade|guildchat|guild-tax|guild-skill|guild-info|give|gear|deals ..."; exit 1;;
esac
