package main

// gearscore.go (AY07 sharpened 2026-11-14: "map choice should base on gear level too, not just level, since most of
// her parties are poorly equipped"; gear counts as much as level). Two parts, both behind tuning flags (default ON):
//
//  1. gear_score_on - the effective level (powerLv) rates the WHOLE kit, not just weapon ATK + body DEF:
//     weapon ATK/MATK plus its refine against the weapon expected at the level, cards in the weapon, the DEF/MDEF of
//     every worn armor piece (body, shield, garment, shoes, headgear) plus refines against a full level-fitting set,
//     armor/accessory cards, and a penalty for each empty core slot (body, garment, shoes, head). Very good gear
//     (refined, carded, full set) rates a little ABOVE the base level (at most +15% / +8). Every map picker already
//     reads powerLv / the published Power (solo SpotsFor/guideSpotsFor, partyFitLv, bestPartySpot, goalMapSafeItem),
//     so they all choose from this effective level; the healer / tank bonus (partyStrength) stays on top.
//
//  2. gear_perf_on - measured performance corrects the estimate: per solo bot (applied in powerLv while it has no
//     party) and per bot party (keyed by its leader, applied in partyFitLv / bestPartySpot), kills, fair kills
//     (monster >= level - 15, the meter's filter) and deaths are sampled over >= perfWindow on one map. Deaths high
//     or kills far under par with deaths -> step DOWN 3 levels (an easier map); no deaths and the kills come easily
//     (mostly unfair kills = monsters far below, or fair kills fast) -> step UP. Bounded perfMin..perfMax.
//
// Lock discipline (lessons.md, 2026-11-14 wedge): the solo tracker runs inside powerLv with b.mu held and only reads
// the bot's own meter counters; the party tracker reads published state (peek). Both trackers have their own small
// mutex and never take a bot lock or a global lock.

import (
	"sync"
	"time"
)

func gearScoreOn() bool { return tune().GearScoreOn }
func gearPerfOn() bool  { return tune().GearPerfOn }

// gearRating: the kit measured against the level (1 = at par).
type gearRating struct {
	W, A    float64 // weapon and armor ratios (W up to 1.5, A up to 1.4)
	Cards   int     // cards in worn gear
	Missing int     // empty core slots (body, garment, shoes, head)
}

// refineAtk: renewal weapon refine bonus per +1 by weapon level (Lv1 +2, Lv2 +3, Lv3 +5, Lv4 +7).
func refineAtk(wlv, ref int) int {
	per := 2
	switch wlv {
	case 2:
		per = 3
	case 3:
		per = 5
	case 4:
		per = 7
	}
	return per * ref
}

// expectedArmorDef: DEF of a full level-fitting NPC set (renewal values: lv20 Adventurer's Suit 20 + Hood 4 + Sandals
// 5 + Hat 2 ~ 31-40; lv40 Mantle 37 + Muffler 8 + Shoes 10 + Cap 7 = 62; lv80 Chain Mail 55 + Manteau 13 + Boots 16 +
// Helm 13 ~ 100+). A shield is not expected (two-handers, bows, casters) and counts as a bonus.
func expectedArmorDef(lv int) float64 { return 18 + 1.1*float64(lv) }

// rateGear: the worn kit of a family-f character at level lv. eq: slot -> worn item (Bot.equipped).
func rateGear(f, lv int, eq map[uint32]*InvItem) gearRating {
	var r gearRating
	seen := map[*InvItem]bool{}
	def, mdef, armorCards := 0, 0, 0
	have := map[string]bool{}
	for slot, e := range eq {
		if e == nil || seen[e] || slot == eqpAmmo {
			continue
		}
		seen[e] = true
		x := items[e.ItemID]
		if x == nil {
			continue
		}
		nc := 0
		for _, c := range e.Cards {
			if c >= 4001 && c < 5000 || c >= 27000 && c < 32000 { // card ids (forged / named marks are 0x00FF/0xFF00)
				nc++
			}
		}
		r.Cards += nc
		if slot == 0x2 { // the weapon (two-handers sit here too)
			atk := float64(x.Atk + refineAtk(max(1, x.WLv), e.Refine))
			pw := atk / expectedWeaponPower(f, lv)
			switch f {
			case famMage:
				pw = float64(x.Matk+refineAtk(max(1, x.WLv), e.Refine)) / expectedWeaponPower(famMage, lv)
			case famAcolyte:
				pw = max(pw, float64(x.Matk+refineAtk(max(1, x.WLv), e.Refine))/expectedWeaponPower(famMage, lv))
			}
			r.W = min(1.5, max(0, pw)*(1+0.10*float64(nc)))
			continue
		}
		armorCards += nc
		if slot == 0x88 {
			continue // accessories: cards only
		}
		def += x.Def + e.Refine
		mdef += x.Mdef
		switch {
		case slot&0x10 != 0:
			have["body"] = true
		case slot&0x4 != 0:
			have["garment"] = true
		case slot&0x40 != 0:
			have["shoes"] = true
		case slot&0x301 != 0:
			have["head"] = true
		}
	}
	for _, k := range []string{"body", "garment", "shoes", "head"} {
		if !have[k] {
			r.Missing++
		}
	}
	a := (float64(def)+0.5*float64(mdef))/expectedArmorDef(lv) + 0.04*float64(armorCards) - 0.05*float64(r.Missing)
	r.A = min(1.4, max(0, a))
	return r
}

