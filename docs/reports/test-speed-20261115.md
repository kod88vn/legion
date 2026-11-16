# Test speed: measurements, changes and proof (2026-11-15)

Branch `fix/test-speed` (worktree `G:\RO\legion-wt\test-speed`), from `main` at 060f55b. Branch tip: see the handback.
Nothing deployed, merged or pushed. A Sonnet review is still required before merge (Constitution 4.2).

## Machine load

Other agents ran their own test suites and gatecheck on this machine during the measurements (their Python processes
were in the process list). Timings are noisy: read the ratios, not the absolute seconds. The "before" run was sequential
and the "after" runs were parallel, at different load levels.

## Before (main 060f55b, one run, sequential, one child per suite)

Each suite as a child process with `LEGION_TEST=1` and `LEGION_GUARD_CHILD=1` (so the guard did not re-run the suites):

| suite | tests | seconds |
|---|---|---|
| tests/system | 403 | 53.8 |
| tests/dashboard | 64 | 88.3 |
| tests/ayla | 3 | 0.2 |
| tests/tools | 57 | 2.8 |
| tools/puppet/tests | 79 | 23.3 |
| **sum** | **606** | **168.4** |

Slowest single tests (in-process timing): `tools/puppet/tests test_puppet.CommandTests.test_dry_run_spends_nothing` 20.8 s;
`tests/system test_watchdog.TestWatchdogPass.test_own_heartbeat_written_by_main` 17.6 s;
`tests/dashboard test_dashserve.OwnerToolEngineDown.test_read_only_when_down` 7.7 s;
`test_dashserve.StackStopped.test_post_fails_cleanly` 5.6 s; `test_dashserve.StackUp.test_live_then_saved` 4.1 s;
the rest of `test_dashserve` takes 2 to 4 s each (fake engine servers). Full per-test data: `scratchpad/timing/before.json`.

Before, the livedata guard (`tests/system test_livedata_guard`) ran all five suites again, serially, inside `tests/system`,
with a 900 s timeout per suite and no tripwire on grandchildren. gatecheck runs `tests/system` (which contains the guard)
and then every other suite, so the suites ran twice or more per gatecheck. Not measured as one number; the duplicate is
about one more sequential pass (the 168 s sum). This is an estimate.

Go (`tools/dev/gocheck.sh`, before): `--fast` 11 s; full 53 s (`go test` 41.6 s, 566 pass, 28 skip, one package).

## After (this branch)

| run | what | seconds |
|---|---|---|
| `runsuites.py` (full, 3 suites in parallel, tripwire on) | 5 suites, 0 failed | **142.2** total |
| per suite inside that run | tests/dashboard 87.0 s (64); tests/system 30.1 s (413); tools/puppet/tests 22.5 s (79); tests/tools 2.2 s (57); tests/ayla 0.2 s (3) | |
| `gatecheck.py` (all suites, once) | exit 0, OK: 25 gates, fields 0 mismatches, every suite passes | **148** |
| Go `--fast` | gofmt, vet, build | **8** |
| Go full | 566 pass, 28 skip, 0 fail (`go test` 37.0 s) | **49** |
| `runsuites.py --changed` (this branch vs `main`) | the diff includes the shared runner files, so every suite ran ("shared module changed"); Go `--fast` ran too | **152.5** total (Go fast 10.4) |

