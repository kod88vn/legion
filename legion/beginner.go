package main

// Beginner map picks (QA 2026-11-14 05:20, after the empty-map change went live): Payon/Alberta beginners (lv3-18)
// died in Payon Cave - pay_dun00 was the top death map. DarkPriest (Novice lv3): "no monsters in sight on pay_fild01"
// -> "too dangerous on pay_fild03" -> both home fields banned -> the empty-map fallback took any spawn-list map near
// home whose monster levels overlapped (pay_dun01) -> died on pay_dun00 twice.
//
// Rules now:
//   - a beginner (base level < beginner_lv) only ever gets a GUIDE FIELD that fits its effective level (no dungeon, no
//     non-guide map), from every fallback;
//   - healthy home field first (fits, not banned for this bot, not reading empty, room, not deadly FOR ITS LEVEL BAND);
//   - every home field bad and it can afford the Kafra: a healthy beginner field in another region (prt_fild08...);
//   - otherwise the least-bad home field (a ban or an empty reading only lowers its rank).
//   - map deaths are judged per level band (recentDeathsFor): 5 Novice deaths on pay_fild03 must not say "too
//     dangerous" to anyone else, nor 5 lv50 deaths to a Novice - and scaled by how many bots stand there.

import (
	"math"
	"strings"
	"time"
)

// beginnerField: m is a guide field (not a dungeon) whose level window fits lv
func beginnerField(m string, lv int) bool {
	if strings.Contains(m, "_dun") || maps[m] == nil {
		return false
	}
	for _, g := range guideSpots {
		if g.Map == m && lv >= g.Min-2 && lv <= g.Max+3 && !g.Party {
			return true
		}
	}
	return false
}

// beginnerPick (b.mu held): the beginner's hunting map, "" if no guide field fits at all
func (b *Bot) beginnerPick() string {
	lv, now := b.powerLv(), time.Now()
	banned := func(m string) bool { return m == b.badMap && now.Before(b.badMapUntil) }
	deadly := func(m string) bool { return b.deadlyFor(m) }

	// 1. a healthy field near home: the nearest with room (crowded: the least crowded near home)
	best, bh := "", 99
	crowdBest, crowdO := "", 0
	healthy := []string{}
	for _, g := range b.guideSpotsFor(lv) { // fits, not swarm, not reading empty (seen.go)
		m := g.Map
		if !beginnerField(m, lv) || banned(m) || deadly(m) {
			continue
		}
		healthy = append(healthy, m)
		h := b.nearHome(m)
		if h > 3 {
			continue
		}
		o := crowdPct(m)
		if mapReadsThin(m) {
			o += 100 // seen.go: bots there see few monsters - the last resort among the full home fields
		}
		if crowdBest == "" || o < crowdO {
			crowdBest, crowdO = m, o
		}
		if !mapHasRoom(m, 1) {
			continue
		}
		if h < bh || (h == bh && (b.shortOfZeny() && mapFarmValue(m) > mapFarmValue(best) || !b.shortOfZeny() && b.rng.Intn(2) == 0)) {
			best, bh = m, h
		}
	}
	if best != "" {
		return best
	}
	if crowdBest != "" {
		return crowdBest // every healthy home field is crowded: the least crowded one, never another region for that
	}
	// 2. no healthy field near home: with Kafra money to spare, a healthy beginner field in another region
	if b.Zeny >= tune().KafraKeepBeginner+1200 && len(healthy) > 0 {
		bs := -1e9
		for _, m := range healthy {
			avg, w := mapSeenAvg(m)
			if w < seenMinWeight {
				avg = 3
			}
			sc := avg - float64(crowdPct(m))/50 - float64(b.nearHome(m))*0.5
			if sc > bs {
				best, bs = m, sc
			}
		}
		if best != "" {
			b.event("info", "", "no good field near home: heading to "+best)
			return best
		}
	}
	// 3. the least-bad fitting field near home (then anywhere): bans / empty readings / deaths only lower the rank
	for pass := 0; pass < 2 && best == ""; pass++ {
		bs := -1e9
		for _, g := range guideSpots {
			m := g.Map
			if !beginnerField(m, lv) || pass == 0 && b.nearHome(m) > 3 {
				continue
			}
			sc := -float64(b.nearHome(m))
			if banned(m) {
				sc -= 20
			}
			if mapReadsEmpty(m) {
				sc -= 10
			}
			if deadly(m) {
				sc -= 15
			}
			if sc > bs {
				best, bs = m, sc
			}
		}
	}
	return best
}

// deathBand: level band for map death counts
func deathBand(lv int) int { return max(lv, 1) / 10 }

// map deaths per level band, decaying with a 20-minute half-life (like nav.go recentDeaths)
var bandDeaths = struct {
	m map[string]float64 // "map|band"
	t map[string]time.Time
}{m: map[string]float64{}, t: map[string]time.Time{}}

func bandKey(m string, band int) string { return m + "|" + string(rune('a'+band)) }

func bandDeathsLocked(k string) float64 {
	v := bandDeaths.m[k]
	if v == 0 {
		return 0
	}
	return v * math.Pow(0.5, time.Since(bandDeaths.t[k]).Minutes()/20)
}

// reportDeathLv: a bot of base level lv died on m (shares nav.go's deathLog lock)
func reportDeathLv(m string, lv int) {
	deathLog.Lock()
	defer deathLog.Unlock()
	k := bandKey(m, deathBand(lv))
	bandDeaths.m[k] = bandDeathsLocked(k) + 1
	bandDeaths.t[k] = time.Now()
}

// recentDeathsFor: recent deaths on m of bots in lv's level band (the neighbouring bands count half)
func recentDeathsFor(m string, lv int) float64 {
	deathLog.Lock()
	defer deathLog.Unlock()
	b := deathBand(lv)
	d := bandDeathsLocked(bandKey(m, b))
	if b > 0 {
		d += bandDeathsLocked(bandKey(m, b-1)) / 2
	}
	return d + bandDeathsLocked(bandKey(m, b+1))/2
}

// deadlyFor (b.mu held): m has had too many deaths lately among bots of our level band, per 10 bots standing there
// (a busy beginner field always sees some Novice deaths)
func (b *Bot) deadlyFor(m string) bool {
	l, _ := mapLoad(m)
	return recentDeathsFor(m, b.BaseLvOr()) > 6*(1.2-b.P.Risk)*math.Max(1, float64(l)/10)
}

// townHops: map changes from town t's spawn point to m (99 if unknown)
func townHops(t, m string) int {
	sp, ok := townSpawn[t]
	if !ok {
		return 99
	}
	return mapHops(t, sp[0], sp[1], m)
}

// saveStillGood: the town we saved in is (about) as near the hunting map m as town nt - no re-save. QA 2026-11-14:
// beginners hunting pay_fild01/pay_fild03 saved alberta, then payon 5 min later, then alberta again.
func (b *Bot) saveStillGood(nt, m string) bool {
	if b.savedTown == "" || b.savedTown == nt || m == "" {
		return b.savedTown == nt && nt != ""
	}
	return townHops(b.savedTown, m) <= townHops(nt, m)+1
}
