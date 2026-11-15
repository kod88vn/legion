package main

import (
	"fmt"
	"time"
)

// fairgate.go: a solo hunting spot must hold fair kills for the bot's base level. The kill metric (meter.go
// meterIsFair) counts a kill fair when the monster's level is at least base level minus meter_fair_lv_gap, so a spot
// whose monsters all sit under that line gives a bot nothing to score (bugq b1794751012196-928: a lv53 Monk on
// pay_fild07, Bigfoot lv29 and Caramel lv25, 119 kills and 0 fair in an hour). The gate applies to every solo picker
// (guide spots, goodHere, beginnerPick, the SpotsFor fallback). Party pickers keep their own rules.

// fairSpotShare: the share of a map's counted monsters that must be fair for a solo bot's base level (tune
// fair_spot_share; 0 or less = the default 0.5).
func fairSpotShare() float64 {
	if s := tune().FairSpotShare; s > 0 {
		return s
	}
	return 0.5
}

// fairBandShare: the share of the map's spawned monsters (count-weighted, bosses left out) at or above the fair line for
// baseLv. known=false when the map has no counted non-boss monster (no data: the gate does not judge it).
func fairBandShare(m string, baseLv int) (share float64, known bool) {
	floor := baseLv - meterTune().FairLvGap
	tot, fair := 0, 0
	for _, s := range spawnsByMap[m] {
		mb := mobs[s.MobID]
		if mb == nil || mb.Boss || s.Count <= 0 {
			continue
		}
		tot += s.Count
		if mb.Level >= floor {
			fair += s.Count
		}
	}
	if tot == 0 {
		return 0, false
	}
	return float64(fair) / float64(tot), true
}

// fairSpotOK: the solo spot gate at base level. Flag off = always true. A map with no known monsters (only bosses or
// no spawn rows) is kept: missing data is neutral, not a reason to drop it.
func fairSpotOK(m string, baseLv int) bool {
	if !tune().FairSpotGateOn {
		return true
	}
	share, known := fairBandShare(m, baseLv)
	return !known || share >= fairSpotShare()
}

// spotsFor: SpotsFor for this bot. A solo bot's spots pass the fair gate at its base level (not the effective level,
// so gear does not move the line); a party member keeps the plain list. When the gate empties a list (a bot whose
// effective level sits well under its base level: the band is mostly monsters under base-15) the bot is not left idle:
// it gets the one ungated spot whose monsters are most often fair (logged once per bot per hour).
func (b *Bot) spotsFor(lv int) []HuntSpot {
	all := SpotsFor(lv, b.P.Risk)
	if b.PartyID != 0 || !tune().FairSpotGateOn {
		return all
	}
	base := b.BaseLvOr()
	out := all[:0:0]
	for _, s := range all {
		if fairSpotOK(s.Map, base) {
			out = append(out, s)
		}
	}
	if len(out) > 0 || len(all) == 0 {
		return out
	}
	best, bs := 0, -1.0
	for i, s := range all {
		sh, known := fairBandShare(s.Map, base)
		if !known {
			sh = fairSpotShare()
		}
		if sh > bs {
			best, bs = i, sh
		}
	}
	b.logFairFallback(lv, base, all[best].Map, bs, len(all))
	return []HuntSpot{all[best]}
}

// logFairFallback: the gate emptied the spot list for this bot. Logged once per bot per hour (buffAt "fairlog").
func (b *Bot) logFairFallback(lv, base int, m string, share float64, n int) {
	if b.buffAt == nil {
		b.buffAt = map[string]time.Time{}
	}
	now := time.Now()
	if now.Before(b.buffAt["fairlog"]) {
		return
	}
	b.buffAt["fairlog"] = now.Add(time.Hour)
	b.event("info", "", fmt.Sprintf("no spot holds fair kills for base lv%d (effective lv%d): %d spots gated out, taking %s (fair share %.2f)", base, lv, n, m, share))
}
