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

## Server and client (session save, 2026-11-15)
Facts read from the repo on 2026-11-15. Where the repo has no fact, the line says `not found in repo`. Passwords,
tokens and keys are never written here: the names of the keys are listed, the values stay in the files named.
Current state and what may start: `STATUS.md`. Plan and progress: `PLAN.md`, `PROGRESS.md`. Engineering wiring:
`HANDOVER.md`. Settings and owner decisions: `docs/rules/settings.md`.

### Server: rAthena in Docker (WSL Ubuntu-24.04)
- Built for packet version **20210107** (RagexeRE): `PACKETVER` build arg in `docker-compose.yml`, image
  `ro-rathena:20210107re`, Dockerfile in `rathena/`. rAthena and OpenKore are pinned to fixed commits (see the
  Dockerfiles). Run from WSL: `wsl -d Ubuntu-24.04`, project at `/mnt/g/RO/legion`.
- Containers (`docker-compose.yml`; every published port is on 127.0.0.1 only):

| container | service | what | ports (host) |
|---|---|---|---|
| `ro-db` | db | MariaDB 11.4, database `ragnarok` | 3306 |
| `ro-rathena` | rathena | login, char and map servers; shares its network with the OpenKore and Legion containers | 6900 login, 6121 char, 5121 map, 8090 Legion API, 6901 X-Kore 2 (Ayla's watch port) |
| `ro-agency` | agency | login-agency stand-in for the 2021 client (`agency/agency.py`) | 6953 |
| `ro-openkore-ayla` | openkore-ayla | Ayla's OpenKore; shares rathena's network, no port of its own | via rathena 6901 |
| `ro-legion` | legion | the Legion engine and the bot population (`LEGION_BOTS`, default 4000; `LEGION_MAX_ONLINE`, default 1000) | 8090 (shared with rathena) |
| `ro-legion-core` | legion-core | the instance that runs Ayla's party members (`LEGION_CORE`); its API is 8091 inside rathena's namespace | 8091 (internal) |
| `ro-openkore` | openkore | old seed test bot AIBot; profile `legacy`, off since 2026-11-14 (AY14) | none |
| `ro-openkore-qa` | openkore-qa | QA bot; profile `qa`, not started by default | none |
| `ro-mastermind` | mastermind | optional fallback only; profile `fallback` | none |

  The Accounts and Layout sections above are older text: they still name `ro-openkore` as the bot. The table
  above is the current list.
- Rates and caps (`rathena/conf/import/*`; IDs in `docs/rules/settings.md`):
  - Base and job EXP 500 percent (5x): `base_exp_rate`, `job_exp_rate` in `battle_conf.txt` (ST04).
  - Item drop rates 500 percent for every class of item (common, heal, use, equip, card, boss, MVP, treasure) in
    `battle_conf.txt` (ST05). `mob_count_rate: 400` (4x monsters) is set there too; `settings.md` does not list it.
  - Walk path `max_walk_path: 32` in `battle_conf.txt`: the fix for the rubber-banding described above.
  - Job cap: 2nd classes only. No rebirth, no 3rd or 4th jobs (ST01 / G10), enforced by the NPC unloads in
    `rathena/npc-custom/scripts_custom.conf`.
  - Level limits: level 99 and 1,273 stat points at level 99; 2nd-class job level cap 50. Sources:
    `docs/classes/all.md` (build milestones, about line 201), `docs/classes/acolyte.md` (about line 75) and
    `docs/classes/mage.md` (about line 67). The repo has no rathena `max_base_level` or `max_job_level` override in
    `rathena/conf/import`.
  - Capacity: about 1,024 connections; 960 normal bot slots with headroom for guild leaders, Ayla's party and humans (ST06).
  - Chat (party, guild, whisper, global) is logged so the dashboard can show it (ST07).
  - Connections from 127.0.0.1 are not limited (`conf/import/packet_conf.txt`), because every bot connects from there.
- Server addresses: `char_ip`, `login_ip` and `map_ip` are 127.0.0.1 (`char_conf.txt`, `map_conf.txt`).
- Accounts: seeded on the first database start from `rathena/sql/seed.sql`. The test accounts are in the Accounts
  table above (passwords are not repeated here). The GM account `wander` is owner-only (G03). The repo does not seed
  it (`not found in repo` as an account; the rule names it).
- Environment keys (`example.env`; copy it to `.env` and never commit values): `LEGION_BOTS`, `LEGION_MAX_ONLINE`,
  `LEGION_CORE` (the names Ayla's party members run under; the current names are in `docs/rules/ayla.md`), and the
  optional `ANTHROPIC_API_KEY` (fallback mastermind only). The game database user and password are set in the `db`
  and `rathena` services of `docker-compose.yml`; they are not repeated here.

### Database
- `ro-db` (MariaDB 11.4). Seed: `rathena/sql/seed.sql`. The market crier's table is `rathena/sql/legion_notice.sql`
  (not loaded yet; the crier is off).
- Reset everything: `docker compose down -v` (wipes the volume; never on live data, G23).
- Dashboard read access: the user `dash_ro` is SELECT-only. Its name and password are in the git-ignored
  `tools/.state/dashboard.json` (keys `db_user`, `db_pass`). Without that file the dashboard falls back to the game
  user, and every query still goes through a read-only guard (`tools/dashboard/dbsnap.py`).

### Dashboard
- `tools/dashboard/dashserve.py` on **127.0.0.1:8099** (flags `--port`, `--bind`, `--engine`). Kept on by
  `tools/dashboard/keep.cmd`, which restarts it when it exits.
- Tabs: server status on top, then Players, Builds and Market. It reads `ro-db` through the read-only user above and
  the Legion API on 8090 when the engine is up. Environment overrides: `LEGION_WSL_DISTRO` (default Ubuntu-24.04),
  `DASH_CONFIG`, `DASH_CACHE`, `DASH_SNAP`, `DASH_AYLA_LOGS`, `DASH_CLIENT_ITEMS`, `LEGION_API`.

### Client (the human player's client, Windows)
- Official kRO RagexeRE client, 2021 build: `RagexeRE.exe` from Gravity's full client zip (`Ragnarok_210607.zip`;
  the URL is in the packet note above). Patched to `RO-AILab.exe` with WARP. The WARP session file is
  `client-tools/ailab-2021-en.yml` (a copy; the working one is `G:\RO\WARP\Inputs\ailab-2021-en.yml`). Host patch:
  `client-tools/patch_hosts_2021.py` points the login hosts at 127.0.0.1. String patch: `client-tools/patch_strings_2021.py`.
- The client folder is `G:\RO\kRO-210607\client\` (outside this repo). Its `data\clientinfo.xml` and
  `sclientinfo.xml` point at 127.0.0.1:6900 (login). The agency host is rewritten to 127.0.0.1 by the hosts patch and
  answered on 6953 (`agency/agency.py`).
- Start: double-click `Play-AILab.cmd` (desktop shortcut "RO-English"). It checks ports 6900, 6121, 5121 and 6953 and,
  when the server is down, starts the stack in WSL (`docker compose up -d`). Agents do not start this client; the
  stack is started only on the owner's word (`STATUS.md`, "How to start"; rule G01 in `docs/rules/gates.md`).
- Server list entry: "AI-Lab (local rAthena)". Login: see the Accounts table above.
- The client uses D3D7; its device GUID is set in the registry (see the table above). Text size 15 px. Sprite
  upscales are in `sprites_hd.grf` (first in `DATA.INI`). The English UI is generated from ROenglishRE for a 2020-09-02 client.

### The agents' Wine client (Ayla's view; not a human client)
- A Wine copy of the same client (`~/roclient/RO-AILab.exe` in WSL, private prefix `~/.wine-ro`; not in this repo)
  runs on the virtual display **:98** at 1280x720. A view-only VNC copy is served on :5998 (`docs/handover/ayla-live-view.md`).
- Scripts for it are in `tools/rogui/` (`ai-display.sh`, `ai-client.sh`, `keeper.sh`, `ayla.sh`). The `ro.ps1` and
  `input.ps1` files that used the real mouse are disabled (G01): agents never drive a client with the real mouse or keyboard.
- Wine: the live client runs on system Wine 9.0 with the private prefix `~/.wine-ro` (`docs/handover/ayla-live-view.md`,
  `tools/rogui/ai-client.sh`). The WoW64 fix (Wine 11.0 in new-WoW64 mode, its own prefix) is pending and not switched
  on: `ROAI_WOW64` defaults to 0 in `tools/rogui/ai-client.sh`.

### Start and stop
Not copied here. The order, the owner's words and the commands are in `STATUS.md` ("How to start", "Stopping"); the
stack is `python tools/system/stackctl.py up|down`. A rathena recreate also recreates Ayla's OpenKore and the Legion
containers (G15). A rathena restart is allowed at most once an hour (G16).

### Logs and data
- Container logs: `docker logs ro-rathena` (login, char and map lines carry `[login]`, `[char]`, `[map]` prefixes),
  `docker logs ro-legion`, `docker logs ro-openkore-ayla`. From Windows: `wsl -d Ubuntu-24.04 -- docker logs -f ro-rathena`.
- Server log and packet settings: `rathena/conf/import/log_conf.txt` and `packet_conf.txt`.
- Engine running state (git-ignored): `legion/data/` inside the Go package folder (repo path `legion/legion/data/`):
  the bug queue `bugqueue.jsonl`, the meter `meter.jsonl`, `lessons.md`, and `watchdog.log`.
- Ayla's OpenKore logs and status: `openkore-ayla/logs/` (mounted read-only into `ro-legion` as `/ayla`; `status.json`).
- Dashboard state and snapshots: `tools/.state/` (git-ignored; `dashboard.json`, `dbsnap/`).
- Reports and reviews: `docs/reports/` and `docs/reports/reviews/`. Lessons index: `docs/rules/lessons-index.md`.
- Backups: `backups/` and `legion/data/backup-*` (git-ignored; nothing is deleted without the owner, G22).
