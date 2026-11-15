package main

// zenyhour.go (economy run 8, income): expected zeny per hour of farming a map, for map choice.
//
// Measured 2026-11-14 12:10 (picklog x RE item db): a lv60 bot made ~3.6k zeny/hour of loot - ~40z per kill at ~90
// kills/h - because gear-poor bots pick the safe, crowded low fields (moc_fild01: 72 bots, avg lv51, lv7-25 monsters
// worth ~16z/kill), and ~16% of the non-vendors roamed with no monster in sight. Map pickers looked at EXP and safety
// (and loot per KILL for broke bots), never at what an hour on the map pays. mapZenyHour estimates it:
//
//	value per kill  = NPC loot (mobLootValue, server drop rates) + player-trade goods at the market price (mobTradeZ, cards/gear/healing)
//	time per kill   = search (grows with the map's crowding: bots per monster) + HP / damage-per-second(power) + loot
//	kills / hour    = 3600 / mean time per kill, over the monsters the bot would fight (level <= power + 5)
//	z/h             = kills/h x mean value per kill - ammo burn (archers: 1 arrow per hit)
//
// The damage model is coarse on purpose (one curve per effective level: powerLv already folds in the weapon and the
// armor); it ranks maps for one bot, it doesn't predict income. MapZenyHour / (b *Bot).MapZenyHour are the public
// hooks (lfg.go goal maps, partyMapSearch).

import (
	"math"
	"sync"
	"time"
)

// dpsAt: rough damage per second of a character fighting at effective level p (Renewal: ~50 at 20, ~150 at 35, ~430
// at 60 - auto attacks plus the odd skill).
func dpsAt(p int) float64 {
	pf := float64(max(p, 1))
	return 2 + 0.12*pf*pf
}

const (
	zphPerHop     = 3000.0 // party pick: 3k zeny/hour more is worth one more map change (max 3)
	zphSearchBase = 6.0    // seconds to find and reach the next monster on an uncrowded field
	zphLootSec    = 1.0    // picking up
	zphMaxSearch  = 60.0   // a map so crowded that bots mostly roam
)

var zphCache = struct {
	sync.Mutex
	m  map[[4]int]float64 // (map hash, power, family, load/3) -> z/h, short-lived (crowds move)
	at time.Time
	ix map[string]int
}{m: map[[4]int]float64{}, ix: map[string]int{}}

// MapZenyHour: expected zeny per hour on map m for a character at effective level power of class family fam, with
// the map's current crowding. 0 when nothing there is worth fighting for it.
func MapZenyHour(m string, power, fam int) float64 {
	zphCache.Lock()
	if time.Since(zphCache.at) > 2*time.Minute {
		zphCache.m, zphCache.at = map[[4]int]float64{}, time.Now()
	}
	id, ok := zphCache.ix[m]
	if !ok {
		id = len(zphCache.ix) + 1
		zphCache.ix[m] = id
	}
	// the crowd is part of the key (load in steps of 3 bots): every seat reservation (crowd.go crowdNote) moves the
	// value at once - a 2-minute cache of a stale crowd sent a whole login wave to the same best map (QA 12:33:
	// gef_fild02 139 bots, pay_fild09 76)
	l0, _ := mapLoad(m)
	key := [4]int{id, power, fam, l0 / 3}
	if v, ok := zphCache.m[key]; ok {
		zphCache.Unlock()
		return v
	}
	zphCache.Unlock()
	l, r := mapLoad(m)
	v := mapZenyHourCalc(m, power, fam, l)
	if l >= 6 {
		v /= 1 + 3*float64(r)/float64(l) // live: bots there roaming with nothing in sight (crowd.go) find little
	}
	zphCache.Lock()
	zphCache.m[key] = v
	zphCache.Unlock()
	return v
}

// mapZenyHourCalc: the model with an explicit crowd (bots on the map, the caller included or not - close enough).
func mapZenyHourCalc(m string, power, fam, bots int) float64 {
	lootOnce.Do(loadMobLoot)
	n, val, sec, monsters := 0, 0.0, 0.0, 0
	dps := dpsAt(power)
	for _, s := range spawnsByMap[m] {
		mb := mobs[s.MobID]
		if mb == nil || mb.Boss || s.Count <= 0 {
			continue
		}
		monsters += s.Count
		if mb.worthless() || mb.Level > power+5 {
			continue // not fought: a target filter skips it (too strong) or a player walks past it
		}
		v := mobLootValue(s.MobID) + mobTradeZ(s.MobID)
		t := float64(mb.HP) / dps
		if fam == famArcher {
			v -= float64(mb.HP) / (dps / 1.5) // ~1.5 hits a second, one 1z arrow each
		}
		n += s.Count
		val += v * float64(s.Count)
		sec += t * float64(s.Count)
	}
	if n == 0 || monsters == 0 {
		return 0
	}
	// search time: every bot on the map shares its monsters; past ~1 bot per 4 monsters they run out
	crowd := float64(bots) / float64(monsters)
	search := math.Min(zphMaxSearch, zphSearchBase*(1+12*crowd*crowd))
	// a map where most monsters are out of reach is mostly walking past them
	search /= math.Max(0.15, float64(n)/float64(monsters))
	perKill := sec/float64(n) + search + zphLootSec
	return math.Max(0, 3600/perKill*(val/float64(n)))
}

// MapZenyHour (b.mu held): this bot's expected zeny per hour on m (its effective level and class family).
func (b *Bot) MapZenyHour(m string) float64 {
	return MapZenyHour(m, b.powerLv(), family(b.Class))
}

// bestZenyMap (b.mu held): of the candidate maps, the one paying this bot the most per hour ("" if none pays).
// One of the maps within 80% of the best, at random (crowd.go seats + zenyhour's crowd term keep a login wave from all
// taking the single best map; QA 12:33 herding).
func (b *Bot) bestZenyMap(ms []string) string {
	bz := 0.0
	zs := make([]float64, len(ms))
	for i, m := range ms {
		zs[i] = b.MapZenyHour(m)
		bz = math.Max(bz, zs[i])
	}
	if bz <= 0 {
		return ""
	}
	var near []string
	for i, m := range ms {
		if zs[i] >= 0.8*bz {
			near = append(near, m)
		}
	}
	return near[b.rng.Intn(len(near))]
}

// partyZenyHour: a party's expected zeny per hour on m, at the weakest member's fitting level (fitLv) - the party
// shares the kills, the search time is the map's.
func partyZenyHour(m string, fitLv int) float64 {
	return MapZenyHour(m, fitLv, famSword)
}
