# Review of 96e7b3d (fix/dash-intent2, merged as c929057), retroactive

Scope (merge-base 13e6cae..96e7b3d): `legion/api.go` (`human_detail`, `human_intent`), `legion/partydash.go` (flat
intent / last_decision, `PartyMemberExtras`, `Helper`), `legion/partyintent.go` (`expSample`, `meterKPM`,
`helperLastRun`), `legion/partyintent_test.go`, `legion/dashboard.html` (to-leader, kpm, exp/h columns; helper line).

## Findings

### major: the "Haiku helper last ran" line silently skips every failed run (still on main)
- `legion/partyintent.go:278` declares `Exit int` (json "exit"), but `tools/puppet/dispatch.py:606-627` writes `exit` as
  `null`, an int, or the strings `"refused"`, `"timeout"`, `"error"`. `json.Unmarshal` of a string into an `int` returns
  an `UnmarshalTypeError`, and `helperLastRun` only keeps a row when `json.Unmarshal(...) == nil` (partyintent.go:321).
  Result: refused, timed-out and crashed helper runs are skipped, and the dashboard shows the last run that happened to
  log an integer exit, as "ok" or "failed". The page (`helperLine`, dashboard.html:1144) even has a red "failed" branch
  for `r.exit`, which a timeout can never reach. Constitution 2.8 (every helper rescue is a filed bug) and 3.2 (a blind
  meter is the first fault) need this line to show the real last run. The code is untested: `helperLastRun` has no test.
- Fix forward: decode `exit` as `json.RawMessage` or `interface{}` (pass it on as given), treat anything other than `0`
  as failed (null with no result too), and let the page print the string (`timeout`, `refused`, `error`). Add a Go test
  with a temp `puppet_calls.jsonl` (G23: temp data dir) holding rows with exit 0, 1, "timeout", "refused", "error", null
  and a mix of roles, asserting the newest row of the role comes back in every case.

### minor
- partyintent.go:224 `expSample`: `expMeter.by` is never pruned (one track per member name, bounded by the roster).
  Samples are only taken when someone polls `/api/parties/detail`, so exp/h reads "needs a minute of samples" after the
  dashboard has been closed; the cell tooltip says so. Acceptable.
- `expSample` counts only the new level's exp on a level-up (documented), so exp/h reads low for that minute.
- dashboard.html:1140 `metricCells`: `dist_lead` is -1 for an offline member and for an unknown leader cell, and the
  page then prints "? ≠ <lead map>" for an offline bot (its `map` is empty). Cosmetic: print "–" when `!m.online`.
- `TestExpSampleAndMemberJSONRoundTrip` covers the embedded-struct JSON round trip that `mergeCoreParties` needs (good),
  but nothing covers `helperLastRun`, `meterKPM` or the JS cells.

### checked, fine
- G08: only added columns and lines. XSS: `helperLine` escapes `trigger` and `result` (title and body); `metricCells`
  escapes map names. `human_detail` in `api.go` exposes the same struct the party view already returns.
- Locking: `expSample` takes only its own leaf lock while `m.mu` is held; `meterKPM` is called after `m.mu.Unlock()`.
- `helperLastRun` reads at most the last 256 KB, at most every 15 s; a cut first line fails to parse and is skipped.

verdict: do not merge
