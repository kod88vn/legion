# Running the tests

One place for the commands. Everything runs offline: temp folders, no Docker, no live data (rule G23).

## Commands

| what | command | when |
|---|---|---|
| every Python suite, once, under the tripwire | `python tools/system/runsuites.py` | merge and deploy gate (full) |
| the suites a change touches, plus the core | `python tools/system/runsuites.py --changed` | reviewer's fast path (`--base REF`, default `main`) |
| full Python suites plus the full Go tests | `python tools/system/runsuites.py --go` | before a merge that touches Go |
| one suite | `python -m unittest discover -s tests/system -p "test_*.py"` | while working on that suite |
| list the suite folders | `python tools/system/runsuites.py --list` | |
| the rule gates (also runs every suite once) | `python tools/system/gatecheck.py` | rule edits; `--no-suites` skips the suites |
| Go (vet, build, tests) | `bash tools/dev/gocheck.sh` (`--fast`: vet and build only) | Go changes |

`--changed` runs `tests/system` (the core) and every suite whose files mention a changed file's name. A change to a
shared module (`tests/testenv.py`, `tools/system/common.py`, `runsuites.py`, `gatecheck.py`) runs every suite. A
changed Go file runs `gocheck.sh --fast`. `--changed` is a fast partial check; the full run is the gate.

Options: `--workers N` (default min(4, CPUs), env `LEGION_SUITE_WORKERS`), `--timeout S` (default 600 s per suite,
env `LEGION_SUITE_TIMEOUT`).

## What the runner guarantees

- Each suite runs in its own child process, at most `--workers` at a time, with a hard timeout. A child that
  hangs is killed with its whole process tree and reported as `TIMEOUT` with the tail of its output. A timeout is a
  failure, never a pass and never a silent stall.
- The G23 tripwire (`tools/system/tripwire/sitecustomize.py`, an audit hook): in every process of a run, a file under
  `legion/data` or `openkore-ayla/logs` cannot be opened for writing, renamed, removed or created. A read is allowed
  only for git-tracked config files in those folders. A test that touches a live lock or state file fails on the spot,
  including a grandchild process that drops `LEGION_TEST`.
- `LEGION_TEST=1` and `LEGION_DATA` unset in every child, so `common.data_dir()` also refuses the live folder.
- The guard test (`tests/system/test_livedata_guard.py`) runs the same check. Inside a runner run it is skipped on
  purpose (`LEGION_GUARD_CHILD=1` is set on every child): the runner's run is the guard's run, so the suites are not
  run twice. Run on its own, the guard runs the runner's check.
- Go: `gocheck.sh` takes its source from its own argument (or the folder it sits in), passes it to WSL inside the
  command string, copies the tree to its own temp folder, and prints the source path and git HEAD at the start and at
  the end. A HEAD that changes during the run fails the check.

## Timings

See `docs/reports/test-speed-20261115.md` (before and after, with the machine load).

## Line endings

`.gitattributes` keeps `*.sh` at LF in the working copy, so a Windows clone with `core.autocrlf=true` runs the shell
scripts. Re-check after a clone: `git ls-files --eol tools/dev/gocheck.sh` should show `w/lf`.
