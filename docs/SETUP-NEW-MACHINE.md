# Setting up on a new machine

Written 2026-11-15 by the implementer on branch `docs/new-machine`, from the repo as it stood at `main` 0d2e7c2.
Status: **written from the repo, not run on a clean machine.** Every step marked `not found in repo` is a gap to fill
by the owner. Nothing here starts a container, a loop or a server. The start order is in `STATUS.md` ("How to start"),
which this guide points to and does not copy.

Read first: `STATUS.md` (what runs and what is on hold), `RULES.md` (G21: nothing starts until the owner says so), and
`HANDOVER.md` (how it is wired). The machine check is `tools/system/portcheck.py`; the owner-supplied file list is
`docs/NEW-MACHINE-MANIFEST.json`.

## 0. Rules that apply to every step

- Nothing starts until the owner says so, part by part (G21). The first run is held: see section 8.
- Live data is never touched by a test or an experiment (G23). A dump and a restore are owner operations on the
  owner's chosen machine, not test runs.
- Secrets stay out of git and out of chat: `.env`, `backups/`, `tools/.state/` and `legion/data/` runtime files are
  git-ignored (`.gitignore`). Section 10 lists the tracked files that hold secrets today.
- **The game client is optional.** The engine speaks the RO login, char and map protocol itself
  (`legion/bot.go`, the login handshake at lines 417-443), so the Legion simulation runs with no client. Ayla's
  OpenKore runs headless in its container (`docker-compose.yml`, service `openkore-ayla`, no GUI). The only client
  users are: the watch view `tools/rogui/ai-client.sh` (a Wine copy that shows Ayla's OpenKore session, see
  `docs/handover/ayla-live-view.md`), a human who logs in to watch or play (the owner's own 2021 client copy), and
  `tools/dashboard/buildexport.py` when it rebuilds `tools/dashboard/builds.json`. Nothing in the engine needs the
  client's files at run time.

## 1. Prerequisites and versions

| item | version or detail | where it is pinned or seen |
|---|---|---|
| Windows | Windows 11 Pro (this machine: 10.0.26200) | owner's machine |
| WSL2 | distro named `Ubuntu-24.04` (the scripts call it by this name) | `STATUS.md`, `tools/rogui/*.sh` |
| Docker | Docker Engine inside WSL with `docker compose` v2; the stack uses `name: ro` | `docker-compose.yml`; the Docker version is `not found in repo` |
| Git | Git for Windows (Git Bash). Shell scripts are LF (`.gitattributes`) | `.gitattributes`, `HANDOVER.md` |
| Python | 3.12.x on Windows (this machine: 3.12.10) | `.claude/settings.json` hooks |
| Go | 1.22.x inside WSL at `~/go-toolchain` (this machine: 1.22.12), installed by hand, no sudo | `docs/design/toolchain.md`; `legion/go.mod` says `go 1.22` |
| Wine | system Wine 9.0 in WSL, for the optional watch view only; WineHQ 11 is an optional switch | `docs/handover/ayla-live-view.md`, `tools/rogui/ai-client.sh` |
| Container images | `mariadb:11.4`, `golang:1.22`, `ubuntu:24.04`, `debian:bookworm` and `debian:bookworm-slim`, `python:3.12-alpine` | the Dockerfiles and `docker-compose.yml` |
| Build-time network | rAthena at pinned commit `1ca3e77...` and OpenKore at pinned commit `edbd385...`, both fetched from GitHub when the image builds | `rathena/Dockerfile`, `openkore/Dockerfile` |
| rAthena build | PACKETVER `20210107` (kRO RagexeRE 2021-01-07) | `docker-compose.yml` build args |

**Install pointers.** The repo does not install any of these; the owner runs each step. Versions not recorded in the repo
are marked `not found in repo`.
- WSL2: in an administrator PowerShell, `wsl --install -d Ubuntu-24.04`, reboot, then create the Linux user. The WSL
  version: `not found in repo`.
- Docker: Docker Engine inside the Ubuntu-24.04 distro, with `docker compose` v2 (`docker compose version` must print a
  version). Add your Linux user to the `docker` group so `docker` runs without `sudo`. The Docker version and whether
  Docker Desktop was used: `not found in repo`. Docker is needed before step 1 of section 7.
- Python: Python 3.12 for Windows (python.org installer; this machine 3.12.10). The hook paths in section 9 must match
  the installed interpreter. The Python version inside WSL: `not found in repo`.
- Git: Git for Windows, which includes Git Bash. The version: `not found in repo`.

## 2. Clone

- The git repo is the folder `G:\RO\legion` (branch `main`). Its `legion/` subfolder is the Go engine, and its
  `legion/data/` is the engine's runtime folder. Paths in this guide are relative to `G:\RO\legion` unless stated. The
  folder above it, `G:\RO`, is not a repo. It holds `CLAUDE.md` and the client folders (section 4).
- There is no git remote in this clone (`git remote -v` is empty; `not found in repo`). Move the repo with one of these:
  - **Bundle (git history, branches and worktree branches; committed work only).** On the old machine, in Git Bash:
    ```
    cd /g/RO/legion
    git status --short                       # commit anything that must travel first
    git bundle create /g/RO/ro-legion-<YYYYMMDD>.bundle --all
    git bundle verify /g/RO/ro-legion-<YYYYMMDD>.bundle
    ```
    Copy the `.bundle` file to the new machine (a USB stick is fine). On the new machine, in Git Bash, into an empty or
    missing folder:
    ```
    git clone /g/RO/ro-legion-<YYYYMMDD>.bundle /g/RO/legion
    cd /g/RO/legion && git branch -a
    git remote remove origin                 # optional: the clone records the bundle file as "origin"
    ```
    The bundle does not carry git-ignored files. Those come separately (section 4 and the file list in
    `docs/NEW-MACHINE-MANIFEST.json`).
  - **Whole folder.** Copy `G:\RO\legion` including `.git`. Git-ignored files come with it, but the copied `.git` still
    names the worktrees of the old machine: run `git worktree prune` on the new machine.
- Recommended layout: `G:\RO\legion` so that the stock paths in the scripts work (section 9 covers other layouts).
- Worktrees (`G:\RO\legion-wt\...`) are local working copies and are not part of the repo. They do not travel with a
  bundle.

## 3. Create `.env` from `example.env`

Copy `example.env` to `.env` in the repo root (`G:\RO\legion\.env`) and fill in the keys below. Never commit it. Key names only here:

| key | required | what it does |
|---|---|---|
| `LEGION_BOTS` | yes | how many bot personas the main engine runs (compose default when unset: 4000) |
| `LEGION_MAX_ONLINE` | yes | at most this many bots online at once (compose default: 1000) |
| `LEGION_CORE` | yes | comma-separated names of Ayla's core party, run by `legion-core`; the main engine excludes them. Must be the same in both services. The current names are in `docs/rules/ayla.md` |
| `ANTHROPIC_API_KEY` | no (commented in example) | only for the optional fallback mastermind container; off by default |

Read by compose with defaults but not listed in `example.env` (optional): `MASTERMIND_MODEL`, `MASTERMIND_CYCLE_SEC`.
`tools/system/portcheck.py` section [b] checks the required key names against `example.env`.

## 4. What the owner must bring over (not in git)

Relative to `G:\RO\legion` unless stated. `Needed` means the stack or the agents fail without it. `Optional` means
the stack runs without it. The manifest `docs/NEW-MACHINE-MANIFEST.json` lists the same files for `portcheck.py`.

| item | goes to | needed | note |
|---|---|---|---|
| `.env` | `legion\.env` | needed | section 3 |
| owner's client copy | `G:\RO\kRO-210607\client\` (`RagexeRE.exe`, `RO-AILab.exe`, `DATA.INI`) and the full-client zip `G:\RO\kRO-210607\Ragnarok_210607.zip` | optional | the owner's own download (Gravity full client 2021-06-07). The repo never fetches a client (G07). `RO-AILab.exe` is the English, patched copy: rebuilt with the WARP steps in `README.md` (client section) and the two `client-tools/*.py` patch scripts |
| English client data | `G:\RO\ClientEN\` | optional | item table for `tools/dashboard/buildexport.py` (`SystemEN\LuaFiles514\itemInfo.lua`); only when rebuilding `builds.json` |
| live-world DB dump | `legion\backups\` | optional, needed to resume the live world | see section 5. The only dump on disk is older (`backups/pre-rebuild-20261114-0038.sql`, 2026-11-14 00:38; git-ignored, not in the repo) |
| `legion/data` runtime state | `legion\legion\data\` | optional | the git-ignored files for the parties, guilds, economy and Ayla's party: `human-parties.json`, `parties*.json`, `guild_*.json`, `friends*.json`, `econ_counters*.json`, `markdowns*.json`, `levels.json`, `char_levels.json`, `ayla_rotation.json`, `memory/` (3,833 files), `owner.token`. A fresh world does not need them; a resumed one does. The full list is `git status --ignored` |
| dashboard read login | `legion\tools\.state\dashboard.json` | optional | holds the keys `db_user` and `db_pass` for the SELECT-only dashboard user `dash_ro`. Without it the dashboard uses the game user (`README.md`, Database). The `dash_ro` creation statements are `not found in repo` (see section 11) |
| item names for the dashboard | `legion\openkore-ayla\logs\items_lut.tsv` | optional | read by `tools/dashboard/offline.py`; the writer is OpenKore's own code, `not found in repo` |
| `CLAUDE.md` | `G:\RO\CLAUDE.md` | needed for agent sessions | project instruction root, outside git |
| Claude memory and agent state | `C:\Users\<user>\.claude\projects\G--RO\memory\` | owner decides | per user, outside git. The folder name encodes the drive and folder (`G--RO`) |
| Wine copies and prefixes in WSL | `~/roclient`, `~/.wine-ro`, optionally `~/wine11-wow64` and `~/.wine-ro-wow64` | optional | watch view only; rebuilt from the owner's client; the setup steps are `not found in repo` in full |
| Go toolchain in WSL | `~/go-toolchain/go` | needed for `tools/dev/gocheck.sh` only | installed by hand (section 1) |

Already in git and therefore coming with the clone (do not copy over them): `legion/data/personas.json` (4,000
profiles, 7,817 characters), `legion/data/seed.sql`, `rathena/sql/seed.sql`, `docker-compose.yml`, the Dockerfiles,
`rathena/conf/`, `rathena/npc-custom/`, `openkore/` and `openkore-ayla/` control and plugins, `tools/`, `docs/`.

## 5. The database: how it is seeded, and the dump

**Volume and service.** The compose service `db` (image `mariadb:11.4`, container `ro-db`) keeps its data in the
volume `dbdata`, which compose names `ro_dbdata` (project name `ro`). Its database is `ragnarok`. The user and passwords
are set in the `db` service of `docker-compose.yml` (environment keys `MARIADB_*`); they are not repeated here.

**First init (empty volume), in order:**
1. `rathena/start.sh` waits for the DB, then checks for the `login` table. If it is missing, it loads the rAthena schema
   from the rAthena source inside the rathena image (`sql-files/main.sql`, `logs.sql`, `web.sql`).
2. It then loads `rathena/sql/seed.sql` (mounted as `/seed.sql`): 2 test accounts and their characters
   (`player`, `bot1`). That file's header describes them. The stack does not use them for the bots.
3. The 4,000 bot personas are **not** loaded by the server. They come from `legion/data/seed.sql` (tracked, 4.8 MB),
   loaded by hand. Its generator is `legion seed` (`legion/main.go`, `legion/README.md` Re-seed section).

**Warning:** `legion seed` **overwrites** the tracked `legion/data/personas.json` and `legion/data/seed.sql`. Do not run
it on a new machine by accident. Load the tracked `seed.sql` into an empty `ragnarok` database instead (fresh route
below).

**Fresh route (no dump), in order, owner's word at each start:**
1. `docker compose up -d db`, and wait for `ro-db` to report `healthy` (section 7, step 2).
2. Start rAthena once, so that `start.sh` imports the schema (the tracked `legion/data/seed.sql` has only `DELETE`,
   `ALTER` and `INSERT` statements and no `CREATE TABLE`, so it cannot run before the schema exists):
   `docker compose up -d rathena`. Wait until the `login` table exists:
   `docker exec ro-db sh -c 'mariadb -u"$MARIADB_USER" -p"$MARIADB_PASSWORD" --skip-ssl ragnarok -e "SELECT COUNT(*) FROM login"'`
3. Load the bot seed from the repo root (WSL, `/mnt/g/RO/legion`). It is the same command as `legion/README.md`
   (Re-seed), with the credentials read from the container's environment instead of typed:
   `docker exec -i ro-db sh -c 'mariadb -u"$MARIADB_USER" -p"$MARIADB_PASSWORD" --skip-ssl ragnarok' < legion/data/seed.sql`
   Do not run `docker compose run --rm legion seed`: that overwrites the tracked personas and seed file.
   The engine services (`legion`, `legion-core`) are not running during this load, so nothing writes to the bot rows.

**What a fresh database gives:** the seed state (the 4,000 bots and their characters at creation, plus the 2 test
accounts). It does not give the live world (zeny, levels, guilds, the market, parties, Ayla's party), which lives only in
the running database. Three bot personas and two account ids in the tracked `seed.sql` do not match the tracked
`personas.json` (bug `newmachine|seed-sql-drift`, section 10). To reproduce the live world, dump the database.

**Dump procedure (written, not run; the owner runs it on the live machine, and only after stopping the writers):**
1. Stop the writers first, as `STATUS.md` ("Stopping") says: `stackctl.py down` (writes `stack.hold`), then the loops,
   then `docker compose stop` for the engine services. Do not stop `db`: `ro-db` stays up, because the dump reads it.
   The dump is taken with the game stopped. rAthena's tables are mostly MyISAM, which `--single-transaction` does not
   snapshot, so the dump is consistent only because nothing writes while it runs.
2. From WSL, in the repo root (WSL path `/mnt/g/RO/legion` on the stock layout), take the dump. The credentials are read
   inside the container from its own environment, so they never appear in the command:
   `docker exec ro-db sh -c 'mariadb-dump -u"$MARIADB_USER" -p"$MARIADB_PASSWORD" --single-transaction --routines ragnarok' > backups/ro-live-<YYYYMMDD-HHMM>.sql`
   If the client refuses the connection, add `--skip-ssl` (the rAthena start script uses it).
   Run it from WSL bash, not PowerShell, so the file is written as plain UTF-8.
3. Check the file: it is not empty, it ends with a "Dump completed" line, and it has the expected table count
   (`grep -c "CREATE TABLE"`).
4. The dump holds account data (including the login table). Treat it as a secret: it stays in `backups/`, which is
   git-ignored, and it is not copied anywhere outside the owner's machines.

**Restore on the new machine (written, not run):**
1. Start only the database, not rAthena: `docker compose up -d db`, and wait for the healthcheck. Owner's word first (G21).
2. Load the dump into the empty `ragnarok` database from WSL:
   `docker exec -i ro-db sh -c 'mariadb -u"$MARIADB_USER" -p"$MARIADB_PASSWORD" --skip-ssl ragnarok' < backups/ro-live-<stamp>.sql`
   The dump contains the schema, so the `login` table exists and `rathena/start.sh` skips its first-run import.
3. Recreate the dashboard's SELECT-only user `dash_ro` (creation statements `not found in repo`; the owner sets the
   grant and the password, and writes the keys into `tools/.state/dashboard.json`, key names only).
4. Copy the runtime state of section 4 into `legion\legion\data\`.
5. Verify the restore by counts, not by content: the number of rows in the `char` table should match the number the
   owner recorded on the old machine (`SELECT COUNT(*) FROM char`).
6. Start the stack only as `STATUS.md` ("How to start") says, on the owner's word.

## 6. Paths: what depends on `G:\RO` and `/mnt/g/RO`

- The stock layout is `G:\RO\legion`. Git Bash sees it as `/g/RO/legion`; WSL sees it as `/mnt/g/RO/legion`.
- Several scripts, hooks and docs name these paths directly. The full list of hits is in
  `docs/NEW-MACHINE-MANIFEST.json` (`known_path_files`), and the clusters are in section 10. They are filed in the bug
  queue, assigned to `followups-batch-2`, and not rewritten here (fix by the fixer, verify live).
- Ports, all on `127.0.0.1` (no host IPs are configured): 3306 MariaDB (optional), 6900 login, 6121 char, 5121 map,
  6953 agency (the login stand-in for the 2021 client), 8090 Legion API, 8091 `legion-core` API, 6061 `legion-core`
  pprof, 6901 X-Kore 2 for Ayla (watch view only), and 8099 dashboard (on the Windows side, `tools/dashboard/dashserve.py`).
  The rAthena address settings are `127.0.0.1` (`rathena/conf/import/char_conf.txt`, `map_conf.txt`, `login_conf.txt`).
- The address of the rAthena container changes when the network is rebuilt. Ayla's OpenKore writes the current one into
  its config at start (`docker-compose.yml`, service `openkore-ayla`). No manual step.

## 7. Build and up order

**Never a plain `docker compose up -d`.** Without a profile it starts `db`, `rathena`, `agency`, `openkore-ayla`,
`legion` and `legion-core`. That starts Ayla and both engines, which G21 and section 8 forbid on a first start. Every
step below is a separate owner decision. Run them from the repo root (WSL: `/mnt/g/RO/legion`). The compose project
is `ro`. Profiles: `openkore` (profile `legacy`), `openkore-qa` (`qa`) and `mastermind` (`fallback`) start only when
their profile is named, and are not used here.

Step 0, hold marker (starts nothing; from `G:\RO\legion` in PowerShell or Git Bash):
```
python tools/system/stackctl.py down
python tools/system/stackctl.py status        # expect stack.hold=True
```

Step 1, build the game server image (no start; needs GitHub for the pinned rAthena commit). Build it before anything
else: the Legion image is built from `ro-rathena:20210107re`, and that is built at the owner's start, not here:
```
docker compose build rathena
```

Step 2, database only:
```
docker compose up -d db
docker inspect --format '{{.State.Health.Status}}' ro-db     # repeat until: healthy
```

Step 3, fill the database, by one of two routes. Both are in section 5: the restore of a dump (restore steps 2 and 3),
or the fresh route (seed steps 2 and 3).

Step 4, game server only (for the restore route; the fresh route already started it in its step 2):
```
docker compose up -d rathena
docker ps --format '{{.Names}}'              # expect exactly: ro-db and ro-rathena
```
`agency` and `openkore-ayla` must not be in that list. Nothing else starts here. Stop at this point.

From here on, the owner orders each start (G21). The full start order is in `STATUS.md`, "How to start", and runs only on
the owner's word. It is not the first start of a new machine, because it starts everything without a profile.

Other points:
- The database goes first and must be healthy before rAthena starts (`depends_on` with `service_healthy`).
- The rAthena image is built before the Legion image, because the Legion Dockerfile copies its game data from
  `ro-rathena:20210107re`. Building rAthena needs network access to GitHub for the pinned commit.
- Recreating rAthena also recreates Ayla's OpenKore and the Legion containers (G15). After the first start, use `legion`
  and `legion-core` with `--no-deps` as the deploy script does (`tools/deploy/deploy.py`).
- Ayla's OpenKore services (`openkore-ayla`, the optional `openkore-qa`) are not started on a new machine until the
  owner orders it (G19 and AY24 in `docs/rules/ayla.md`).
- The build order above is `not verified` on a clean machine. Record what happens on the first build.

## 8. First-run checklist (the owner runs the start on his word)

1. `python tools/system/portcheck.py --base G:/RO` (run from the repo root). Blocking gaps: 0. The output lists every
   missing owner-supplied file by its severity. Client files are `optional`: their absence is not a gap. Inside WSL the
   `wsl` check is shown as `skip`, not a gap.
2. `python tools/system/runsuites.py`. All suites pass; they are offline and run under the G23 tripwire (no live data).
3. `legion\legion\data\stack.hold` exists before any start (step 0 of section 7). Ayla stays held: her OpenKore services are not started, and no
   `ro-openkore-ayla` container is running. The owner decides when she starts (G19, G21).
4. Database: `ro-db` is healthy, and `ragnarok` holds the expected tables (section 5, step 5 for the counts).
5. Dashboard: `tools/dashboard/keep.cmd` (or `python tools/dashboard/dashserve.py --port 8099`) answers on
   `http://127.0.0.1:8099`. The Players, Builds and Market tabs load. Expected with no live database: the offline
   snapshot path (`tools/dashboard/offline.py`) is used; this is `not verified` here, so record what the tabs show.
6. Game login, only after rAthena is up and only with the owner's word: a test account from `rathena/sql/seed.sql`
   logs in on `127.0.0.1:6900` with the owner's client (optional). Nothing else logs in.
7. `legion/data/watchdog.log` and the container logs show no restart loop after a minute.
8. Nothing else starts. Legion (`legion`, `legion-core`) waits for the owner's order part by part (G21).

## 9. If this machine's paths differ

- Run `portcheck.py`. Its `[a]` section lists every new host path (file and line, never the matched text), and
  `known` hits are the filed clusters.
- The simplest route, if the new drive letter differs: map the folder to `G:` with the Windows `subst` command before
  any script runs (`subst G: <folder>`). Not verified here, and not tested at all. `subst` changes the drive letter for
  the Windows tools at once. It does not persist across a reboot, so it must run again at each login (the repo sets
  up no such task). WSL mounts the drive by its Windows letter, so whether WSL sees the mapped folder as `/mnt/g`
  is untested. A different drive letter is the failing case: the scripts hard-code `/mnt/g/RO` and `G:`. Test this first,
  outside the stack.
- The other route is to fix the clusters in section 10. That is a fixer task on the bug queue, verified live.
- Python: the hooks in `.claude/settings.json` name the interpreter `C:/Users/kod/AppData/Local/Programs/Python/Python312/python`.
  On another machine that path must match the new interpreter. This is in the bug queue (`newmachine|paths:claude-hooks`).
- The agent guard reads the repo's own root; it is not configured per machine except in those hooks and the allow list.

## 10. Known path assumptions (filed in the bug queue, all assigned to `followups-batch-2` except the one closed row)

The full file list is `known_path_files` in `docs/NEW-MACHINE-MANIFEST.json`, checked by `portcheck.py`. A new host
path in a file not on that list blocks the check. The clusters:

| cluster | what | bug id | priority |
|---|---|---|---|
| secrets in tracked files | plain-text passwords: `legion/data/personas.json` (password on all 4,000 records) and `username`/`password` keys set in `openkore/control/config.txt` and `openkore-ayla/control/config.txt`. Closed as informational: the owner decided fake-account passwords need no removal | `b1794798034yoe` | low (closed) |
| agent hooks | `.claude/settings.json` (Python 3.12 path under `C:/Users/kod`, `G:/RO/legion`); `tools/system/mainguard.allow.json` (per-user memory and Temp paths, `repo_root`) | `b1794798034vuz` | high |
| deploy path | `tools/deploy/rathena_recreate.sh`, `guard.sh`, `deploy.py` (`/mnt/g/RO/legion`, the Ayla status file) | `b1794798034kch` | high |
| launchers and watch scripts | `tools/rogui/*.sh`, `tools/watch/*.sh`, `tools/sentinel.py`, `tools/lifecycle.sh`, `tools/mem.py`, `openkore-qa/qa.sh`, `openkore-qa/kafratest.sh` | `b1794798034uzu` | normal |
| other scripts and docs | `tools/dashboard/buildexport.py` (a `G:\RO\ClientEN` client default), `tools/mindgate.py`, `tools/rogui/tell-ayla.cmd`, `.claude/agents/*.md` | `b179479813185g` | low |
| QA scratch | `tools/qa/*.sh` (a Python 3.12 path under `C:/Users/kod` and an old scratchpad) | `b1794798034iws` | low |
| test fixtures | `legion/gearsuspect_offline_test.go` reads a git-ignored `dbsnap` snapshot by absolute path and skips nothing when it is absent | `b17947980343ta` | low |
| seed drift | `legion/data/seed.sql` does not match `legion/data/personas.json` (3 names, 2 ids) | `b1794798034rxp` | normal |

Not filed (by design): `tools/puppet/dispatch.py` translates `/mnt/g/RO` and `G:/RO` on purpose; `constcheck.py` and
`mainguard.py` know the `G:` drive by design. The scanner also skips `docs/` and `RESTART-NOTES/` (prose).

## 11. What is not in git, and what is unknown

Not in git (from `git status --ignored`, summarised): `.env`; `backups/`; `legion/data/` runtime state (about 70
entries, section 4); `tools/puppet/` runtime (32 entries); `tools/.state/` (dashboard login, snapshot caches); `logs/`
and the `*/logs/` folders; the root demo screenshots (`/*.png`); the Go binary `legion/legion`; the git-ignored
OpenKore and Ayla control runtime files.

Unknown or `not found in repo`:
- The git remote URL for the clone.
- The Docker version and the install method (Engine in WSL is what the docs say; no version pin).
- How the `dash_ro` database user was created (statements, grants).
- How the Wine 9.0 prefix `~/.wine-ro` and the copy `~/roclient` were set up, step by step. The repo has the launcher
  and the patch, not the prefix setup.
- The writer of `openkore-ayla/logs/items_lut.tsv` (OpenKore's code, outside the repo).
- The owner's client download source for `Ragnarok_210607.zip` beyond the file name.
- Whether a rAthena and an OpenKore image build succeed on a clean machine (the pinned commits are fetched from GitHub;
  not built here).
- Whether the dashboard's offline mode shows the Market and Builds tabs with no database (not run here).
- Whether `subst` mapping is acceptable to every script (not tested).
