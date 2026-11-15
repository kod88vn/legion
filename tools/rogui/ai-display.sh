#!/bin/bash
# The AI play-tester's private screen: Xvfb :98 (no window, no link to the Windows mouse/keyboard) mirrored
# read-only to the desktop through x11vnc + a view-only VNC viewer window (WSLg). Runs in the foreground.
# VIEW_SCALE enlarges only the viewer image (x11vnc scales the 1280x720 screen; the game is untouched).
# Owner 2026-11-14: "double the UI window" (1.85 = 2368x1332) then "too big, keep 3/4" -> 1.39 = 1780x1000.
# (1.85 is the largest that fits his 1440-px-high screen; 2 = 2560x1440 needs scrolling). Revert: VIEW_SCALE=1.
VIEW_SCALE=${VIEW_SCALE:-1.39}
pgrep -f "Xvfb :98" >/dev/null || { setsid nohup Xvfb :98 -screen 0 1280x720x24 +extension GLX -nolisten tcp >/tmp/xvfb.log 2>&1 & sleep 2; }
while true; do
  pgrep -f "x11vnc -display :98" >/dev/null || { env -u WAYLAND_DISPLAY -u XDG_SESSION_TYPE x11vnc -display :98 -viewonly -shared -forever -nopw -localhost -rfbport 5998 -scale $VIEW_SCALE -o /tmp/x11vnc.log -bg; sleep 2; }
  # viewer window on the Windows desktop (view only); reopens if closed
  DISPLAY=:0 xtigervncviewer -ViewOnly -Shared localhost::5998 >/tmp/vncviewer.log 2>&1
  sleep 15
done
