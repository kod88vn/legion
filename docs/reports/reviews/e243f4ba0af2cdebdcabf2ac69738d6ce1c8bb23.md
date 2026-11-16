# Review of fix/market-leader-feed e243f4ba0af2cdebdcabf2ac69738d6ce1c8bb23

Reviewer: Sonnet. Scope: WP5 (guild leaders read the market feed, `guildmarketfeed.go`) and WP6 step 1 (market direction
channel retired). Rules: EC43, EC46, EC48; Constitution 2.5, 2.6, 2.7, 2.9; design market-engine.md 4.2-4.3.

## Verification run
- Scratch merge: detached worktree `G:\RO\legion-wt\scratch-review-mlf`, main 2eb4b9d + e243f4ba (merge eb96892; the branch is
  based on 91a1c45, main has since gained only review commits, so the merge is clean). gocheck (explicit /g/ path, first
  line `src: /mnt/g/RO/legion-wt/scratch-review-mlf/legion` = my tree): vet ok, `pass=599 fail=0 skip=28`. The gofmt
  "UNFORMATTED (not fatal)" list is existing files; the new files are not on it.
- Puppet suite on the scratch merge: 90 tests OK in 24.6 s.
- `test_dry_run_spends_nothing`: passes in 22.7 s on main and 22.7 s on the merge, so the 60 s timeout the fixer saw is
  pre-existing, not caused by this branch. Cause: the test points LEGION_API at 127.0.0.1:1 and the dispatcher's briefing
  makes several GETs; on Windows a refused loopback connect is slow (about 2 s per attempt, retried), so the dry run needs
  ~22 s idle and passes 60 s only with headroom. Under load (parallel gocheck/agents) it can cross 60 s. Same family as the
  comment in `test_cli_bug_and_neo`; the fix there was a local 503 stub. Follow-up: use a local stub server here too.

## Checks the brief asked for
- Flag off is the identical path. `dutyAutopilotRound`: the guilds are iterated in sorted order; `autopilotDirs` has no
  cross-guild state or randomness and results go into a map `res`, so order cannot change results. `gmfApply` returns the
  same slice when the flag is off or the feed is nil; `gmfFeed()` returns nil first when the flag is off (no lock taken).
  `dutyMaps`: `maps` is empty at the switch, so with flag off `ItemID==0` runs the same `MoneyFarmGoal` branch as before
  and `ItemID>0` never falls back (`feedFallback` needs the flag). Identical. Test `TestGMFFlagOffChangesNothing`.
- Locking and cost: `gmfFeed` takes `meState` only to copy the pointer out; the engine replaces the feed wholesale
  (`marketengine.go:713`), the feed is then read only. `gmfChoose` takes `gduty` once per guild (the same lock the round
  takes afterwards), no bot or gstock lock is held, `dutyCount` is called with `gduty` held as it expects. Cost per round:
  ten guilds x the short items, trivial.
- Score math: all factors are in (0,1] (need floor 0.25, gap ratio times price ratio, fit capped at 1); items with
  `Demand<=0`, `gap<=0`, `ItemID<=0`, no drop maps or `maxPrice<=0` are skipped, so there is no division by zero or NaN,
  and a zero-price item scores 0 and is never chosen (strict `>`). Ties break on the lower item id: stable ranking.
  Personalities are distinguishable (tests `PersonalitiesDiffer`, `HerdPenalty`); guilds 06 and 08-10 keep 1.0 weights and
  rank alike apart from need share and herd (minor, matches the survey).
- Herd penalty: works across guilds (others' held picks count as chasers, `1/(1+chasers)`). It is sequential in sorted
  order, so guild01 always chooses first on a cold start and gets first pick of the best item (minor, accepted
  consequence of the sorted iteration; the held picks then persist 24 h).
- Hysteresis: inside the 24 h hold the pick is kept while it stays short; after the hold a new item needs 1.25x the held
  item's current score; a covered item is dropped at once. Correct and tested.
- EC43 (information, not direction): the market sets no direction. `by=market` returns 410 before any flag check (after
  the clear/remove branches, which are leader operations); `marketDirOn()` is a constant false; stored market directions
  are dropped on load and never written (tests `OldFileDroppedOnLoad`, `NotWrittenOnSave`, `FlagHasNoEffect`). The pick
  goes only into the guild's autopilot wealth direction (the leader's own code). Precedence verified in
  `activeDirsLocked` (guildduty.go:288-338): owner direction, then the leader-set directions, then (unless a leader
  direction is exclusive) autopilot; so a leader's own direction always wins and an exclusive one suppresses the pick.
  Judgement: this is information used by the leader's own default, which matches design 4.2 and Constitution 2.6 ("feeds
  the guild leaders"); it is not an override. A leader that wants otherwise sets its own direction.
- Old `guild_duty.json` loads: `market_directions` is still parsed (omitempty field kept) and ignored; `feed_picks` is
  optional. Dropping the stored directions is acceptable: they were never live (flag default off, EC43 first version).
- G08: `market_set` kept in `dirView`, `market_state` and `feed_pick` added; nothing removed.
- legionctl refuses `--by market` before any call; the market brief says words only; the role stays in the dispatcher
  marked SUPERSEDED (Constitution 2.9 respected). `briefing.py` guards the feed block with try/except.
- No new default-on behaviour: `guild_market_feed_on` defaults false (tuning.go); `market_directions_on` stays parsed.

## Findings
No blockers. No major finding that harms live play with the defaults.

Minor
1. `guildmarketfeed.go` `gmfApply` (the `idx < 0` branch): when the autopilot already holds `guildDirMax` (3) directions
   and none is wealth (heal + stock + carry), the feed truncates to two and appends wealth, silently evicting the carry
   direction. The autopilot deliberately adds wealth only when a slot is free. Suggest: with no free slot and no wealth
   direction, return `dirs` unchanged. Fix before turning `guild_market_feed_on` on; no test covers it.
2. `gmfApply` / `activeDirsLocked`: a leader-set wealth direction for item X and the autopilot wealth direction for the
   feed item Y have different dedupe keys, so both can be active. Same before (item 0), but now two named items compete.
   The leader still ranks first; watch it in the flag-on soak.
3. `guildmarketfeed.go` (sequential herd penalty): guild01 picks first on every cold start; consider rotating the order
   daily once metrics exist. Not needed now.
4. `guildduty_api.go` clear/remove branches run before the 410, so a POST with `by=market` plus `clear` still clears the
   leader directions (pre-existing, harmless).

## Docs follow-ups
- `marketengine.go` header comment still says the leaders do not read the feed yet: stale after this merge.
- design market-engine.md status line ("leader feed ... not yet merged") and STATUS.md need the WP5/WP6 state.
- Deleting the dead channel code (unreachable `market` branches in `guildDirAPI`, `gduty.market`) is the planned later step.
- Replace the `127.0.0.1:1` dry-run test target with a local stub (pre-existing slow test, see above).

verdict: merge
