package main

// Empty maps (QA 2026-11-14 04:22): pay_fild01 read empty to every bot on it - 38 of 40 samples of ~10 bots saw no
// monster at all, while the spawn files list 264 spawns there (x4 mob_count_rate) and every other field showed 5-11
// monsters per bot. The map pickers only knew the static spawn list, so beginners kept being sent there and roamed
// with 0 kills. Whatever the server-side cause, a map is only as good as what the bots actually see on it.
//
// mapSeen keeps, per map, a decaying average (time constant ~10 min) of the monsters in view (b.ents, objMob) per bot
// sample; each bot on a non-town map adds one sample every 5 s (~12 per bot-minute, skipping its first 5 s on a map
// while the view fills). A map with enough recent samples whose average is below seen_empty_avg reads EMPTY: every
// map picker drops it (guideSpotsFor, SpotsFor, partyMapFor, partySpotNear, bestPartySpot). Nobody samples a dropped
// map, so its weight decays and after ~15-20 min it is tried again - a map that refilled comes back by itself.
//
// Solo bots: seeing 0 monsters (none at all, not even too-high ones) for seen_none_sec (60 s) while roaming gives the
// map up (badMap 10 min) and the bot moves on to the next map in its plan (the 120 s roam give-up still applies to
// maps where it sees only monsters it can't take).

import (
	"math"
	"net/http"
	"sort"
	"sync"
	"time"
)

const (
	seenTau        = 10 * time.Minute // decay time constant
	seenSampleGap  = 5 * time.Second  // one sample per bot per 5 s
	seenMinWeight  = 24.0             // ~2 bot-minutes of recent samples before a map can read empty
	seenEmptyAvg   = 0.6              // average monsters in view below this = empty (healthy fields read 5-11)
	seenThinAvg    = 2.0              // below this = thin: crowd.go counts the map as full (pickers prefer others)
	seenNoneGiveUp = 60 * time.Second // a solo bot seeing no monster at all for this long moves on
)

type seenStat struct {
	sum, w float64 // decayed sum of monster counts / decayed number of samples
	at     time.Time
}

var mapSeen = struct {
	sync.Mutex
	m map[string]*seenStat
}{m: map[string]*seenStat{}}

func (s *seenStat) decay(now time.Time) {
	if !s.at.IsZero() {
		f := math.Exp(-float64(now.Sub(s.at)) / float64(seenTau))
		s.sum *= f
		s.w *= f
	}
	s.at = now
}

// seenAdd records one bot's sample: n monsters in view on map m
func seenAdd(m string, n int, now time.Time) {
	mapSeen.Lock()
	s := mapSeen.m[m]
	if s == nil {
		s = &seenStat{}
		mapSeen.m[m] = s
	}
	s.decay(now)
	s.sum += float64(n)
	s.w++
	mapSeen.Unlock()
}

// mapSeenAvg: average monsters in view per bot sample on m, and the (decayed) sample weight behind it
func mapSeenAvg(m string) (float64, float64) {
	mapSeen.Lock()
	defer mapSeen.Unlock()
	s := mapSeen.m[m]
	if s == nil {
		return 0, 0
	}
	s.decay(time.Now())
	if s.w <= 0 {
		return 0, 0
	}
	return s.sum / s.w, s.w
}

// mapReadsEmpty: enough bots looked at m lately and they saw (almost) no monsters - every map picker skips it
func mapReadsEmpty(m string) bool {
	avg, w := mapSeenAvg(m)
	return w >= seenMinWeight && avg < seenEmptyAvg
}

// mapReadsThin: enough recent samples and bots see under seen_thin_avg monsters each (QA 2026-11-14 06:35: pay_fild01
// 0.7, prt_fild02 0.81, mjolnir_09 0.95, mjolnir_02 1.54, prt_fild03 1.75 per bot vs 4-6 on healthy fields). Those
// maps sat just above the empty line, so every picker kept sending bots and parties there and they roamed: crowd.go
// treats a thin map as full (mapHasRoom false, mapCrowded true) - pickers prefer a populated map, and a bot or party
// roaming there for roam_crowd_sec moves on. Like an empty reading, it decays once nobody is there.
func mapReadsThin(m string) bool {
	avg, w := mapSeenAvg(m)
	return w >= seenMinWeight && avg < seenThinAvg
}

