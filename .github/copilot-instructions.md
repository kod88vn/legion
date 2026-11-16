# GitHub Copilot entry point

This repository (git root `legion/`, branch `main`) is a private, local Ragnarok Online world: a Go bot engine (Legion)
runs the bot players, and AI puppet masters and system agents steer and keep it running. The goal is a world that runs
without AI, where only the puppet masters run, once a day. The live numbers are in `STATUS.md`, not here.

Read these in order before any change. Each is the only copy of its text; do not copy from them.

1. `CONSTITUTION.md`, in full, first. It is the highest law of the project and nothing overrides it.
2. `RULES.md`: the owner's standing orders and the hard gates (G01 to G25). Topic files are in `docs/rules/`.
3. `STATUS.md`: what runs, what is on hold, and what may be started. Nothing is started, loaded or deployed without the
   owner (G21).
4. `HANDOVER.md`: how the system is wired; its index points to `docs/handover/`.
5. `AGENTS.md`: the guide for the in-world puppet masters and the `legionctl` commands.
6. `CLASSES.md`: class and build rules; the detail is in `docs/classes/`.

## Roles (Constitution Article 1 and 2.5 to 2.6)
- Main session: listens to the owner, keeps the bug queue and the status note, and assigns every task with a brief. It
  never writes or edits code, configuration or data, never commits, builds, deploys, starts or stops anything.
- Fixers: one git worktree per fixer under `G:\RO\legion-wt\<name>`, on its own branch `fix/<name>` from `main`. Haiku
  implements (4.1). A Sonnet review is required before any change is merged or loaded (4.2).
- Deploys: only the deploy script in `tools/deploy/`, run by an assigned deploy agent (G15 to G17, G24). Working agents
  report "built" and never deploy.
- Puppet masters act inside the world through `python tools/legionctl.py`: guild leaders, class and build, chat. They
  steer by priorities and words and never drive a character (2.5, G18). The one-shot briefs are in `tools/puppet/briefs/`.
- The market is an algorithm engine, not an agent (2.6, EC46): it calculates, feeds the guild leaders and advises the
  farmers. The farmers' own code decides.
- System agents and scripts (2.6): watchdog, sentinel, party watcher, QA, analyst, fixers, deploy.

## Tests and checks (run from the repository root)
- `bash tools/dev/gocheck.sh`: Go vet, build and tests, inside WSL. `--fast` runs vet and build only. Exit 2 means no
  toolchain was found: that is not a pass (G24 applies to the containers it may stop).
- `python -m unittest discover -s tests/system -p "test_*.py"`: the system suites. Other Python folders are run the same
  way with their own `-s` path.
- `python tools/system/gatecheck.py --coverage`: gate register, rule coverage and the constitution check (`--no-suites`
  skips the suites).
- `python tools/system/constcheck.py`: the constitution is in every prompt, once (PR42); `--strict` also fails the
  uncovered sources.
- `python -m unittest discover -s tests/system -p "test_copilot_takeover.py"`: this entry point and its pointers (PR48).

## Hard rules
Apply the gates by ID from `RULES.md`. The ones that most often stop a change: G01, G05, G13, G15, G16, G17, G21, G22,
G23, G25. Live data is never touched by tests (G23). Every change is verified with evidence before it is reported (3.6).
