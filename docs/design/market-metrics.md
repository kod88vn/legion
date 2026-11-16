# Market metrics (WP8): counters and JSON shape

Implements section 3.5 of `docs/design/market-engine.md`. Code: `legion/marketmetrics.go` (counters, block, persistence),
`legion/marketmetrics_test.go`. Flag-independent: the counters run every economy round whatever `market_engine_on` says.
Nothing here changes a bot, a price or an order.

## Where the block is served

- `GET /api/meter/market`: the block alone.
- `GET /api/market/engine`: the same block as the key `"metrics"`, both when the flag is off (status `off`) and on.
  Existing keys (`status`, `flag`, `note`, `runs`, `last_run`, `farmers`, `caps`, `ordered`, `unassigned`,
  `distribution`, `needs`, `orders`) are unchanged.
- `GET /api/market/feed`: `zeny_per_hour` is filled from the meter (bands with counted bots, `n`, `zph_mean`). The
  missing-list line `zeny_per_hour: ...` is removed only when that list is non-empty.
- `tools/puppet/progress.py` reads `/api/market/engine` and carries a compact `"metrics"` key in the briefing (see
  `_metrics_brief`). An engine that answers without a block adds a note, no number.

## Shape

Every ratio is `null` when its denominator is zero. Every list is capped at 40 rows (`mmTopN`), sorted by need, sd or zph
as noted. Times are RFC 3339.

```
{
  "t": time, "counters_since": time, "rounds": int,
  "coverage": {
    "status": "no_data" | "ok", "overall": float|null,
    "unit": "units supplied / need units (need = sum over hours of the peak bots short)",
    "by_band": [ {"band": "1-32", "need": f, "supplied": f, "coverage": float|null} ],
    "by_item": [ {"item_id": int, "item": str, "need": f, "supplied": f, "coverage": float|null} ]
  },
  "price_stability": {
    "status": "no_data" | "ok", "target": 1.5, "overall": float|null,   // mean stddev over counted items
    "within_target": bool|null, "items_counted": int, "items_over": int,
    "items": [ {"item_id": int, "item": str, "n": int, "stddev": float} ]  // largest stddev first
  },
  "farmers": {
    "status": "no_data" | "ok", "offers": int, "accepted": int, "accept_rate": float|null,
    "declined": {"<reason>": int}, "deaths": int, "farmer_hours": float,
    "deaths_per_hour": float|null, "items_supplied": int
  },
  "kite": {"status": "no_data" | "ok", "steps": int, "deaths": int, "deaths_per_step": float|null},
  "zeny_per_hour": {"status": "no_data" | "ok", "by_band": [ {"band": str, "n": int, "zph_mean": float|null} ]}
}
```

## Definitions

- **Need** (per band and item): every engine round counts the bots whose next build piece is the item, per band
  (`meBand`). Each clock hour keeps the peak of that count; when the hour closes the peak is added to the need
  (`mmApplyRound`). So a need unit is "one bot-hour-peak of demand". The hour in progress is shown in the block but
  is not saved; at most one hour is lost at a restart.
- **Supplied**: units delivered by farmers, from `noteMMSupplied(band, item, n)`. Only the farmer code calls it.
- **Coverage** = supplied / need. Null when need is zero.
- **Price stability**: each round samples `priceOf(id) / standardPrice(id)` for the items bots are short of and the
  heal items. Per item, the population stddev of the samples (needs at least 3). Target: under 1.5. Cumulative since
  the counters began; the 24-hour window of section 3.5 is not implemented yet.
- **Farmers** (placeholders): `noteMMFarmerOffer`, `noteMMFarmerAccept`, `noteMMFarmerDecline(reason)`,
  `noteMMFarmerDeath`, `noteMMFarmerHours(h)`. The farmer-orders branch calls them; until then `status` is `no_data`
  and the ratios are null.
- **Kite**: read from `kiteCounters()` (kite.go).
- **Zeny per hour**: the meter's `zph` for counted rows in the 60-minute window (`meterCur`), by band.

## Persistence

`<data>/market_metrics.json` (core instance `market_metrics_core.json`), written every minute and at stop with a
temp file and rename (as `econ_counters.json`). Read once at start-up and added to the running counts. Saved fields:
need and supplied per band and item, price sums per item, farmer counters, round count, `since`.

## Tests (fixtures only, G23)

`TestMMStddevFormula`, `TestMMCoverageFormula`, `TestMMEmptyDataIsNull`, `TestMMCoverageByBandAndItemFromRounds`,
`TestMMPriceStabilityFromRounds`, `TestMMCountersPersistAcrossRuns`, `TestMMFeedZenyLineGoesOnlyWhenFed`,
`TestMMEngineAPIAdditive`. Puppet: `tools/puppet/tests/test_progress.py` class `TestMarketMetrics`.

## Not verified

No live run. The meter's zph by band and the price samples depend on live data that this branch has not seen. The
farmer placeholders stay empty until fix/farmer-orders is merged and calls the note functions.
