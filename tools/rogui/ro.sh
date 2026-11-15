#!/bin/bash
# ro.sh - drive the AI play-tester's private Ragnarok client (Wine on Xvfb :98). Never touches the Windows desktop.
# Call from PowerShell:  wsl -d Ubuntu-24.04 -- bash /mnt/g/RO/tools/rogui/ro.sh <action> [args]
#   shot NAME | click X Y | rclick X Y | dclick X Y | hold X Y SECONDS | drag X1 Y1 X2 Y2
#   key ENTER|ESC|TAB|SPACE|BACK|UP|DOWN|LEFT|RIGHT|INSERT|F1..F12|ALT+x|CTRL+x | type TEXT | say TEXT | wait S
#   hunt X Y [SECONDS] | walk DIR [SECONDS] | loot
#   status   (is the client up?)   login (log in as Ayla from the agreement screen)
export DISPLAY=:98
SHOTS=/mnt/g/RO/tools/rogui/shots
mkdir -p $SHOTS
focus() {
  w=$(xdotool search --name "ROAI" 2>/dev/null | head -1)
  [ -n "$w" ] && xdotool windowfocus --sync "$w" 2>/dev/null
}
keyname() {
  case "$1" in
    ENTER) echo Return;; ESC) echo Escape;; TAB) echo Tab;; SPACE) echo space;; BACK) echo BackSpace;;
    UP) echo Up;; DOWN) echo Down;; LEFT) echo Left;; RIGHT) echo Right;; INSERT) echo Insert;;
    F[0-9]|F1[0-2]) echo "$1";;
    ALT+*) echo "alt+$(echo ${1#ALT+} | tr A-Z a-z)";;
    CTRL+*) echo "ctrl+$(echo ${1#CTRL+} | tr A-Z a-z)";;
    *) echo "$1";;
  esac
}
a=$1; shift
case "$a" in
  login) # agreement -> server -> password -> char server -> Ayla -> Game Start (used after a client restart)
    focus; xdotool mousemove 714 563 sleep 0.1 mousedown 1 sleep 0.15 mouseup 1; sleep 4
    focus; xdotool key Return; sleep 3
    focus; xdotool mousemove 597 531 sleep 0.1 mousedown 1 sleep 0.15 mouseup 1; xdotool type --delay 45 player; xdotool key Return; sleep 4
    focus; xdotool key Return; sleep 6
    focus; xdotool mousemove 545 170 sleep 0.1 mousedown 1 sleep 0.15 mouseup 1; sleep 1
    xdotool mousemove 1040 530 sleep 0.1 mousedown 1 sleep 0.15 mouseup 1; sleep 10
    echo "login sequence sent";;
  status) pgrep -f "RO-AILab.exe" >/dev/null && echo "client running" || echo "client NOT running (it restarts itself within ~20 s)";;
  shot) n=${1:-now}; import -window root "$SHOTS/$n.png" && echo "G:\\RO\\tools\\rogui\\shots\\$n.png (1280x720)";;
  click) focus; xdotool mousemove "$1" "$2" sleep 0.12 mousedown 1 sleep 0.15 mouseup 1; echo ok;;
  rclick) focus; xdotool mousemove "$1" "$2" sleep 0.12 mousedown 3 sleep 0.15 mouseup 3; echo ok;;
  dclick) focus; xdotool mousemove "$1" "$2" sleep 0.12 mousedown 1 sleep 0.1 mouseup 1 sleep 0.08 mousedown 1 sleep 0.1 mouseup 1; echo ok;;
  hold) focus; xdotool mousemove "$1" "$2" sleep 0.1 mousedown 1 sleep "$3" mouseup 1; echo ok;;
  drag) focus; xdotool mousemove "$1" "$2" sleep 0.1 mousedown 1 sleep 0.15 mousemove "$3" "$4" sleep 0.15 mouseup 1; echo ok;;
  key) focus; xdotool key --delay 60 "$(keyname "$1")"; echo ok;;
  type) focus; xdotool type --delay 45 -- "$*"; echo ok;;
  say|cmd) focus; xdotool key Return sleep 0.2; xdotool type --delay 45 -- "$*"; xdotool key Return; echo ok;;
  wait) sleep "$1"; echo ok;;
  hunt) # attack the monster at X Y and keep at it for SECONDS (re-clicks it so the attack continues), default 6
    focus; s=${3:-6}; end=$((SECONDS+s))
    while [ $SECONDS -lt $end ]; do xdotool mousemove "$1" "$2" sleep 0.05 mousedown 1 sleep 0.15 mouseup 1; sleep 1.2; done
    echo ok;;
  walk) # walk toward a screen direction for SECONDS: n s e w ne nw se sw (holds the mouse near the screen edge)
    focus; cx=640; cy=375
    case "$1" in n) x=$cx;y=120;; s) x=$cx;y=640;; e) x=1180;y=$cy;; w) x=100;y=$cy;; ne) x=1100;y=150;; nw) x=180;y=150;;
                 se) x=1100;y=620;; sw) x=180;y=620;; *) echo "dir?"; exit 1;; esac
    xdotool mousemove $x $y sleep 0.05 mousedown 1 sleep "${2:-3}" mouseup 1; echo ok;;
  loot) # click around your feet to pick up drops (call after a kill)
    focus; for p in "640 375" "610 360" "670 360" "610 395" "670 395" "640 345" "640 405"; do set -- $p; xdotool mousemove $1 $2 sleep 0.05 mousedown 1 sleep 0.12 mouseup 1; sleep 0.35; done; echo ok;;
  *) echo "unknown action $a"; exit 1;;
esac
