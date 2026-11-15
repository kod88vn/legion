# Text for `G:\RO\CLAUDE.md` (outside the repo)

Copy everything between the two lines into `G:\RO\CLAUDE.md` after the merge into `main` (the old file still names
`server/`, which is now `legion/`). Only the core is imported; the topic files are read on demand (the core has the map).

---8<---
# G:\RO - local Ragnarok Online AI world

The project lives in `legion/` (git repository, branch `main`). **Start every session with `legion/STATUS.md`**: it says
what runs and what is on hold. Nothing is started (containers, servers, loops, agents) until the owner says so.

Standing rules and hard gates (always loaded):

@legion/RULES.md

Where to read more is in the "What to read for which task" table of that file: topic rules in `legion/docs/rules/`,
class and build rules in `legion/docs/classes/`, engineering handover in `legion/HANDOVER.md` and `legion/docs/handover/`,
guide for in-world agents in `legion/AGENTS.md`.

When the owner states a new rule ("must", "always", "never", "should", "apply to all ..."), in the same turn, without
waiting to be asked: add one short plain line (ID, statement, date; no quote of the owner) to the right file under
`legion/docs/rules/` (class- or build-specific rules under `legion/docs/classes/`), put the owner's words with the date in
`legion/docs/rules/archive.md` under the same ID, and add a row to `legion/docs/rules/coverage.md`. A new hard gate also
gets a G ID in `legion/docs/rules/gates.md` and a line in `legion/RULES.md`; then run
`python legion/tools/system/gatecheck.py --coverage`.
---8<---
