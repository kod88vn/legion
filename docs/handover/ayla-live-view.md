> **Reference only: OpenKore era; current state is in HANDOVER.md and STATUS.md.**

<!-- Moved unchanged from HANDOVER.md lines 323-434 (as of commit 63ca923). Index: HANDOVER.md. -->

## 4. Ayla and the live view (the fiddly part)

```
 OpenKore (ro-openkore-ayla, X-Kore 2) ----logged in to----> rAthena (account player, char slot 2 = Ayla)
      ^  proxy: account 172.18.0.3:6901, char :6902, map :6903  (XKore_publicIp = rathena container IP; binds AND advertises it)
      |
 Wine client ~/roclient/RO-AILab.exe (login address patched to 127.0.0.1:6901 -> docker publish -> proxy)
      on Xvfb :98 1280x720  --x11vnc (view-only, :5998, -scale 1.39)-->  xtigervncviewer window on the owner's desktop
```
- **Ayla's Haiku mind picks where the party grinds** (owner order 2026-11-13: "let AI make the right grinding
  decision", Prontera/pay_fild01 too slow): it checks party levels (`/api/parties/detail`), `legionctl spots <lv>`,
  picks the best map that is safe for the lowest member, moves with `ayla.sh map NAME`, explains via
  `legionctl think ayla ... --kind decision`, re-evaluates every ~20 min.
  **It looks up game data before choosing** (owner rule 2026-11-14, RULES.md 5): `legionctl drops "<item>" --for Ayla`
  (who drops it at what rate, maps ranked by drop rate x spawn count, graded vs the party's levels, danger),
  `legionctl mob "<monster>"`, `legionctl mapinfo <map> --for Ayla`. It never says it can't find a drop table.
- **Game-data lookup for all minds** (`legion/lookup.go`, 2026-11-14): `GET /api/lookup/item|monster|map?q=<name|id>
  [&lv=69,70,72]` answers from the rAthena tables legion already loads (mob_db Drops/MvpDrops, item_db, npc/re/mobs
  spawns; dungeon/field from the spawn script folder). Fuzzy names (case, `_`, word prefixes "blue pot", initials
  "SS card", small typos); items/monsters that really spawn rank first; `matches` lists the other candidates.
  Unit test `TestLookupMatcher`; `LEGION_RATHENA=/rdata go test -run TestLookupLive` checks real data and, with
  `LEGION_LOOKUP_SERVE=0.0.0.0:18090`, serves the endpoints for a legionctl dry run without a full legion.
- **OpenKore plays Ayla** (fights, loots, levels; `attackAuto 2`, `lockMap` = the map the mind chose). The watch client just shows
  what OpenKore sees and does. The owner's own Windows client is NOT involved.
- **`openkore-ayla/plugins/cmdfile/xkore2fix.pl`** (loaded via `loadPlugins_list` in `control/sys.txt`) is required:
  OpenKore's X-Kore 2 proxy was written for older clients. Found by recording the real server's traffic to this client
  (`tools/rogui/realcap.py`) vs the proxy's (`tools/rogui/pktlog.py`). It makes the proxy send `0AC4` (server list
  with a 16-char web token; was `0069`), non-zero session IDs, a `099D` page for every `09A1` (OpenKore alternated
  with a PIN request), `0AC5` map info on char select (was `0071`), and lets the client close login/char connections
  (the proxy closing first -> "Disconnected from Server"). Base::Ragnarok::* modules compile only when X-Kore 2
  starts, so those overrides are applied lazily in `mainLoop_pre`.
