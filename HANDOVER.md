# Handover: current state and index

Engineering handover for an AI taking over. Read `STATUS.md` first (what runs, what is on hold, first start after the move),
then `RULES.md` (core rules and hard gates). This page is the index; the long sections are in `docs/handover/` and were
moved unchanged (each file's first line says which lines of the old `HANDOVER.md` it holds; the old file is in git at
commit 63ca923). Engineering history is not rewritten.

## Current state (2026-11-14, redesign in progress)
- Nothing is running on purpose (owner's hold). The stack is stopped with `docker compose stop`; containers, images and the
  database volume `ro_dbdata` still exist. First start only on the owner's word (STATUS.md).
- The harness is `G:\RO\legion` (git, branch `main`); the Go engine is `G:\RO\legion\legion` (frozen tree of 2026-11-14,
  not deployed since 14:55). The redesign work packages are in `docs/design/REDESIGN.md`.
- Old session start-up (sections 0.1 to 0.6: loops, 15 minds, standing agents) is superseded by RULES.md and the redesign; it
  stays as reference for how things are wired.
- Environment: Windows host plus WSL Ubuntu-24.04 with Docker inside WSL. From Git Bash set `MSYS_NO_PATHCONV=1`.
  No Go toolchain on Windows; see `docs/design/toolchain.md` for `tools/dev/gocheck.sh`.

## Index of `docs/handover/`
| file | old section | what |
|---|---|---|
| `startup-old.md` | 0 | old session start-up, standing agents, deploy scripts, pending items (0.6) |
| `owner-wants-layout.md` | 1, 2 | what the owner wants, owner rules, folder layout |
| `operate.md` | 3 | how to operate: WSL, docker, deploys, restarts |
| `ayla-live-view.md` | 4 | Ayla's OpenKore, the live watch view, plugins, gotchas |
| `legion-overview.md` | 5 | what the Legion engine does, general bot rules added 2026-11-13 |
| `state-and-loops.md` | 6, 6b, 6c | state at 2026-11-13, the minds gate, the self-improvement loop |
| `guild-leaders.md` | 6d | guild leaders: always online, levelling, leaderboard |
| `economy.md` | 6e | economy, vending, alchemy, market batches 1 to 18 |
| `builds.md` | 6f to 6f4 | wrong builds, AoE casters, build review per class, trans cap |
| `farming-town.md` | 6g | farming time and town trips |
| `ayla-party-guild.md` | 6h | Ayla's party of 7 and her guild Starveil |
| `social.md` | 6i to 6i3 | friendship, guild loyalty, name filter |
| `qa-gm.md` | 6j | travel and services QA with the GM test character |
| `open-issues.md` | 7, 8 | open issues, quick checks, background loops |

## Other references
- Rules: `RULES.md`, `docs/rules/*` (topics, `gates.md`, `coverage.md`, `archive.md`, `lessons-index.md`).
- Classes and builds: `docs/classes/*`. Lessons: `legion/data/lessons.md` via `docs/rules/lessons-index.md`.
- Engine internals and HTTP API: `legion/README.md`. Server and client details: `README.md`.
