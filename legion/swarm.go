package main

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// Swarm maps (QA 2026-11-14, mjolnir_06: 7 deaths in 5 min, 3 of the first 5 bots only passing through). mapDanger
// ignores aggressive monsters 5+ levels under the traveller, but 100 aggressive Poison Spores (lv26) still kill a
// lv38-48 bot that walks the map long enough to collect a train: 11 Spores hit a Merchant within 35 s.

// swarmDanger: route cost of the dense aggressive monsters on map m that mapDanger leaves out (5+ levels under us).
// Weighted by their level relative to ours: 100 lv26 Spores = 32 for a lv46, 10 for a lv60, 0 from lv75.
func swarmDanger(m string, level int) float64 {
	if level <= 0 {
		level = 1
	}
	n, d := 0, 0.0
	for _, s := range spawnsByMap[m] {
		mb := mobs[s.MobID]
		if mb == nil || !mb.Aggressive || mb.Boss || s.Count <= 0 || mb.Level-level > -5 {
			continue
		}
		r := float64(mb.Level) / float64(level)
		w := math.Min(1, math.Max(0, (r-0.35)/0.15))
		n += s.Count
		d += float64(s.Count) * r * r * w
	}
	if n < 40 { // a few low aggressive monsters are no threat; a dense pack is
		return 0
	}
	return d
}

// swarmMap: a map a traveller of this (effective) level crosses without stopping to fight.
func swarmMap(m string, level int) bool { return swarmDanger(m, level) >= 15 }

// swarmVeto: map pickers (guide spots, auto hunt, party maps, Kafra landings) skip such a map for this level (a party:
// its weakest member). 100 Spores (mjolnir_06): vetoed up to ~lv50.
func swarmVeto(m string, level int) bool { return swarmDanger(m, level) >= 25 }

// swarmCache: "from|goal|lv" -> extra hops for the swarm maps on the walking route (static data: spawns don't change).
var swarmCache sync.Map

// swarmPenalty: 2 extra "hops" per swarm map the walk from (a, ax, ay) to goal crosses (the goal itself excluded), so
// kafraPlan / kafraVia take a Kafra teleport around such a map when one exists.
func swarmPenalty(a string, ax, ay int, goal string, level int) int {
	k := fmt.Sprintf("%s|%s|%d", a, goal, level)
	if v, ok := swarmCache.Load(k); ok {
		return v.(int)
	}
	p := 0
	for _, w := range RouteMaps(a, ax, ay, goal, level) {
		// also a train / aggressive map too hard for us (ai.go partyMapTooHard): members travelling to their party
		// died crossing mjolnir_02 (Onirogue 7 deaths, xXYukiqueenXx 5) - kafraPlan / kafraVia teleport around it
		if w.To != goal && (swarmMap(w.To, level) || !isTown(w.To) && partyMapTooHardHere(w.To, level)) {
			p += 2
			// QA 2026-11-14 19:20-19:56 UTC (life mind): crossings with aggressive monsters 10+ levels over the traveller
			// (mjolnir_12 Hunter Fly lv63 for Rin.2k lv41, ...) killed bots on the way; +2 hops was not enough to make
			// the Kafra planners go round them
			if mapAggroTopLv(w.To) >= level+10 {
				p += 3
			}
		}
	}
	swarmCache.Store(k, p)
	return p
}

// transitSwarm (under b.mu): only passing through a swarm map - walk on, fight only what blocks us, flee when 3+ are
// on us (losingFight transit).
func (b *Bot) transitSwarm() bool {
	return b.Goal.Map != "" && b.Goal.Map != b.MapName && b.Goal.Mode != "idle" && !isTown(b.MapName) &&
		swarmMap(b.MapName, min(b.powerLv(), b.BaseLvOr())) // the lower of level and gear-rated level (Thorn.claw died)
}

// transitBlocked: in transit but not moving for 3 s (something stands in the way / holds us): fight it.
func (b *Bot) transitBlocked(now time.Time) bool {
	return now.Sub(b.lastPosAt) > 3*time.Second
}