- The `openkore-ayla` entrypoint in docker-compose.yml writes the rathena container IP into `XKore_publicIp` at every start (a stale IP made OpenKore quit with "Cannot assign requested address" / "Bye!" and restart-loop, 2026-11-14). `tools/rogui/ayla-ip.sh` resolves that same IP at run time (`docker inspect ro-rathena`, not `ro-openkore-ayla`: it shares rathena's network namespace and has no IP of its own; fallbacks: `XKore_publicIp` in `control/config.txt`, then 172.18.0.3); `ai-client.sh` (`in_game`), `pktlog.py` and `realcap.py` use it, nothing hardcodes the IP any more. Start order for the owner's view: `ai-display.sh` (Xvfb :98 + view-only viewer window), then `ai-client.sh` (waits for Ayla to be online; `keeper.sh` is only Ayla's online watchdog and is independent).
- **Scripts** (`G:\RO\legion\tools\rogui\`, run in WSL as long-running commands):
  - `ai-display.sh`: Xvfb :98 1280x720 + x11vnc + TigerVNC viewer window. Restart all three if the view freezes.
    **View size** (owner 2026-11-14 "double the UI window", then "too big now, keep 3/4"): the game and Xvfb stay at
    1280x720; x11vnc serves the screen scaled by `VIEW_SCALE` (default 1.39 -> 1780x1000 viewer window; 1.85 =
    2368x1332 was too big; the owner's screen is 1440 physical px high, exact 2x = 2560x1440 would need scrolling).
    Restart trick: kill with bracketed patterns (`pkill -f "[x]11vnc -display :98"`) so pkill doesn't hit its own shell. Normal window, not fullscreen. Changing it needs only a
    restart of `ai-display.sh` (kill the script, x11vnc and xtigervncviewer, NOT Xvfb), never of the client.
    Revert: `VIEW_SCALE=1` (edit the default or start it as `VIEW_SCALE=1 bash ai-display.sh`). From Git Bash start it
    with `MSYS_NO_PATHCONV=1 wsl.exe -d Ubuntu-24.04 -- bash /mnt/g/RO/legion/tools/rogui/ai-display.sh` (else the path is mangled).
  - `ai-client.sh`: waits until Ayla is online, starts the Wine client, logs in through the proxy (agree -> Enter ->
    player/player -> Enter -> Ayla slot (545,170) -> Game Start (1040,530) -> closes tip box), re-logs after drops,
    stops after 3 failed logins in a row (log `/tmp/roai-restarts.log`, client log `/tmp/roai.log`).
  - `keeper.sh`: checks the DB `online` flag of Ayla (char 150010) every 20 s; offline ~5 min -> restarts
    `ro-openkore-ayla` once, then only logs (`legion/data/watchdog.log`). It never touches the client.
  - `ayla.sh`: the Ayla mind's interface: `log [N]`, `chat`, `status`, `cmd TEXT`, `party TEXT`, `say TEXT`,
    `whisper NAME TEXT`, `map NAME` (writes OpenKore commands to `control/commands.txt`).
  - `ro.sh`: xdotool actions on :98 (shot/click/key/type...). `PLAYER.md` is the older brief from when Haiku played
    Ayla by clicking the client; OpenKore plays her now, so `ro.sh` is only for client housekeeping.
- **Wine client facts**: private copy `~/roclient` (WSL home), prefix `~/.wine-ro`, `DATA.INI` without HD sprites,
  exe byte-identical to `G:\RO\kRO-210607\client\RO-AILab.exe` except the login address string (4 occurrences of
  `127.0.0.1:6900` -> `127.0.0.1:6901`).
  - Do NOT set the large-address-aware flag (that copy dies right after server select under Wine).
  - 32-bit Mesa llvmpipe ran out of address space (`GL_OUT_OF_MEMORY in glBufferStorage` -> access violation). Fixed by
    env `MESA_EXTENSION_OVERRIDE=-GL_ARB_buffer_storage LP_NUM_THREADS=1 LP_NATIVE_VECTOR_WIDTH=128` and registry
    `HKCU\Software\Wine\Direct3D` `csmt=0`, `VideoMemorySize=256`.
- **Ayla leads her party** ("a true party leader, protect and guide its party instead of just running ahead"):
  - Plugin `openkore-ayla/plugins/cmdfile/partylead.pl` (in `loadPlugins_list`): while any online partymate is
    on another map or more than 9 cells away, Ayla stops random walking and lockMap routing and waits.
  - If a partymate is more than 16 cells away on the same map, she walks back to them.
  - She still fights anything that attacks her or a partymate (`attackAuto_party 1`). The log shows
    "[partylead] holding for the party: ...".
- **Watch-client crash on unknown NPC sprites**: some NPC IDs have no sprite name in this client. It then tries to
  load `sprite\npc\.spr`, shows "Resource File Loading fail" and crashes. Fixed with `~/roclient/npcfix.grf` (a
  fallback sprite at exactly that path, first in DATA.INI); the same files are also loose in `~/roclient/data/sprite/npc/`.
- **Watch client crashed every ~30 min (OPEN, 2026-11-14)**: the crash logs (`/tmp/roai-crash-*.log`) all showed
  `LLVM ERROR: Unable to allocate section memory!`, then `GL_OUT_OF_MEMORY in Resizing framebuffer` and the Wine
  lock timeout. llvmpipe's LLVM JIT keeps compiling shader variants inside the 32-bit client's 4 GB address space
  (RSS was only ~0.5 GB and flat, so this was not a RAM leak) until a JIT allocation failed. softpipe (no JIT) was tried
  via `ROAI_GALLIUM=softpipe`: the screen stays black and the scripted login fails, so the default stays llvmpipe.
  Next ideas: a 64-bit host GL path (new-WoW64 Wine), or a newer Mesa. Window size and
  VIEW_SCALE are unchanged. A changed export only takes effect when the ai-client.sh loop is restarted.
  **Fix found, waiting for main to switch (2026-11-14 16:05): new-WoW64 Wine.** Measured with `/proc/PID/maps`
  (address space below vs above 4 GB): the live classic client (system Wine 9.0, 32-bit Mesa 25.2.8 / LLVM 20 inside
  the 32-bit process) starts in game at 3,780 MB of its 4,096 MB and grew to 4,047 MB in 20 min (crash zone); RSS is
  only ~0.4 GB. Under WineHQ **wine-stable 11.0 in new-WoW64 mode** the same exe runs in a 64-bit host process: Mesa,
  libLLVM and the JIT live above 4 GB (libLLVM mapped at 0x7236...), the 32-bit window holds only the Windows side and
  stayed flat at 1,778 MB for 28 min on a test display (:97, separate prefix, exe copy renamed ROTEST.exe; login
  screen only - no safe test account, so in-game rendering under WoW64 is verified on the first real switch). Agree and
  login screens render normally, first screen in 20 s. Mesa was already new (25.2.8), so a newer Mesa alone would not
  help; LP_* knobs only slow the growth.
  - Install (done, no system packages touched): `~/wine11-wow64` = `wine-stable`, `wine-stable-amd64`,
    `wine-stable-i386` 11.0.0.0~noble-1 from dl.winehq.org extracted with `dpkg -x`, then `lib/wine/i386-unix`
    deleted (without the 32-bit unix side, Wine 11 runs 32-bit exes through wow64). Debs kept in `~/winetest/deb`.
    Prefix `~/.wine-ro-wow64` = copy of `~/.wine-ro`, upgraded once with `wineboot -u` (csmt=0 and
    VideoMemorySize=256 kept). `WINEDLLOVERRIDES="mscoree=;mshtml="` stops the Mono/Gecko install dialog (it blocked
    the first boot). Keep `~/.wine-ro` untouched: it is the fallback for Wine 9.0 (an 11.0-upgraded prefix must not go
    back to 9.0).
  - Switch: patch `tools/rogui/ai-client-wow64.patch` adds `ROAI_WOW64` (default 0 = unchanged). Apply it by writing
    a new file and `mv` over the old one (never edit a running bash script in place), stop the loop, end the old
    client with the SYSTEM `WINEPREFIX=~/.wine-ro wineserver -k` (the new wineserver only knows the wow64 prefix), then
    start `ROAI_WOW64=1 bash /mnt/g/RO/legion/tools/rogui/ai-client.sh`. Revert: restart without `ROAI_WOW64`. Check after
    the switch: `/tmp/roai-restarts.log` no longer shows drops every ~30 min, and the client's below-4 GB total stays
    flat (awk over `/proc/$(pgrep -f '^RO-AILab.exe')/maps`, addresses < 0x100000000).
  - Test hygiene for the future: never name a test exe RO-AILab.exe (ai-client.sh `pkill -f RO-AILab.exe` would kill
    it and its `pgrep` would take it for the watch client); use another display (:97) and another prefix.
- **Known launcher gap**: `ai-client.sh` checks "Ayla online" from the last 60 s of OpenKore logs, so right after an
  OpenKore restart it can try a login too early (one wasted attempt, then it recovers).
- **OpenKore gotchas**: never send a bare `ai` command (it TOGGLES the AI off; use `ai on`). Never set `lockMap` to a
  town (she just sits). `attackAuto 1` only fights back; use `2`. A restart takes ~1-2 min ("server still recognizes
  your last connection" is normal, it retries).
- X-Kore 3 was tried earlier and is broken with this client (OpenKore never relayed the server list).
- **Ayla's own stats on the dashboard** (2026-11-13): plugin `openkore-ayla/plugins/cmdfile/statusout.pl` (in
  `loadPlugins_list`) writes `logs/status.json` every 3 s (hp/sp/zeny/weight/levels/map/pos, `ts`). `legion` and
  `legion-core` mount `openkore-ayla/logs` read-only at `/ayla`; `legion/humanstats.go` reads it (ignored when > 30 s
  old) and fills her row in `/api/parties/detail`. `plugin load <name>` takes the plugin NAME as a regexp, not a path.
- Party dashboard shows each member's weight vs limit (`w cur/max (pct)`, client units = server/10): amber >= 50%
  (natural regen stops), red >= 90% (can't attack).

---------------------------------------------------------------------------------------------------------------------
