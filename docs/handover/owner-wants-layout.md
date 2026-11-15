<!-- Moved unchanged from HANDOVER.md lines 163-208 (as of commit 63ca923). Index: HANDOVER.md. -->

## 1. What the owner wants

- A **local Ragnarok Online server** on their Windows 11 PC (`G:\RO`), everything in **Docker**, everything in **English**,
  used for experiments with **AI-driven bots**: up to ~1000 bots online out of ~4000 player profiles, each with a persona.
  The world must "feel real, like interacting with humans" (a Matrix simulation). Bots never reveal they are bots/AI.
- 99% of behaviour is **algorithmic** (Legion, Go). LLM "minds" handle judgement and words (chat, guild strategy).
- **Real progression**: every bot started at level 1 Novice (re-seeded 2026-11-13) and levels up for real, each with
  a build plan, levelling strategy and equipment path to 99. 10 ambitious guild leaders found guilds and recruit.
  Parties use tank/ranged/priest roles, bait-and-carry, even EXP share. Bots prefer farming, open shops for surplus,
  and must **run away from danger**.
- **Ayla's core test party** must **always be online**: Ayla (owner's account) + Zendancer84, Echobunny, Zen.fox, xXRinfangXx (Hunter), LilLilyblade (Monk; replaced the Alchemist Silent rogue 2026-11-14, AY11), vex_x18 (Crusader) (6h).
  Lessons from this party get built back into all bots. The owner **watches Ayla live** in a client window.
- A **dashboard** to see the world, talk to the minds, and see guild and party data.

### Owner rules (don't break these)
- **Never drive any client with the real mouse/keyboard**: "it keeps clicking the wrong thing and I cannot use my
  computer". Only the Wine client on the virtual display `:98` (xdotool there is fine) or OpenKore.
  (`tools/rogui/ro.ps1.disabled-uses-real-mouse`, `input.ps1.disabled-uses-real-mouse` are disabled on purpose.)
- **No fullscreen** for the owner's client.
- **No GM accounts for watching** (a hidden GM "camera" was tried and removed on request).
- **Don't remove information from the dashboard**; add, don't replace.
- **If something keeps failing, stop retrying and investigate** (find the root cause, don't loop).
- **Re-seeds must never touch human characters**: delete only by bot account range 2100001-2199999, never by char_id
  range (the owner's character "kitty" was lost once and recreated). Keep the core party: `seed -keep "Zendancer84,Echobunny,Zen.fox,xXRinfangXx,LilLilyblade,vex_x18,Silent rogue"` (Silent rogue left the party 2026-11-14 but stays in Starveil, AY11).
- Don't bypass anti-cheat / EAC, don't use pirated client downloads, don't unpack protected executables.
- Ask before outward-facing or hard-to-reverse actions (installing external tools, deleting data).

---------------------------------------------------------------------------------------------------------------------
## 2. Layout

| Path | What |
|---|---|
| `G:\RO\legion\docker-compose.yml`, `.env` | the stack: `ro-db` (MariaDB), `ro-rathena` (login/char/map), `ro-agency` (login token stub), `ro-legion` (bot engine + dashboard/API on :8090), `ro-openkore` (spare), `ro-openkore-ayla` (plays Ayla) |
| `G:\RO\legion\rathena\` | rAthena (Renewal, packetver 20210107). `conf/import/*` local config, `npc-custom/legion.txt` job changes / autoskill / starter kit |
| `G:\RO\legion\legion\` | Legion (Go). `README.md` = engine internals + HTTP API. `data/` = personas, memory, minds, guild roster, human parties, bugs.md, lessons.md, backups |
| `G:\RO\legion\tools\legionctl.py` | CLI the minds use (status, inbox, say, party, goal, minds bus...) |
| `G:\RO\legion\.claude\skills\legion-*` | skills for the mind roles (session, chat, life, guild, market, ops) |
| `G:\RO\legion\openkore\` | OpenKore image build (Dockerfile, servers-local.txt, custom serverType) |
| `G:\RO\legion\openkore-ayla\` | Ayla's OpenKore: `control/config.txt`, `control/sys.txt`, `control/commands.txt` (command inbox), `plugins/cmdfile/` (cmdfile.pl, **xkore2fix.pl**) |
| `G:\RO\kRO-210607\client\` | the owner's game client (`RO-AILab.exe`, WARP-patched 2021-01-07 client), launched via `Play-AILab.cmd` / desktop shortcut "RO-English"; login player/player |
| `G:\RO\legion\tools\rogui\` | watch-client and Ayla tooling (WSL scripts), screenshots in `shots\` |
| `G:\RO\tools\sprhd\`, `realesrgan\` | HD sprite pipeline (AI-upscaled sprites packed in `sprites_hd2.grf` for the owner's client) |
| `G:\RO\WARP\` | client patcher (profile `Inputs\ailab-2021-en.yml`: NoHourly, CustomFontHeight 13) |
| `C:\Users\kod\.claude\projects\G--RO\memory\` | Claude Code memory (rules above, core party) |

---------------------------------------------------------------------------------------------------------------------