The tests/system child took 54 s before and 30 s after. Most of that difference is load (the before run was sequential under
other agents' load); the test code did not get faster. The reduction is structural: no suite runs twice, the suites run in
parallel, and a stalled child cannot hold the run (it is killed at its timeout).

Test counts: `tests/system` 403 to 413 (10 new in `test_runsuites.py`). In a runner run tests/system shows 2 skips, because
the guard test skips itself inside the runner (item 3 below); a plain run shows 1. Go skips: 28, unchanged.

## What changed

1. `tools/system/runsuites.py` (new): one suite discovery (`suite_dirs`) and one runner. `run_suite` runs a suite in a child
   process under the G23 tripwire environment with a hard timeout (600 s; `LEGION_SUITE_TIMEOUT`). `run_all` runs them with
   a bounded number of parallel children (`LEGION_SUITE_WORKERS`, default min(4, CPUs)). `run_bounded` sends output to temp
   files, not pipes, and kills the whole process tree on timeout (`taskkill /T` on Windows, a process group on POSIX). A
   timeout is reported as `suite X: TIMEOUT after N s (child killed)` with the tail of the output, and fails the run.
2. `tools/system/gatecheck.py`: suite discovery and running come from `runsuites` (one copy). Each suite runs once.
3. `tests/system/test_livedata_guard.py`: the suite check uses `runsuites` (same discovery, timeout and tripwire
   environment; parallel). Every known suite must be discovered. The guard test is skipped only inside a runner run
   (`LEGION_GUARD_CHILD=1` on every child): the runner's run is the guard's run. Run on its own, the guard performs the check.
4. The G23 audit tripwire (`tools/system/tripwire/sitecustomize.py`, below).
5. `testenv.isolate()` added to nine test files that did not import it: `tests/dashboard/test_builds.py`,
   `test_buildexport.py`; `tests/system/test_ayla_engine_migrate.py`, `test_fieldcheck.py`, `test_gatecheck_suites.py`;
   `tests/tools/test_buildgear_realism.py`, `test_buildmilestones.py`, `test_gearalts.py`, `test_wrongbuilds_replay.py`.
   In `test_builds.py` and `test_buildexport.py` the exporter's two live inputs point at a nonexistent temp file
   (`buildexport.ENGINE_GEAR_JSON`, `buildexport.ITEMS_TSV`); tests that want those inputs still pass their own paths.
6. `tools/dashboard/buildexport.py`: `lookup_names` and `export` read their default paths at call time (they captured
   `ITEMS_TSV` and `SKILLS_TSV` as default arguments, so a test could not redirect them).
7. `tools/dev/gocheck.sh` and `tools/dev/tests/gocheck_test.sh`: see Go below.
8. `.gitattributes`: `*.sh text eol=lf`.
9. `docs/handover/testing.md` (the commands), and one pointer line in `HANDOVER.md`.
10. `tests/system/test_system.py`: `test_imports_without_legion_data` still drops `LEGION_TEST` (it only imports the module
    and prints a constant, touching no file). The audit tripwire covers any file access in that child.

## The livedata lock question (coordinator's item)

The merge agents saw `dashboard/dispatcher/partycheck/sentinel/unwall: another copy holds ... ; not starting` in tests/system
runs. Finding: those lines come from tests that deliberately check a refusal. Each creates its own lock in its own temp data
folder and asserts the refusal (`test_watchdog_safety.TestSingleInstances`, `test_system`, `test_dashserve`). The text says
`legion/data/` because the message is hard-coded; the folder is the test's temp folder. Evidence: a plain run of tests/system
(no runner, no tripwire) printed 5 such lines, all from those tests; the tripwire runs found zero live touches from them.
The wording was left as it is.

Two real reach-outs to live data were found and fixed:
- `tests/dashboard` read `legion/data/buildmilestones_gear.json` (the engine's file) by default, and
  `openkore-ayla/logs/items_lut.tsv` (Ayla's logs) by default. Fixed as in item 5 and item 6.
- `tests/system` `test_system.py` removed `LEGION_TEST` from a child's environment. The child now also runs under the audit
  tripwire, which covers it.

## The G23 audit tripwire (extension for lock and state files)

`tools/system/tripwire/sitecustomize.py` is an audit hook (`sys.addaudithook`). The runner puts it on `PYTHONPATH` of every
child, and sets `LEGION_TRIPWIRE_LIVE` (the live `legion/data` and `openkore-ayla/logs` of this checkout) and
`LEGION_TRIPWIRE_READ` (the git-tracked files in those folders, which may be read: committed config). In every process of a
run, opening a live-folder file for writing or creating it, renaming, removing, or reading an untracked live file raises
`G23 tripwire: a test opened ... under the live folder ...`. It works in grandchildren that drop `LEGION_TEST`.

Probe (child process): a write to `legion/data/tripwire-probe.lock` was refused; a read of tracked `tuning.json` passed; a read
of an untracked `not-tracked.state` was refused; no probe file was left behind.

Limit: a process started with an environment that drops `PYTHONPATH` is outside the hook. Tests build children from
`os.environ`, which keeps it; the runner and the guard do the same.

## Mutation proof

A probe test was added to `tests/tools` (temporary, removed after the run). It did two things: `common.data_dir()` with
`LEGION_DATA` unset and `LEGION_TEST=1`, and a direct write to `legion/data/probe.lock`.

`python -m unittest discover -s tests/system -p test_livedata_guard.py` with the probe present:

```
FAIL: test_every_suite_exits_zero_under_the_tripwire
suite tests/tools: Ran 59 tests, FAILED (errors=1)
... G23 tripwire: a test opened ...\legion\data\probe.lock under the live folder ... (open for write) ...
Ran 3 tests ... FAILED (failures=1)
```

The direct write was refused. The resolver plant did not fire in that process: an earlier test module in the same process had
already set `LEGION_DATA` to a temp folder with `testenv.isolate()`. The resolver path is covered by the guard's own
`PlantedLeak` test, which passed in every run. The probe was removed; `legion/data/probe.lock` does not exist.

## Runner results

- Full (`python tools/system/runsuites.py`, tripwire on): 5 suites, 0 failed, 142.2 s. Earlier runs in this session found
  real problems (the tripwire caught live-folder reads in tests/dashboard, and a syntax error in the guard file), which were
  fixed before the final run.
- Plain `python -m unittest discover` (no runner, no tripwire): tests/system 413 tests OK (171 s under load); tests/dashboard
  64 OK (85.5 s).
- `tests/system/test_runsuites.py`: 10 tests OK (a hang becomes TIMEOUT and the tree is killed; a live-folder write is refused
  in a suite; a grandchild without test variables is refused; committed config is readable; `--changed` selection rules).

## Fast path

`python tools/system/runsuites.py --changed` runs `tests/system` plus every suite whose files mention a changed file's name.
A change to `tests/testenv.py`, `tools/system/common.py`, `runsuites.py` or `gatecheck.py` runs every suite. Docs-only changes
run the core only. A changed Go file runs `gocheck.sh --fast`. `--go` adds the full Go tests. The full run is the merge gate.

## Go (gocheck.sh)

- The coordinator's item: a run printed another worktree's path. Not reproduced: two concurrent `--fast` runs on two worktrees
  each printed their own path (`test-speed` and `batch-2`). Defects found on the way: the old script passed the source path to
  WSL through `WSLENV`, and a single quote inside its inner script (`'$WSL_RUN'`) closed that script early, so the rest ran in
  the outer shell. I did not find a path by which one run reads another's tree. Unverified: the reported run may have been given
  an argument that pointed elsewhere; the new output names the resolved source, so that case is now visible.
- Now: the path travels in the WSL command string (quoted with `printf %q`), not in `WSLENV`. The script prints the source path
  (as WSL resolves it) and the git HEAD at the start and at the end, and fails if HEAD changed during the run. The HEAD comes
  from this shell's git, because WSL's git cannot read a worktree whose `.git` file holds a `G:/` path (verified: `fatal: not a
  git repository` from WSL). Git Bash paths such as `/tmp/x` are converted with `cygpath -w` before WSL sees them. Each run
  writes its own log in WSL (`~/gocheck-last.XXXXXX.log`). The Go build cache is shared; that is safe (content-addressed).
- `tools/dev/tests/gocheck_test.sh` has a concurrency case: two temp Go modules, two runs at the same time, each must report
  its own path and its own HEAD sha and not the other's. Result: 0 failures (6 checks).

## Line endings

`.gitattributes`: `*.sh text eol=lf`. In a clone made with `core.autocrlf=true` from the committed HEAD (which had no rule yet),
`git ls-files --eol` showed `w/crlf` for `tools/dev/gocheck.sh`. After the rule was applied in that clone, it showed `w/lf`,
and `gocheck_test.sh` passed there (0 failures). The other `.sh`-using tests (`test_watchdog`, `test_watchdog_safety`) were not
re-run in a CRLF clone.

## Not done or not verified

- A Sonnet review of this branch (required before merge).
- Speed-ups inside the slow tests (`test_puppet` dry run, `test_watchdog` heartbeat): not changed.
- The gatecheck `before` time is an estimate, not a measurement.
- Machine load was not controlled.
