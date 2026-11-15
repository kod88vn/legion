# Local rAthena + OpenKore (Docker in WSL Ubuntu-24.04) + 2021 kRO client

Packet version **20210107, RagexeRE** (Renewal). It matches `RagexeRE.exe` (kRO Sakray RagexeRE built 2020-12-29),
which ships in Gravity's official full-client zip `http://rofull.gnjoy.com/Ragnarok_210607.zip`.
rAthena and OpenKore are pinned to fixed commits.

![demo](demo-2021-client-with-bot.png)
![english](demo-2021-client-english.png)

## Run the server (from PowerShell)
```
wsl -d Ubuntu-24.04 -- bash -c "cd /mnt/g/RO/legion && docker compose up -d --build"
wsl -d Ubuntu-24.04 -- docker logs -f ro-openkore      # watch the bot
wsl -d Ubuntu-24.04 -- docker attach ro-openkore       # type OpenKore commands (detach: Ctrl-P Ctrl-Q)
wsl -d Ubuntu-24.04 -- docker logs -f ro-rathena       # server logs ([login]/[char]/[map] prefixes)
```
`Play-AILab.cmd` (desktop shortcut **RO-English**) starts the stack itself if it isn't running.

### WSL networking (important for smooth movement)
`%UserProfile%\.wslconfig`:
```
[wsl2]
networkingMode=mirrored   # Windows localhost goes straight into WSL
vmIdleTimeout=-1
[general]
instanceIdleTimeout=-1    # don't stop Ubuntu (and the server) when no WSL terminal is open
```
With WSL's default NAT mode, Windows reaches Docker through `wslrelay.exe`, which held the client's small
packets for ~0.4 s. The client moves the character immediately, so every (late) server walk reply pulled it
back: constant rubber-banding when holding the mouse to walk. Measured: click -> server went from ~440 ms to
~1 ms, and hold-to-walk no longer snaps back. In mirrored mode, ports published on all interfaces would also be
reachable from the LAN, so every port in `docker-compose.yml` is bound to 127.0.0.1.

