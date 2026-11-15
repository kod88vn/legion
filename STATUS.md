# STATUS - read this first

**Nothing is running, on purpose.** The owner said on 2026-11-14: "go ahead and proceed to implement, do not start any
server, we will test after I restart this machine". Do not start containers, background loops, minds or agents until
the owner says so.

## The redesign (approved 2026-11-14)
The rules that define it are in `RULES.md`:
- Section 0: the society model; the whole system keeps running without AI agents; every bot has one goal and works
  toward it without AI; two classes of agents (puppet masters / system agents); the main session listens and assigns;
  the two key metrics per bot (kills per minute, zeny per hour); token balance (most agents on Haiku); deploys every
  3 hours; short rules without repeated prompts.
- Section 3b: guilds compete on rank and wealth; members follow the guild's direction about 2 hours a day or risk
  being kicked.
- Section 4: giving among party and guild members; a chat agent helps bots communicate.
- Section 5: Ayla is Neo and has a direct, instant line to the system.

## Phase 1 - done 2026-11-14 (evening)
- **Stack stopped** with `docker compose stop` (bots first, then rathena, agency, db). Containers, images and the
  database volume `ro_dbdata` are all still there; nothing was removed.
- **Harness moved**: `G:\RO\server` was renamed to `G:\RO\legion` (this folder), and `sentinel.py`, `mindgate.py`,
  `tools/legion/`, `tools/rogui/` moved from `G:\RO\tools` into `tools/` here. The Go engine is at `legion/` inside
  this folder (`G:\RO\legion\legion`). Client graphics tools (dgvoodoo, magpie, realesrgan, sprhd) stayed in
  `G:\RO\tools`.
- **Git**: this folder is a git repository (branch `main`). First commit = the tree as moved. Second commit = path
  fixes and the hold notices. Runtime state, logs, backups and screenshots are ignored by `.gitignore` and stay on disk.
- **Mind gate paused**: `tools/mindgate.py` is silent while `tools/mindgate.paused` exists. A shim at the old path
  `G:\RO\tools\mindgate.py` forwards to it, for sessions started before the move.
- **Leftover processes stopped**: about 100 orphan `tail`/`grep` monitors from the previous session, the watch
  client keeper loop, x11vnc and Xvfb (they held the old folder open).

## Things that start the stack by themselves (know these before testing)
- `tools/rogui/keeper.sh` starts `ro-openkore-ayla` (and with it rathena and the db) whenever Ayla's container is not
  running. During Phase 1 a leftover keeper loop restarted those three 26 seconds after the stop; the loop was killed
  and they were stopped again. Do not start keeper.sh until the owner wants Ayla online.
- The game client launcher `G:\RO\kRO-210607\client\Play-AILab.cmd` (desktop shortcut "RO-English") runs
  `docker compose up -d` in this folder when the server ports are closed. Its path was updated to the new folder, so
  launching the client starts the whole stack.

## First start after the move (only on the owner's word)
- Start from this folder: `wsl -d Ubuntu-24.04 -- bash -c "cd /mnt/g/RO/legion && docker compose up -d"`.
- Every container will be **recreated**, because the bind-mount paths changed with the folder. That is expected.
  The compose project is still named `ro`, so the database volume `ro_dbdata` is reused and no game data is lost.
- rathena being recreated means Ayla's OpenKore and both Legion containers are recreated with it (they share its
  network); a plain `up -d` from here does all of that in one go.
- The deploy scripts (`tools/deploy/`) point at the new folder. `/tmp/legion-deploy.tag` in WSL still names the last
  deployed image (`ro-legion:deploy-20261114-145352`); `/tmp` may be emptied by the machine restart.

## Engine state
- The Go tree under `legion/` is the **frozen tree** of 2026-11-14: it passed `go vet` and `go test` at 16:26, then
  received a few small complete changes (restock order, npcextended.go, marketflow.go). It contains the fixes for the
  API wedge (reset.go storage handler, LFG goal loop) but **has not been deployed**; the last deploy was 14:55.
- Staged and not applied: `RESTART-NOTES/economy-farmer.go.staged`, `RESTART-NOTES/party_strand_edit.py`.
- There is no Go toolchain on Windows or in WSL; so far the engine was only compiled and tested inside a golang
  Docker container by the deploy scripts.

## Old documents
`AGENTS.md`, `HANDOVER.md`, `RESTART-NOTES/` and the `legion-session` skill still describe the old always-on setup
(15 minds, standing agents, restart order). Treat them as reference for how things are wired, not as instructions to
start anything.

## Next
(The main session fills this in.)
