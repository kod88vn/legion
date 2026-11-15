package main

// buildmilestones.go (EC39, owner 2026-11-15: "list the stats, gears, skills at certain level range for bots to
// achieve ... Farm certain maps unless gear requirements are met"). Every build has milestones per level band (the
// bands of buildgear.go: 1-20, 21-40, 41-60, 61-80, 81-99). A milestone is the gear a bot should wear by the band's
// first level: a weapon attack (MATK for casters), a body armor DEF and a number of cards. It is read from the build's
// own weapon and armor routes (builds.go Weapons / Armors): the strongest route item reached by the band's first
// level (the weakest route item when none is reached). Stats and skills stay in the Build's Stats / Skills.
//
// Gate: a bot at level 21+ whose worn gear misses the current band's milestone is capped at the ceiling of the last
// band its gear meets (20 when it meets none). Every map picker reads the effective level (gearpower.go powerLv), so
// the cap moves the solo pickers, the party pickers (the published Gate, party.go) and the guild leaders together.
// Band 1-20 is never gated: everyone starts with the Knife. The gear goal is the unmet milestone's item (EC38: the
// next step); chooseHuntingGround farms its drop map every time while gated. tuning gear_gate_on, default ON.
//
// Dashboard: milestoneTable() is the one table (the engine and the Builds tab read it).

import (
	"sync"
	"time"
)

// milestoneTolPct: a worn piece meets its milestone at this percentage of the value (85 = 15% under is still enough).
const milestoneTolPct = 85

type gearMilestone struct {
	Min, Max   int  // level band
	Magic      bool // caster build: the weapon's MATK is the value
	Weapon     int  // weapon attack (MATK for casters) to hold, raw item value
	Armor      int  // body armor DEF to wear, raw item value
	Cards      int  // cards in the worn gear by the band (0 for 1-40, 1 for 41-60, 2 from 61)
	WeaponItem int  // item id the weapon milestone reads (0 = none in the route)
	ArmorItem  int  // item id the armor milestone reads (0 = none in the route)
}

func gearGateOn() bool { return tune().GearGateOn }

// milestoneCache: per build; the guide file (data/buildgear.json) is re-checked every 5 min like buildGearTable.
var milestoneCache = struct {
	sync.Mutex
	m       map[string][]gearMilestone
	checked time.Time
	mod     time.Time
}{m: map[string][]gearMilestone{}}

// routePickAt: the item of a route (weakest first) that a character reaches by lv with the highest value; the first
// known item when none is reached (value: Atk, Matk or Def of the item).
func routePickAt(route []int, lv int, value func(*Item) int) *Item {
	var best, first *Item
	for _, id := range route {
		x := items[id]
		if x == nil {
			continue
		}
		if first == nil {
			first = x
		}
		if x.MinLv <= lv && (best == nil || value(x) > value(best)) {
			best = x
		}
	}
	if best == nil {
		return first
	}
	return best
}

func cardsForBand(min int) int {
	switch {
	case min >= 61:
		return 2
	case min >= 41:
		return 1
	}
	return 0
}

// milestonesFor: the milestones of one build, one per band (not cached: see milestonesOf). The source is the guide's
// band lists in data/buildgear.json (the same bands the Builds tab and the exporter read: the weapon and armor the
// guide lists for the band); the build's own routes (builds.go Weapons / Armors) fill what the guide leaves out.
func milestonesFor(p *Build, guide *bgBuild) []gearMilestone {
	lookupOnce.Do(loadLookup)
	magic := buildStyle(p) == "magic"
	wval := func(x *Item) int {
		if magic {
			return x.Matk
		}
		return x.Atk
	}
	var out []gearMilestone
	for _, be := range bandEdges {
		m := gearMilestone{Min: be[0], Max: be[1], Magic: magic, Cards: cardsForBand(be[0])}
		var gw, ga []int
		if guide != nil {
			for i := range guide.Bands {
				g := &guide.Bands[i]
				if g.Min <= be[1] && g.Max >= be[0] {
					gw, ga = append(gw, g.Weapon...), append(ga, g.Armor...)
				}
			}
		}
		wroute, aroute := p.Weapons, p.Armors
		if len(gw) > 0 {
			wroute = gw
		}
		if len(ga) > 0 {
			aroute = ga
		}
		if w := routePickAt(wroute, be[0], wval); w != nil {
			m.WeaponItem, m.Weapon = w.ID, wval(w)
		}
		if a := routePickAt(aroute, be[0], func(x *Item) int { return x.Def }); a != nil {
			m.ArmorItem, m.Armor = a.ID, a.Def
		}
		out = append(out, m)
	}
	return out
}