// noteMobSight (think, b.mu held): sample the monsters in view for the map average, and track how long this bot has
// seen no monster at all
func (b *Bot) noteMobSight(now time.Time) {
	if b.MapName != b.seenMap {
		b.seenMap, b.seenMapAt, b.noMobSince = b.MapName, now, now
	}
	// worthless monsters (Green / Blue / Red Plants, mushrooms: no exp, every bot walks past them) are no monsters:
	// QA 2026-11-14 14:28, 50 of 52 bots on pay_fild10 sat in its 40x40 plant corner with 0 kills in 3 min - the
	// plants were shared as "sightings" (every sweeper headed for them) and made the corner read as full of monsters
	n := 0
	for _, e := range b.ents {
		if e.Type == objMob && !mobs[e.Class].worthless() {
			n++
		}
	}
	if n > 0 || b.Dead {
		b.noMobSince = now
	}
	if b.Dead || b.MapName == "" || isTown(b.MapName) || now.Sub(b.seenMapAt) < seenSampleGap || now.Sub(b.seenAt) < seenSampleGap {
		return
	}
	b.seenAt = now
	seenAdd(b.MapName, n, now)
	for _, e := range b.ents {
		// not our own target, not one another bot has claimed (QA 10:15: sweepers chased monsters already being
		// fought), not one busy with a player
		if e.Type == objMob && e.ID != b.target && claims.free(b.MapName, e.ID, 0) && !(e.Target != 0 && now.Sub(e.TargetAt) < 4*time.Second) {
			if mb := mobs[e.Class]; mb != nil && !mb.Boss && !mb.worthless() {
				x, y := e.pos(now)
				sightAdd(b.MapName, mobSight{x: x, y: y, lv: mb.Level, at: now})
			}
		}
	}
}

// Monster sightings shared by every bot on a map (QA 2026-11-14 06:40: 24% of the Legion's roaming time was a bot
// party leader seeing NO monster on a well-populated map - explore() walked to a random far cell and searched blind
// while other bots a screen away saw monsters they were not fighting). Each 5 s sample also records the positions of
// the non-boss monsters in view that the bot is not fighting; explore() heads for the nearest recent one it can take.
type mobSight struct {
	x, y, lv int
	at       time.Time
}

const (
	sightKeep   = 64               // sightings kept per map (ring)
	sightMaxAge = 40 * time.Second // older ones are gone or killed by now
)

var mapSights = struct {
	sync.Mutex
	m   map[string][]mobSight
	pos map[string]int
}{m: map[string][]mobSight{}, pos: map[string]int{}}

func sightAdd(m string, s mobSight) {
	mapSights.Lock()
	l := mapSights.m[m]
	if len(l) < sightKeep {
		mapSights.m[m] = append(l, s)
	} else {
		i := mapSights.pos[m] % sightKeep
		l[i] = s
		mapSights.pos[m] = i + 1
	}
	mapSights.Unlock()
}

// sightNear: the nearest recent sighting on m of a monster at most maxLv, between minD and maxD cells from (x, y),
// for which ok(x, y) holds; found=false when none
func sightNear(m string, x, y, minD, maxD, maxLv int, now time.Time, ok func(int, int) bool) (int, int, bool) {
	mapSights.Lock()
	l := append([]mobSight(nil), mapSights.m[m]...)
	mapSights.Unlock()
	bx, by, bd := 0, 0, maxD+1
	for _, s := range l {
		if now.Sub(s.at) > sightMaxAge || s.lv > maxLv {
			continue
		}
		if d := dist(x, y, s.x, s.y); d >= minD && d < bd && ok(s.x, s.y) {
			bx, by, bd = s.x, s.y, d
		}
	}
	return bx, by, bd <= maxD
}

// seesNothing: a solo bot that has roamed AND seen no monster at all for seen_none_sec
func (b *Bot) seesNothing(now time.Time) bool {
	return !b.noMobSince.IsZero() && now.Sub(b.noMobSince) > seenNoneGiveUp && now.Sub(b.roamSince) > seenNoneGiveUp
}

// registerMapSeen: GET /api/mapseen - per-map average monsters in view, sample weight, and whether it reads empty
func registerMapSeen(mux *http.ServeMux) {
	mux.HandleFunc("/api/mapseen", func(w http.ResponseWriter, r *http.Request) {
		type row struct {
			Map    string  `json:"map"`
			Avg    float64 `json:"avg_in_view"`
			Weight float64 `json:"samples"`
			Empty  bool    `json:"empty"`
		}
		mapSeen.Lock()
		names := make([]string, 0, len(mapSeen.m))
		for m := range mapSeen.m {
			names = append(names, m)
		}
		mapSeen.Unlock()
		sort.Strings(names)
		out := []row{}
		for _, m := range names {
			avg, wt := mapSeenAvg(m)
			out = append(out, row{m, math.Round(avg*100) / 100, math.Round(wt*10) / 10, wt >= seenMinWeight && avg < seenEmptyAvg})
		}
		writeJSON(w, out)
	})
}
