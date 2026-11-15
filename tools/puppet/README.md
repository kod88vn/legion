# tools/puppet - puppet-master harness

`dispatch.py` is the only long-running piece (no AI itself). It wakes one-shot Claude agents ("puppet masters") for
guild leaders (guild01-10), market, chat, build/class and Ayla, hands each a short briefing built from the engine API,
and logs every call. Design: `docs/design/puppet.md`.

## Use (nothing here is started by itself)
```
python tools/puppet/dispatch.py --list                  # roles, models, intervals, budget caps
python tools/puppet/dispatch.py --dry-run [--roles a,b] # print the exact command + briefing, spends nothing
python tools/puppet/dispatch.py --once guild03          # one real call now (spends tokens)
python tools/puppet/dispatch.py                         # the loop (start it only when the owner says so)
```
Stop: Ctrl+C or kill (heartbeat state becomes `stopped`). Pause without stopping: create `legion/data/puppet.pause`
(`tools/system/stackctl.py down`, which writes `stack.hold`, pauses it too).
Wake the build/class master: `touch legion/data/build.wake` (or an owner order to mind `build`).
Files: `legion/data/puppet_calls.jsonl` (call log), `puppet_heartbeat.json` (each loop, details) and
`dispatch.heartbeat` (epoch; the session gate and the sentinel read it), `bugqueue.jsonl` (bug/neo
queue written by `legionctl bug|neo`), `tools/puppet/notes/<role>.md` (each mind's notes, git-ignored).

## Layout
- `dispatch.py` schedule, triggers, budgets, CLI call, log, heartbeat. `briefing.py` the per-role assemblers.
- `briefs/_common.md` + `briefs/<role>.md` the system prompts (in-world rules only). `workdir/CLAUDE.md` tiny rules file
  for the agent's cwd (the CLI also runs with `--safe-mode`, so the project's big CLAUDE.md files are not loaded).
- `tests/test_puppet.py`: `python -m unittest discover -s tools/puppet/tests` (no server, no tokens).

Interpreter: the minds run `python <legionctl.py>` through Bash; pass `--python` if `python` is not the right name on
PATH for the agent's shell.