// gearEffectiveLv: the level a character with this kit fights at. Beginners (< 12) are not rated (everyone starts
// with the Knife); never below half the base level; very good gear at most +15% (max +8) over it.
func gearEffectiveLv(f, lv int, r gearRating) int {
	if lv < 12 {
		return lv
	}
	k := 0.4 // melee lives by its weapon
	switch f {
	case famArcher:
		k = 0.35
	case famMage, famAcolyte:
		k = 0.25
	}
	pen := (1-r.W)*float64(lv)*k + (1-r.A)*float64(lv)*0.18
	if bonus := min(0.15*float64(lv), 8); pen < -bonus {
		pen = -bonus
	}
	out := lv - int(pen+0.5)
	if pen < 0 {
		out = lv - int(pen-0.5)
	}
	return max(out, (lv+1)/2, 1)
}

// gearPowerLv (b.mu held; the powerLv hook): the kit-rated effective level, plus the solo performance step while
// the bot has no party (a party's step is applied by the party pickers).
func (b *Bot) gearPowerLv(lv int) int {
	p := gearEffectiveLv(family(b.Class), lv, rateGear(family(b.Class), lv, b.equipped))
	if b.PartyID == 0 && gearPerfOn() {
		c := &b.meter.sess
		p += soloPerf.offset(b.P.Name, b.MapName, isTown(b.MapName), 1, int(c.Kills), int(c.Fair), int(c.Deaths), time.Now())
	}
	p = max(1, min(p, lv+10))
	return b.gearGatedLv(b.gearPlan(), lv, p) // buildmilestones.go (EC39): the gear ceiling caps every picker
}

// ------------------------------------------------------------------ performance steps

const (
	perfWindow = 8 * time.Minute // minimum time on one map before a verdict
	perfMin    = -15
	perfMax    = 10
)

type perfState struct {
	m                   string
	at                  time.Time
	kills, fair, deaths int
	n                   int // members counted at the baseline
	off                 int
	changed             time.Time
}

type perfTracker struct {
	mu sync.Mutex
	s  map[string]*perfState
}

var soloPerf = &perfTracker{s: map[string]*perfState{}}
var partyPerf = &perfTracker{s: map[string]*perfState{}}

// perfVerdict: the step for one window. n = members, min = minutes, kills/fair/deaths = deltas.
// Down 3: deaths >= 1.5 per member-hour, or kills under 0.25 per member-minute with a death.
// Up 3: no deaths, >= 0.8 kills per member-minute and under half of them fair (monsters far below the level).
// Up 2: no deaths and fair kills fast (>= fastFair per member-minute: 4 solo, 3 in a party - kills are shared).
func perfVerdict(n int, minutes float64, kills, fair, deaths int) int {
	if n <= 0 || minutes <= 0 {
		return 0
	}
	bm := float64(n) * minutes
	kpm, fpm, dph := float64(kills)/bm, float64(fair)/bm, float64(deaths)/(bm/60)
	switch {
	case dph >= 1.5 || deaths > 0 && kpm < 0.25:
		return -3
	case deaths > 0:
		return 0
	case kills > 0 && kpm >= 0.8 && float64(fair) < 0.5*float64(kills):
		return 3
	}
	fast := 4.0
	if n > 1 {
		fast = 3.0
	}
	if fpm >= fast {
		return 2
	}
	return 0
}

// offset: the current step for key (a bot or a party leader) on map m, updating it from the counters (cumulative:
// kills, fair kills, deaths summed over n members). A new map or member count starts a new window; town time pauses it.
func (t *perfTracker) offset(key, m string, town bool, n, kills, fair, deaths int, now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.s[key]
	if s == nil {
		s = &perfState{}
		t.s[key] = s
	}
	if town || m == "" {
		s.m = "" // a town trip ends the window (its minutes are not farming)
		return s.off
	}
	if s.m != m || s.n != n || kills < s.kills || deaths < s.deaths || fair < s.fair {
		s.m, s.at, s.kills, s.fair, s.deaths, s.n = m, now, kills, fair, deaths, n
		return s.off
	}
	el := now.Sub(s.at)
	if el < perfWindow {
		return s.off
	}
	if v := perfVerdict(n, el.Minutes(), kills-s.kills, fair-s.fair, deaths-s.deaths); v != 0 {
		s.off = max(perfMin, min(perfMax, s.off+v))
		s.changed = now
	}
	s.at, s.kills, s.fair, s.deaths = now, kills, fair, deaths
	return s.off
}

// partyPerfOffset (no bot locks): the party's performance step (ms[0] = the leader), from the members' published
// meter counters on the leader's map.
func partyPerfOffset(ms []*Bot) int {
	if len(ms) == 0 || !gearPerfOn() {
		return 0
	}
	ls, ok := peek(ms[0].P.Name)
	if !ok || !ls.Online {
		return 0
	}
	n, k, f, d := 0, 0, 0, 0
	for _, b := range ms {
		s, ok := peek(b.P.Name)
		if !ok || !s.Online || !s.Known {
			continue
		}
		n++
		k, f, d = k+s.MKills, f+s.MFair, d+s.MDeaths
	}
	return partyPerf.offset(ms[0].P.Name, ls.Map, isTown(ls.Map), n, k, f, d, time.Now())
}

// partyPerfAdjust: the party pickers' hook - the weakest-member level and target filter moved by the party's step.
// down = only a step down (partyFitLvRaw: item-goal maps never stretch up), else only a step up (partyFitLv adds it
// on top of the raw level, which already carries any step down).
func partyPerfAdjust(ms []*Bot, minLv, fitLv int, down bool) (int, int) {
	o := partyPerfOffset(ms)
	if down {
		o = min(0, o)
	} else {
		o = max(0, o)
	}
	return max(1, minLv+o), max(1, fitLv+o)
}