// milestonesOf: the cached milestones of a build. The guide file is re-read at most every 5 min; a changed file drops
// the cache.
func milestonesOf(p *Build) []gearMilestone {
	buildGear.Lock()
	changed := false
	if time.Since(milestoneCache.checked) > 5*time.Minute {
		milestoneCache.checked = time.Now()
		changed = loadBuildGearFile()
	}
	var guide *bgBuild
	if buildGear.file != nil {
		guide = buildGear.file.Builds[p.ID]
		if guide == nil {
			guide = buildGear.file.Builds[p.base()]
		}
	}
	mod := buildGear.fileMod
	buildGear.Unlock()

	milestoneCache.Lock()
	defer milestoneCache.Unlock()
	if changed || !mod.Equal(milestoneCache.mod) {
		milestoneCache.m = map[string][]gearMilestone{}
		milestoneCache.mod = mod
	}
	if ms, ok := milestoneCache.m[p.ID]; ok {
		return ms
	}
	ms := milestonesFor(p, guide)
	milestoneCache.m[p.ID] = ms
	return ms
}

// milestoneTable: every build's milestones (build id -> bands), for the dashboard export and tests.
func milestoneTable() map[string][]gearMilestone {
	out := map[string][]gearMilestone{}
	for id, p := range builds {
		out[id] = milestonesOf(p)
	}
	return out
}

// gearMeets (b.mu held): the worn weapon, body armor and cards reach the band's milestone.
func gearMeets(b *Bot, f, lv int, m *gearMilestone) bool {
	if m.Weapon > 0 {
		e := b.equipped[0x2]
		if e == nil || items[e.ItemID] == nil {
			return false
		}
		x := items[e.ItemID]
		v := x.Atk
		if m.Magic {
			v = x.Matk
		} else {
			v += refineAtk(max(1, x.WLv), e.Refine)
		}
		if v*100 < m.Weapon*milestoneTolPct {
			return false
		}
	}
	if m.Armor > 0 {
		e := b.equipped[0x10]
		if e == nil || items[e.ItemID] == nil {
			return false
		}
		if (items[e.ItemID].Def+e.Refine)*100 < m.Armor*milestoneTolPct {
			return false
		}
	}
	if m.Cards > 0 && rateGear(f, lv, b.equipped).Cards < m.Cards {
		return false
	}
	return true
}

// gearGateOf (b.mu held): the effective-level ceiling the bot's gear allows at its base level lv, and the first unmet
// milestone (nil = none unmet). 0 = no gate (flag off, no build, or band 1-20).
func gearGateOf(b *Bot, p *Build, lv int) (int, *gearMilestone) {
	if p == nil || lv < 21 || !gearGateOn() {
		return 0, nil
	}
	f := family(b.Class)
	ms := milestonesOf(p)
	gate := 20
	for i := range ms {
		m := &ms[i]
		if m.Min <= 20 || lv < m.Min {
			continue
		}
		if !gearMeets(b, f, lv, m) {
			return gate, m
		}
		gate = m.Max
	}
	return gate, nil
}

// gearGatedLv (b.mu held): the raw effective level, capped at the gear ceiling of build p (EC39).
func gearGatedLv(b *Bot, p *Build, lv, raw int) int {
	if g, _ := gearGateOf(b, p, lv); g > 0 && raw > g {
		return g
	}
	return raw
}

// gearGate (b.mu held): the ceiling and the unmet milestone of this bot's own build.
func (b *Bot) gearGate(lv int) (int, *gearMilestone) {
	return gearGateOf(b, b.gearPlan(), lv)
}

// gearPlan (b.mu held): the bot's build, nil when it has no character spec yet (tests, a bot not set up).
func (b *Bot) gearPlan() *Build {
	if b.cur == nil && (b.P == nil || len(b.P.Chars) == 0) {
		return nil
	}
	return b.plan()
}

// partyGearGate: the lowest gear ceiling of the online, known members (0 = none). A party's map follows its
// weakest-geared member (EC39); the ceilings come from the published Gate (party.go publish).
func partyGearGate(ms []*Bot) int {
	g := 0
	for _, b := range ms {
		s, ok := peek(b.P.Name)
		if !ok || !s.Online || !s.Known || s.Gate <= 0 {
			continue
		}
		if g == 0 || s.Gate < g {
			g = s.Gate
		}
	}
	return g
}
