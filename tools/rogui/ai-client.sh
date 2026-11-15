#!/bin/bash
# Watch Ayla directly: a private Wine copy of the client (~/roclient) on the virtual display :98 (mirrored to the
# desktop view-only by ai-display.sh) logs in THROUGH OpenKore's X-Kore 2 proxy, so it shows exactly what OpenKore
# (which plays Ayla) sees and does. No GM account involved.
#
#   client -> 127.0.0.1:6901 (docker publish) -> OpenKore X-Kore 2 account server (172.18.0.3:6901)
#          -> char server 172.18.0.3:6902 -> map server 172.18.0.3:6903   (OpenKore advertises XKore_publicIp)
#   OpenKore itself stays logged in to rAthena as usual (account player, char slot 2).
#   The exe's login address is patched to 127.0.0.1:6901.
#
# Lessons (2026-11-13):
#  - OpenKore's X-Kore 2 proxy didn't speak the 2021 client's login/char packets. Fixed by the plugin
#    openkore-ayla/plugins/cmdfile/xkore2fix.pl (0AC4 server list with web token + non-zero session IDs,
#    099D on every 09A1, 0AC5 map info, connections closed by the client not the proxy).
#  - Do NOT set the large-address-aware flag on the exe (dies right after server select under Wine).
#  - 32-bit llvmpipe address space: MESA_EXTENSION_OVERRIDE=-GL_ARB_buffer_storage plus
#    HKCU\Software\Wine\Direct3D csmt=0, VideoMemorySize=256 in ~/.wine-ro.
#  - 2026-11-14: crashes every ~30 min were "LLVM ERROR: Unable to allocate section memory!" (llvmpipe JIT code
#    exhausting the 32-bit address space; GL_OUT_OF_MEMORY / Wine lock timeout follow). softpipe (no JIT) was tried: black screen / login fails, so the default stays llvmpipe
#    (ROAI_GALLIUM=softpipe to retry). Open issue.
#  - 3 failed logins in a row stop the loop (investigate instead of retrying); see /tmp/roai-restarts.log.
# ROAI_WOW64=1 (2026-11-14 fix for the ~30 min "LLVM ERROR: Unable to allocate section memory" crash): run the
# client under WineHQ wine-stable 11.0 in new-WoW64 mode (~/wine11-wow64 = the official .debs extracted with dpkg -x,
# lib/wine/i386-unix removed, so 32-bit apps run in a 64-bit host process and Mesa/llvmpipe's JIT lives above 4 GB),
# with its own prefix copy ~/.wine-ro-wow64 (already upgraded). Default 0 = system Wine 9.0 + ~/.wine-ro as before.
if [ "${ROAI_WOW64:-0}" = 1 ]; then
  WINE=$HOME/wine11-wow64/opt/wine-stable/bin/wine; WINESERVER=$HOME/wine11-wow64/opt/wine-stable/bin/wineserver
  export WINEPREFIX=$HOME/.wine-ro-wow64 WINEDLLOVERRIDES="mscoree=;mshtml="
else
  WINE=wine; WINESERVER=wineserver; export WINEPREFIX=$HOME/.wine-ro
fi
export DISPLAY=:98 WINEDEBUG=fixme-all LP_NUM_THREADS=1 LP_NATIVE_VECTOR_WIDTH=128 \
       MESA_SHADER_CACHE_DISABLE=1 MESA_EXTENSION_OVERRIDE=-GL_ARB_buffer_storage GALLIUM_DRIVER=${ROAI_GALLIUM:-llvmpipe}
LOG=/tmp/roai-restarts.log
log() { echo "$(date '+%F %T') $*" >>$LOG; }
win() { xdotool search --name ROAI 2>/dev/null | head -1; }
focus() { xdotool windowfocus --sync "$(win)" 2>/dev/null; }
click() { focus; xdotool mousemove $1 $2 sleep 0.1 mousedown 1 sleep 0.15 mouseup 1; sleep 0.6; }
in_game() { ss -tn 2>/dev/null | grep -q "172.18.0.3:6903"; }   # live connection to OpenKore's map server
# Ayla really in game right now: the server's online flag plus a fresh OpenKore status line (the log alone is stale
# right after an OpenKore restart, which wasted a login attempt)
ayla_online() {
  [ "$(docker exec ro-db mariadb -N -uragnarok -pragnarok ragnarok -e 'SELECT online FROM `char` WHERE char_id=150010' 2>/dev/null)" = "1" ] &&
    docker logs --since 20s ro-openkore-ayla 2>&1 | grep -qa "Ayla:  B"
}

