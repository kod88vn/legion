# Rules core (always loaded)

Owner's standing orders. They override defaults and earlier designs. Details live in topic files; the owner's original words are in `docs/rules/archive.md` (never auto-loaded).

**Goal (PR03).** A Matrix-like simulation of real people. Judge every choice by "what would real players and a normal society do?" When no rule covers a case, pick the most natural human behaviour: no mechanical shortcuts, no mass actions, believable pacing. Nothing is started (no containers, servers, loops, agents) until the owner says so (G21).

## Hard gates (the full register with enforcement is `docs/rules/gates.md`)
- G01 (SA01) Never drive any client with the real mouse or keyboard. Only the Wine client on virtual display :98, or OpenKore.
- G02 (SA02) No fullscreen for the owner's client.
- G03 (SA03) No GM account for watching the world.
- G04 (SA04) The GM test account stays out of the economy and parties and never talks in public.
- G05 (SA05) Never turn Ayla's AI off: never send a bare `ai`, `ai off` or `ai manual` to her OpenKore; never weaken or test the `cmdfile.pl` guard. To inspect her use only `where`, `ai_v`/`aiv` via logs, or `ai on`.
- G06 (SA06) Re-seeds never touch human characters: delete only in the bot account range 2100001-2199999, keep Ayla's party (`-keep`).
- G07 (SA07) No anti-cheat bypass, no pirated client downloads, no unpacking of protected executables.
- G08 (SA08) Never remove information from the dashboard; add, do not replace.
- G09 (EC10) No items added to NPC shops; stock rAthena shops stay as shipped.
- G10 (ST01) The server caps at transcendent classes: no 3rd or 4th jobs.
- G11 (EC02) A wrong build is deleted and restarted only on bot accounts, never on humans or Ayla's party.
- G12 (AY09) Nothing may send away Ayla's zeny if it would leave her under 10,000z; under 10,000z she sends none.
- G13 (PR23) The main session never controls Ayla (no map orders, rallies, party lines or moves); it has root-cause bugs fixed.
- G14 (PR11) The main session does no coding: it listens to the owner and bug reports and assigns fixers. All coding, fixes and deploys are done by subagents.
- G15 (PR29) Deploys never recreate rathena by accident: only `legion`/`legion-core` with `--no-deps`. Recreating rathena also recreates Ayla's OpenKore and the legion containers.
- G16 (PR30) Restart or recreate rathena at most once an hour.
- G17 (PR32) Deploys every 3 hours (critical unblocking fixes excepted), as one batch assigned by the main session; only the deploy agent deploys, both `legion` and `legion-core`, and it alone runs go vet/test and image builds. Working subagents report "built" and never deploy.
- G18 (CT05) Never reveal or hint that players are bots, AI, a simulation or a script.
- G19 (AY03) Ayla and her party are always online. (G19 to G23 confirmed as hard gates by the owner, 2026-11-14.)
- G20 Ayla's party is never subjected to kicks, resets or build resets. (Human part removed by the owner 2026-11-14; see G25.)
- G21 Nothing is started until the owner says so (`STATUS.md` hold; temporary).
- G22 Ask the owner before outward-facing or hard-to-reverse actions (installing external tools, deleting data).
- G23 Tests and experiments never touch live data: not `ro-db`, not `~/.wine-ro`, not the running-state files in `legion/data`.
- G24 (PR31) The deploy agent may `docker stop`/`docker kill` stray golang test or build containers that block a deploy, NEVER rathena, Ayla, legion or the db.
- G25 No in-world discrimination between humans and other players: a human is subject to the same rules as everyone (guild tax, duty, warnings, kicks, prices, trades, chat), and nothing in the world may treat a human in a way that reveals the difference. Data safety stays (G06, G11). (2026-11-14)

## Recording a new rule (PR02)
When the owner states a rule (a "must", "always", "never", "should", "apply to all ..."), in the same turn, without waiting to be asked: add one short plain line (ID, statement, date; no quote of the owner) to the right file under `docs/rules/` (class rules under `docs/classes/`), put the owner's words with the date in `docs/rules/archive.md` under the same ID, and add a row to `docs/rules/coverage.md`. A new hard gate also gets a G ID in `docs/rules/gates.md` and a line above; then run `python tools/system/gatecheck.py --coverage`. A new rule that replaces an old one edits the old line.

## What to read for which task
| task | read |
|---|---|
| anything first | `STATUS.md` (what runs, what is on hold) |
| roles, deploys, metrics, who does what, dashboard rules (DB01, DB02) | `docs/rules/process.md` |
| bot economy, farming, loot, town runs | `docs/rules/economy.md` |
| parties and party leaders | `docs/rules/parties.md` |
| guilds, loyalty, guild leaders | `docs/rules/guilds.md` |
| chat and giving between players | `docs/rules/chat.md` |
| Ayla, her party, her guild Starveil | `docs/rules/ayla.md` |
| server rates, caps, names, owner decisions | `docs/rules/settings.md` |
| a class or build | `docs/classes/all.md` and the class file (`novice`, `swordman`, `mage`, `archer`, `acolyte`, `merchant`, `thief`, `transcendent`) |
| playing a character as an in-world agent | `AGENTS.md` |
| engineering: how it is wired, current state | `HANDOVER.md`, then `docs/handover/*` |
| the redesign and work packages | `docs/design/REDESIGN.md` |
| past bugs and lessons | `docs/rules/lessons-index.md` (into `legion/data/lessons.md`) |
| where an old rule went | `docs/rules/coverage.md` |
