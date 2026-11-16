# Handover: current state and index

Engineering handover for an AI taking over. What runs and what is on hold: `STATUS.md`. This page is the index; the long sections are in `docs/handover/` and were
moved unchanged (each file's first line says which lines of the old `HANDOVER.md` it holds; the old file is in git at
commit 63ca923). Engineering history is not rewritten.

## Current state (2026-11-15, redesign in progress; the live status is `STATUS.md`)
- The harness is `G:\RO\legion` (git, branch `main`); the Go engine is `G:\RO\legion\legion`. The redesign work packages
  are in `docs/design/REDESIGN.md`. Nothing is started without the owner (G21).
- Ayla's target is the Legion engine plan (AY24, `docs/rules/ayla.md`). Until the owner orders the cut-over ("start Ayla
  on the engine"), the OpenKore-era sections of `docs/handover/` describe her actual current setup and stay current for
  her; after the cut-over they become reference.
- The market is an algorithm engine, not an agent (EC46, `docs/rules/economy.md`). The farmers take its advice in their own code.
- The dashboard is `tools/dashboard/dashserve.py` on `http://127.0.0.1:8099` (kept on by `tools/dashboard/keep.cmd`, DB06);
  it reads ro-db as `dash_ro`. The Legion API it reads is on port 8090 and is stopped when the engine is down. What runs
  now is in `STATUS.md`.
- GitHub Copilot can take over from repository access alone: `.github/copilot-instructions.md` is the entry point (PR48).
- Old session start-up (sections 0.1 to 0.6: loops, 15 minds, standing agents) is superseded by RULES.md and the redesign; it
  stays as reference for how things are wired.
- Environment: Windows host plus WSL Ubuntu-24.04 with Docker inside WSL. From Git Bash set `MSYS_NO_PATHCONV=1`.
  No Go toolchain on Windows; see `docs/design/toolchain.md` for `tools/dev/gocheck.sh`.

## Index of `docs/handover/`
Rows marked **REFERENCE** describe the OpenKore era or the 15-mind era. They are kept unchanged (nothing deleted) and each has a banner at its top; for how things run now read `STATUS.md`, `RULES.md` and the redesign (`docs/design/REDESIGN.md`).

| file | old section | what |
|---|---|---|
| `startup-old.md` **REFERENCE** | 0 | old session start-up, standing agents, deploy scripts, pending items (0.6) |
| `owner-wants-layout.md` **REFERENCE** | 1, 2 | what the owner wants, owner rules, folder layout |
| `operate.md` **REFERENCE** | 3 | how to operate: WSL, docker, deploys, restarts |
| `ayla-live-view.md` | 4 | Ayla's current OpenKore setup until the owner orders the cut-over (AY24), reference after it: the live watch view, plugins, gotchas |
| `legion-overview.md` **REFERENCE** | 5 | what the Legion engine does, general bot rules added 2026-11-13 |
| `state-and-loops.md` **REFERENCE** | 6, 6b, 6c | state at 2026-11-13, the minds gate, the self-improvement loop |
| `guild-leaders.md` | 6d | guild leaders: always online, levelling, leaderboard |
| `economy.md` **REFERENCE** | 6e | economy, vending, alchemy, market batches 1 to 18 |
| `builds.md` | 6f to 6f4 | wrong builds, AoE casters, build review per class, trans cap |
| `farming-town.md` | 6g | farming time and town trips |
| `ayla-party-guild.md` | 6h | Ayla's current party of 7 and guild Starveil until the owner orders the cut-over (AY24), reference after it |
| `social.md` **REFERENCE** | 6i to 6i3 | friendship, guild loyalty, name filter |
| `qa-gm.md` **REFERENCE** | 6j | travel and services QA with the GM test character |
| `open-issues.md` **REFERENCE** | 7, 8 | open issues, quick checks, background loops |
| `testing.md` | | how to run the tests: `runsuites.py` (full, `--changed`, `--go`), timeouts, the G23 tripwire, gocheck |

## Other references
- Classes and builds: `docs/classes/*`. Lessons: `legion/data/lessons.md` via `docs/rules/lessons-index.md`.
- Engine internals and HTTP API: `legion/README.md`. Server and client details: `README.md`.