# Login timing scales with machine load (2026-11-14: with load ~25 on 24 cores the client took >40 s to draw its
# first screen, every fixed-sleep click missed, 3 failed logins). Wait for the real first screen, then use
# slower steps (S=sleep factor, ROAI_SLOW, default 2).
S=${ROAI_SLOW:-2}
w() { sleep $(echo "$1*$S" | bc); }
first_screen() {   # until the RO window shows something other than a flat colour (max 180 s)
  for i in $(seq 90); do
    n=$(import -window root -resize 160x90 txt:- 2>/dev/null | tail -n +2 | awk '{print $3}' | sort -u | wc -l)
    [ "${n:-0}" -gt 200 ] && return 0; sleep 2
  done; return 1
}
login() {
  first_screen; w 5
  click 714 563; w 4                                           # "Do you agree?" OK
  focus; xdotool key Return; w 4                               # server select
  click 597 509; focus; xdotool key ctrl+a; for i in $(seq 10); do xdotool key BackSpace; done
  xdotool type --delay 60 player
  click 597 531; focus; xdotool type --delay 60 player; xdotool key Return; w 7
  focus; xdotool key Return; w 8                               # service list
  click 545 170; w 1.5; click 1040 530; w 20                   # Ayla (slot 3) -> Game Start
  click 456 554; click 884 551; click 1256 395                 # tip box: untick "open on start", Exit; close inventory
}

until pgrep -f "Xvfb :98" >/dev/null; do sleep 1; done
cd $HOME/roclient
fails=0
while true; do
  until ayla_online; do sleep 10; done                         # the proxy only serves while OpenKore is in game
  $WINESERVER -k 2>/dev/null; sleep 1
  $WINE explorer /desktop=ROAI,1280x720 RO-AILab.exe >/tmp/roai.log 2>&1 &
  login
  if pgrep -f RO-AILab.exe >/dev/null && in_game; then
    fails=0; log "watch client in game as Ayla (through OpenKore)"
  else
    fails=$((fails+1)); log "watch client login failed ($fails/3); client errors: $(grep -m3 ':err:' /tmp/roai.log | tr '\n' ' ')"
    pkill -f RO-AILab.exe
    [ $fails -ge 3 ] && { log "stopping: 3 failed logins in a row - investigate before restarting"; exit 1; }
    sleep 30; continue
  fi
  # watch the client itself, not just the process (lesson 2026-11-13: it froze for 10 min on a Wine lock while the
  # process and the connection stayed up, and it showed error boxes nobody dismissed):
  #   - a Wine "wait timed out ... retrying" deadlock line in the log       -> restart
  #   - an Error / Gravity Error Handler window                             -> restart
  #   - the screen not changing at all for ~60 s (identical screenshots)    -> restart
  same=0; prev=""; why=""
  while pgrep -f RO-AILab.exe >/dev/null && in_game; do
    sleep 10
    if grep -q "RtlpWaitForCriticalSection.*timed out" /tmp/roai.log; then why="frozen (Wine lock timeout)"; break; fi
    if xdotool search --name "^Error$|Gravity" >/dev/null 2>&1; then why="error dialog"; break; fi
    cur=$(import -window root -resize 320x180 gray:- 2>/dev/null | md5sum | cut -c1-12)
    if [ -n "$cur" ] && [ "$cur" = "$prev" ]; then same=$((same+1)); else same=0; fi
    prev=$cur
    if [ $same -ge 5 ]; then why="frozen (screen unchanged for 60 s)"; break; fi
  done
  [ -n "$why" ] && cp /tmp/roai.log /tmp/roai-crash-$(date +%H%M%S).log 2>/dev/null
  log "watch client dropped: ${why:-OpenKore restarted or client exited} $(grep -m2 -E 'OUT_OF_MEMORY|:err:d3d' /tmp/roai.log | tr '\n' ' ')"
  echo "$(date '+%F %T') ai-client: watch client restarted: ${why:-connection dropped}" >> /mnt/g/RO/server/legion/data/watchdog.log
  pkill -f RO-AILab.exe; sleep 5
done
