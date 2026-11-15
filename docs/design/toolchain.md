# Go toolchain without Docker

Work package T, 2026-11-14. Lets agents compile and unit-test the engine (`legion/`, module `legion`, go 1.22,
stdlib only) with no containers.

## Installed
- Go **1.22.12 linux/amd64**, unpacked from the go.dev tarball into WSL (Ubuntu-24.04) `~/go-toolchain/go`
  (`~/go-toolchain/go/bin/go version`). No sudo. Build cache: `~/.cache/go-build-gocheck`.

## Use
```
bash /g/RO/legion/tools/dev/gocheck.sh [--fast] [-run <regex>] [engine-dir]
```
- `engine-dir`: Windows (`G:\RO\legion-wt\x\legion`) or `/g/...` path; default is the main checkout's `legion/`.
  For a worktree pass `/g/RO/legion-wt/<name>/legion`.
- It copies `go.mod`, `*.go`, `*.html`, `recvpackets.txt` into a fresh WSL-native temp dir (deleted afterwards),
  then runs `gofmt -l`, `go vet ./...`, `go test -count=1 -v ./...` with `GOTOOLCHAIN=local GOPROXY=off`.
- `--fast`: gofmt + vet + build only. `-run RE`: passed to `go test`.
- Output: gofmt file list (informational only, never fails), vet ok/FAIL, pass/fail/skip counts, failing test names,
  skipped test names. Full test log stays in WSL at `~/gocheck-last-test.log`.
- Exit code 1 on vet/build failure or any failing test not listed in `tools/dev/baseline-failures.txt` (those are
  printed as "baseline failures (ignored)"). Exit 2 if the dir has no go.mod.
- Tests that read data files at runtime are not copied; they skip (see below).
- Never starts docker, containers or servers; no network.

## Baseline on main (2026-11-14)
- gofmt: 14 files unformatted (ai, alchemy, bossnote, danger, economy, junkgear, kafra, lookup, mailtrade,
  opportunity, party, town, vend, vendbuy). Informational; do not mass-reformat (noisy diffs across branches).
- vet: ok. build: ok.
- tests: **168 pass, 0 fail, 23 skip**. Time: about 9 s total (`--fast` about 4 s; first run also fills the cache).
- Baseline failures: none, so `baseline-failures.txt` is empty.
- Skips (all need data that only exists on the live server / a rathena copy; none are failures):
  - `*Live` tests (TestBossCatalogLive, TestKafraWalkLive, TestKafraChainLive, TestKafraNoWingBackLive,
    TestKafraPayonDungeonLive, TestKafraMoroccLandingLive, TestKafraYunoLive, TestKafraWaitInTownLive,
    TestLookupLive, TestMapAccessLive, TestCrossingDangerLive, TestMVPMapsLive, TestPartyFitLive, TestZenyHourLive,
    TestKafraViaMatchesKafraPlan, ...): skip unless `LEGION_RATHENA` points to a copy of `/rathena`
    (map cache, mob/item db, NPC scripts); some also need `LEGION_PARTIES`, `LEGION_POWERS`, `LEGION_BOTS`.
  - Offline measurement tests (TestZZDumpMapLv, TestZZSweep, TestZZWhy, TestZZAggroTop): same, "offline measurement".
  - TestWarpGuardStepBesideDoor, TestRouteEndsInShopRoom, TestShopExitFirstStepAwayFromDoor,
    TestWarpLoopGeffenNorthExitIsProgress: skip on "maps/items missing / no rAthena map cache" (need map_cache.dat).
- To run those too, copy the needed rathena files somewhere and set `LEGION_RATHENA`; gocheck.sh does not do that.