Second rubber-band source (hold-to-walk toward far cells): rAthena silently rejects walk requests longer than
`max_walk_path` (default 17) and, with `OFFICIAL_WALKPATH`, longer than 14 when the straight line is blocked. At
1920x1200 the client asks for 20+ cell walks; it starts walking immediately, and the next accepted reply snapped
it back over a second of walking. Fixed with `max_walk_path: 32` (rAthena's maximum) (conf/import/battle_conf.txt) and
`OFFICIAL_WALKPATH` disabled in the Dockerfile. Remaining by design: a direction change mid-step is applied at the
next cell centre (up to ~0.2 s), as on official servers.

Containers: `ro-db` (MariaDB), `ro-rathena` (login/char/map), `ro-agency` (login-agency stand-in for the
2021 client, see below), `ro-openkore` (bot).

## Play (human client)
Double-click `G:\RO\kRO-210607\client\Play-AILab.cmd`, click OK twice (agreement, server "AI-Lab (local rAthena)"),
log in as **player / player**, pick "AI-Lab", then select **Admin** and Start (the pink button).
Admin is GM 99, so `@jumpto AIBot`, `@recall AIBot`, `@warp prontera` etc. work.

### How the client was made to work (all reproducible)
| piece | what | where |
|---|---|---|
| exe | `RagexeRE.exe` from the official zip (Gravity-signed, unprotected), WARP-patched to `RO-AILab.exe` | WARP session `G:\RO\WARP\Inputs\ailab-2021-en.yml` (copy in `client-tools/`): ailab-local patch set with `UseOldLogin`, plus the English patches `MsgStrings` (read msgstringtable.txt instead of built-in Korean), `QuestDisplay`, `FixArrowsCharset`, `TranslateTaekwon`, `IgnoreLuaErr`, and `Custom*Lub` redirects to `SystemEN\` (iteminfo, towninfo, tipbox, quests, achievements, mapInfo) |
| hosts | kRO's hard-coded login-agency and login-server hostnames are rewritten to 127.0.0.1 | `client-tools/patch_hosts_2021.py RO-AILab.exe` (run after WARP) |
| strings | two hard-coded Korean default chat-tab names -> English (minor; the visible tab names come from msgstringtable) | `client-tools/patch_strings_2021.py RO-AILab.exe` (run after WARP) |
| English | ROenglishRE (`G:\RO\ROenglishRE`, commit f7d270e) generated like its ClientGenerator for a **2020-09-02 Renewal** client (Renewal + Compatibility 2017-06-14, 2017-12-13\Renewal, 2018-06-20\Renewal, 2019-06-05, 2020-09-02), copied over the client's `data\` (except clientinfo/sclientinfo.xml) and `SystemEN\`. `data\luafiles514\lua files\worldviewdata\worldviewdata_list.lub` was removed (newer world-map list; it crashed this client with "querymaptable_load // WorldMap_Pw02") | client folder; generated set in `G:\RO\temp\roen-2020-09-02` |
| login | the client sends kRO's `0x0ACF` token request to the "agency" on :6953; `ro-agency` answers `0x0AE3` with token = account name; the client then logs into rAthena with `0x0825` (SSO) and that token as the password | `agency/agency.py` |
| GRFs | `DATA.INI`: `rdata_ok.grf` then `data.grf`. `rdata_ok.grf` is `rdata.grf` minus one encrypted entry the client fails on (`msgstring_kr_s.lub`), whose decoded copy sits in `data\luafiles514\lua files\` | client folder |
| config | `data\clientinfo.xml` / `sclientinfo.xml` point to 127.0.0.1:6900 | client folder |
| D3D | `HKCU\Software\Gravity Soft\{Ragnarok,RenewSetup}\GUIDDEVICE` = Direct3D T&L HAL device GUID (this client uses D3D7; an all-zero GUID makes it exit with "Cannot init d3d OR grf file has problem.") | registry |

**Security note:** because of the agency stand-in, the password typed into the *client* is not checked. The
token (= account name) is checked by rAthena, so it only works for accounts whose password equals their
name. 6953 is published on 127.0.0.1 only.

**English UI:** done (menus, windows, chat, item and map names, see `demo-2021-client-english.png`). Still Korean:
the age-rating artwork on the loading/login screens (they're pictures), the world-map list, and a few rarely seen
hard-coded strings. If chat tabs ever show Korean names, delete `savedata\ChatWndInfo_U.lua` (per-PC UI save).
The previous Korean-UI exe is kept as `RO-AILab.korean-ui.exe`.

Rebuild the exe from scratch:
```
G:\RO\WARP\win32\WARP_console.exe -using Inputs\ailab-2021-en.yml -from <client>\RagexeRE.exe -to <client>\RO-AILab.exe   (run in G:\RO\WARP)
python -I G:\RO\legion\client-tools\patch_hosts_2021.py <client>\RO-AILab.exe
python -I -X utf8 G:\RO\legion\client-tools\patch_strings_2021.py <client>\RO-AILab.exe
```

### Sound
If there's no music or sound, check the Windows Volume Mixer: Windows remembers a per-program mute, and
`RO-AILab.exe` had been muted there (the game was playing audio the whole time). It's unmuted now.

### HD sprites (AI-upscaled)
`sprites_hd.grf` (first in `DATA.INI`) holds AI-upscaled copies of the sprites: player bodies and hair at 4x,
monsters, headgear and shields at 2x. Every animation layer is scaled down by the same factor, so characters
keep their normal size but have 4x (or 2x) the detail. Frames stay palette-indexed and are mapped back onto each
sprite's own palette, so hair colours and job dyes still work. To go back to the original sprites, delete the
`0=sprites_hd.grf` line from `DATA.INI` (or restore `DATA.INI.bak-before-hd`).

Pipeline: `G:\RO\tools\sprhd` (Real-ESRGAN `realesrgan-x4plus-anime` via ncnn/Vulkan in `G:\RO\tools\realesrgan`).
- `run_all.cmd` upscales the categories in order; `python -I -X utf8 batch.py <category>` does one (resumable;
  results cached in `G:\RO\temp\sprhd\cache`); `batch.py --count` lists categories; `batch.py --build` writes
  `sprites_hd.grf` from the cache (close the client first).
- Encrypted GRF entries are decrypted with rAthena's own DES code (`grfdec.sh`, built in the ro-openkore image).
- Real-ESRGAN gotcha on this GPU/driver: more than one processing thread (or several processes at once, or auto
  tiling) gives all-black output for multi-tile images, so it runs single-threaded with `-t 256` and every output
  is checked for black images.
- Not done (yet): robes/wings (15k files), job weapons, NPCs.
- Attach points (where heads/headgear sit on bodies) are written multiplied by the upscale factor, because the
  client multiplies them by the layer scale. Without that, heads floated off the body in attack poses (standing and
  walking hid it because body and head attach points coincide there). `fix_acts.py` regenerates all cached ACTs
  from the originals without re-upscaling.

### Text size
WARP's `CustomFontHeight` is in the session (`$newFontHgt`, 4-byte little-endian, `type: 0`): currently **15 px**
(default ~10). True 2x (20 px) clips text in the fixed-size UI boxes (login fields, buttons, Basic Info).

## Legion: a server full of players
See `AGENTS.md` (how LLM agents drive it) and `legion/README.md` (engine). Container `ro-legion` runs 4,000 player
profiles, up to 1,000 online, on daily schedules (API on 127.0.0.1:8090, CLI `tools/legionctl.py`). Start the
minds with `/legion-session` in Claude Code or the `legion-start` prompt in Copilot. rAthena allows unlimited connections
from 127.0.0.1 (`conf/import/packet_conf.txt`) because every bot connects from there.

## Accounts (seeded on first DB init, see rathena/sql/seed.sql)
| user / pass     | char  | notes                       |
|-----------------|-------|-----------------------------|
| player / player | Admin | GM group 99, for the human client |
| bot1 / bot1     | AIBot | driven by OpenKore          |

## Layout
- `rathena/conf/import/*`: server overrides (IPs 127.0.0.1, DB=db, exp rates)
- `rathena/Dockerfile`: `PACKETVER` build arg (compose sets 20210107); `FORCE_MAIN=1` would build "main Ragexe" semantics instead of rAthena's forced RagexeRE window (20200902..20211118)
- `openkore/control/`: live OpenKore config (seeded on first start, so edit freely; delete `.configured` to re-seed)
- `openkore/servers-local.txt`: `[rAthena Local]`, serverType `kRO_RagexeRE_2020_07_23`, tables `kRO/RagexeRE_2020_09_02b`
- `openkore/serverTypes/`: extra profile `kRO_Ragexe_2021_06_02` (for a main-Ragexe 20210602 server; unused now)
- ports (all 127.0.0.1 only): 6900 login, 6121 char, 5121 map, 6953 agency, 3306 MariaDB

Reset everything: `docker compose down -v` (wipes the DB volume).
