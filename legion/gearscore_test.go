package main

import (
	"testing"
	"time"
)

// real item stats (renewal item_db_equip.yml) of Ayla's party and the bug's solo bots, live 2026-11-14
var gsTestItems = map[int]*Item{
	1116: {ID: 1116, Name: "Katana", Atk: 60, WLv: 1, Slots: 3},
	1351: {ID: 1351, Name: "Battle Axe", Atk: 80, WLv: 1, Slots: 3},
	1604: {ID: 1604, Name: "Wand", Atk: 25, Matk: 45, WLv: 2, Slots: 2},
	1107: {ID: 1107, Name: "Blade", Atk: 53, WLv: 1, Slots: 3},
	1707: {ID: 1707, Name: "Great Bow", Atk: 50, WLv: 2, Slots: 2},
	1250: {ID: 1250, Name: "Jur", Atk: 125, WLv: 2, Slots: 2},
	1504: {ID: 1504, Name: "Mace", Atk: 37, WLv: 1, Slots: 3},
	1104: {ID: 1104, Name: "Falchion", Atk: 39, WLv: 1, Slots: 3},
	1207: {ID: 1207, Name: "Main Gauche", Atk: 43, WLv: 1, Slots: 3},
	1162: {ID: 1162, Name: "Broad Sword", Atk: 115, WLv: 3},
	2503: {ID: 2503, Name: "Muffler", Def: 8},
	2505: {ID: 2505, Name: "Manteau", Def: 13},
	2212: {ID: 2212, Name: "Eye Patch"},
	2220: {ID: 2220, Name: "Hat", Def: 2},
	2226: {ID: 2226, Name: "Cap", Def: 7},
	2228: {ID: 2228, Name: "Helm", Def: 13},
	2211: {ID: 2211, Name: "Bandana", Def: 2},
	2224: {ID: 2224, Name: "Goggles", Def: 5},
	2403: {ID: 2403, Name: "Shoes", Def: 10},
	2405: {ID: 2405, Name: "Boots", Def: 16},
	2401: {ID: 2401, Name: "Sandals", Def: 5},
	2307: {ID: 2307, Name: "Mantle", Def: 37},
	2305: {ID: 2305, Name: "Adventurer's Suit", Def: 20},
	2328: {ID: 2328, Name: "Wooden Mail", Def: 25},
	2314: {ID: 2314, Name: "Chain Mail", Def: 55},
	2501: {ID: 2501, Name: "Hood", Def: 4},
	2101: {ID: 2101, Name: "Guard", Def: 20},
	2609: {ID: 2609, Name: "Skull Ring"},
}

type gsPiece struct {
	id    int
	loc   uint32
	ref   int
	cards []int
}

func gsEquip(ps ...gsPiece) map[uint32]*InvItem {
	eq := map[uint32]*InvItem{}
	for _, p := range ps {
		it := &InvItem{ItemID: p.id, Loc: p.loc, Equipped: true, Refine: p.ref}
		copy(it.Cards[:], p.cards)
		eq[gearSlot(p.loc)] = it
	}
	return eq
}

func withGSItems(t *testing.T) {
	saved := map[int]*Item{}
	for id, x := range gsTestItems {
		saved[id] = items[id]
		items[id] = x
	}
	t.Cleanup(func() {
		for id, x := range saved {
			if x == nil {
				delete(items, id)
			} else {
				items[id] = x
			}
		}
	})
}

func TestGearEffectiveLvRealParty(t *testing.T) {
	withGSItems(t)
	cases := []struct {
		name   string
		f, lv  int
		eq     map[uint32]*InvItem
		lo, hi int
	}{
		// Ayla's party (engine API, live): every member lv74-79 in lv1-20 NPC gear
		{"Echobunny Knight 79", famSword, 79, gsEquip(gsPiece{1116, 34, 0, nil}, gsPiece{2503, 4, 0, nil}, gsPiece{2212, 512, 0, nil}, gsPiece{2220, 256, 0, nil}, gsPiece{2403, 64, 0, nil}, gsPiece{2307, 16, 0, nil}), 52, 58},
		{"Silent rogue Alchemist 79", famMerchant, 79, gsEquip(gsPiece{2609, 136, 0, nil}, gsPiece{2403, 64, 0, nil}, gsPiece{2501, 4, 0, nil}, gsPiece{2220, 256, 0, nil}, gsPiece{1351, 34, 0, []int{4006, 4020}}, gsPiece{2212, 512, 0, nil}, gsPiece{2328, 16, 0, nil}), 56, 64},
		{"Zen.fox Wizard 77", famMage, 77, gsEquip(gsPiece{2305, 16, 0, nil}, gsPiece{2501, 4, 0, nil}, gsPiece{2212, 512, 0, nil}, gsPiece{1604, 2, 0, nil}, gsPiece{2220, 256, 0, nil}, gsPiece{2403, 64, 0, nil}), 53, 59},
		{"Zendancer84 Priest 79", famAcolyte, 79, gsEquip(gsPiece{2101, 32, 0, nil}, gsPiece{2305, 16, 0, nil}, gsPiece{2609, 136, 0, nil}, gsPiece{2212, 512, 0, nil}, gsPiece{2220, 256, 0, nil}, gsPiece{1604, 2, 0, nil}, gsPiece{2403, 64, 0, nil}, gsPiece{2503, 4, 0, nil}), 58, 64},
		{"vex_x18 Crusader 78", famSword, 78, gsEquip(gsPiece{2101, 32, 0, nil}, gsPiece{1107, 2, 0, nil}, gsPiece{2403, 64, 0, nil}, gsPiece{2224, 768, 0, nil}, gsPiece{2328, 16, 0, nil}, gsPiece{2503, 4, 0, nil}), 51, 57},
		{"xXRinfangXx Hunter 74", famArcher, 74, gsEquip(gsPiece{2224, 768, 0, nil}, gsPiece{2403, 64, 0, nil}, gsPiece{2503, 4, 0, nil}, gsPiece{2307, 16, 0, nil}, gsPiece{1707, 34, 0, []int{4025}}), 58, 63},
		// bug b1794711435dty solo bots
		{"Moon.fox Monk 64", famAcolyte, 64, gsEquip(gsPiece{2305, 16, 0, nil}, gsPiece{1504, 2, 0, []int{4006}}, gsPiece{2503, 4, 0, nil}, gsPiece{2403, 64, 0, nil}), 44, 50},
		{"lily hunter64 Assassin 51", famThief, 51, gsEquip(gsPiece{2305, 16, 0, nil}, gsPiece{2401, 64, 0, nil}, gsPiece{1104, 34, 0, nil}), 32, 38},
		{"Mint.boy Mage 36", famMage, 36, gsEquip(gsPiece{2305, 16, 0, nil}, gsPiece{2211, 256, 0, nil}, gsPiece{2501, 4, 0, nil}, gsPiece{1207, 2, 0, nil}, gsPiece{2212, 512, 0, nil}, gsPiece{2403, 64, 0, nil}), 24, 28},
		// a well-kept lv60 swordsman: +7 Broad Sword, full lv-fitting set -> at or a bit above its level
		{"good swordsman 60", famSword, 60, gsEquip(gsPiece{1162, 34, 7, nil}, gsPiece{2314, 16, 4, nil}, gsPiece{2505, 4, 0, nil}, gsPiece{2405, 64, 0, nil}, gsPiece{2228, 256, 0, nil}), 62, 68}, // capped at +8
		{"beginner lv8 bare", famSword, 8, gsEquip(), 8, 8},
	}
	for _, c := range cases {
		r := rateGear(c.f, c.lv, c.eq)
		got := gearEffectiveLv(c.f, c.lv, r)
		t.Logf("%-28s W=%.2f A=%.2f cards=%d missing=%d -> effective lv %d", c.name, r.W, r.A, r.Cards, r.Missing, got)
		if got < c.lo || got > c.hi {
			t.Errorf("%s: gearEffectiveLv = %d, want %d-%d", c.name, got, c.lo, c.hi)
		}
	}
}

func TestPerfVerdict(t *testing.T) {
	cases := []struct {
		name                      string
		n                         int
		min                       float64
		kills, fair, deaths, want int
	}{
		{"party wiping", 6, 10, 20, 15, 3, -3},
		{"solo dies, few kills", 1, 10, 1, 1, 1, -3},
		{"solo lv64 on porings: fast, all unfair", 1, 10, 40, 0, 0, 3},
		{"solo fair kills fast", 1, 10, 45, 42, 0, 2},
		{"party fair kills at par", 6, 10, 120, 110, 0, 0},
		{"party fair kills fast", 6, 10, 200, 190, 0, 2},
		{"one death, decent kills: hold", 6, 10, 100, 90, 1, 0},
		{"empty map, no deaths: hold", 1, 10, 0, 0, 0, 0},
	}
	for _, c := range cases {
		if got := perfVerdict(c.n, c.min, c.kills, c.fair, c.deaths); got != c.want {
			t.Errorf("%s: perfVerdict = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestPerfTrackerSteps(t *testing.T) {
	tr := &perfTracker{s: map[string]*perfState{}}
	t0 := time.Now()
	// a lv64 on a beginner field: 4 kills/min, none fair, no deaths -> +3 per 8-min window, capped at perfMax
	k := 0
	off := tr.offset("Moon.fox", "gef_fild09", false, 1, 0, 0, 0, t0)
	for i := 1; i <= 6; i++ {
		k += 36
		off = tr.offset("Moon.fox", "gef_fild09", false, 1, k, 0, 0, t0.Add(time.Duration(i)*9*time.Minute))
	}
	if off != perfMax {
		t.Fatalf("easy farming: offset %d, want %d", off, perfMax)
	}
	// a new map starts a new window, the step is kept; dying there steps down
	t1 := t0.Add(2 * time.Hour)
	if o := tr.offset("Moon.fox", "gef_fild06", false, 1, k, 0, 0, t1); o != perfMax {
		t.Fatalf("new map: offset %d, want kept %d", o, perfMax)
	}
	if o := tr.offset("Moon.fox", "gef_fild06", false, 1, k+5, 5, 2, t1.Add(9*time.Minute)); o != perfMax-3 {
		t.Fatalf("deaths: offset %d, want %d", o, perfMax-3)
	}
	// town time pauses: no verdict from a window that included the town
	if o := tr.offset("Moon.fox", "geffen", true, 1, k+5, 5, 2, t1.Add(10*time.Minute)); o != perfMax-3 {
		t.Fatalf("town: offset %d", o)
	}
	// steps never pass perfMin
	tr2 := &perfTracker{s: map[string]*perfState{}}
	d := 0
	tr2.offset("p", "m", false, 5, 0, 0, 0, t0)
	for i := 1; i <= 10; i++ {
		d += 3
		off = tr2.offset("p", "m", false, 5, i*5, i*5, d, t0.Add(time.Duration(i)*9*time.Minute))
	}
	if off != perfMin {
		t.Fatalf("party dying: offset %d, want %d", off, perfMin)
	}
}

// the guide spots a level qualifies for (strategy.go guideSpots range test only; the live pickers also check crowd,
// fit share, swarms and routes) - logs what the real party / solo levels now get, before vs after
func TestGearScoreMapPicks(t *testing.T) {
	withGSItems(t)
	spots := func(lv int, party bool) []string {
		var out []string
		for _, g := range guideSpots {
			if lv >= g.Min && lv <= g.Max+3 && (party || !g.Party) {
				out = append(out, g.Map)
			}
		}
		return out
	}
	// Ayla's party: weakest member (Zen.fox, fragile -6) + healer 4 + tank 3 + 4 online 1
	party := []struct {
		f, lv int
		eq    map[uint32]*InvItem
		frag  bool
	}{
		{famSword, 79, gsEquip(gsPiece{1116, 34, 0, nil}, gsPiece{2503, 4, 0, nil}, gsPiece{2220, 256, 0, nil}, gsPiece{2403, 64, 0, nil}, gsPiece{2307, 16, 0, nil}), false},
		{famMerchant, 79, gsEquip(gsPiece{2403, 64, 0, nil}, gsPiece{2501, 4, 0, nil}, gsPiece{2220, 256, 0, nil}, gsPiece{1351, 34, 0, []int{4006, 4020}}, gsPiece{2328, 16, 0, nil}), true},
		{famMage, 77, gsEquip(gsPiece{2305, 16, 0, nil}, gsPiece{2501, 4, 0, nil}, gsPiece{1604, 2, 0, nil}, gsPiece{2220, 256, 0, nil}, gsPiece{2403, 64, 0, nil}), true},
		{famAcolyte, 79, gsEquip(gsPiece{2101, 32, 0, nil}, gsPiece{2305, 16, 0, nil}, gsPiece{2220, 256, 0, nil}, gsPiece{1604, 2, 0, nil}, gsPiece{2403, 64, 0, nil}, gsPiece{2503, 4, 0, nil}), false},
		{famSword, 78, gsEquip(gsPiece{2101, 32, 0, nil}, gsPiece{1107, 2, 0, nil}, gsPiece{2403, 64, 0, nil}, gsPiece{2224, 768, 0, nil}, gsPiece{2328, 16, 0, nil}, gsPiece{2503, 4, 0, nil}), false},
		{famArcher, 74, gsEquip(gsPiece{2224, 768, 0, nil}, gsPiece{2403, 64, 0, nil}, gsPiece{2503, 4, 0, nil}, gsPiece{2307, 16, 0, nil}, gsPiece{1707, 34, 0, []int{4025}}), false},
	}
	minNew, minOld := 999, 999
	for _, m := range party {
		n := gearEffectiveLv(m.f, m.lv, rateGear(m.f, m.lv, m.eq))
		var w *Item
		if e := m.eq[0x2]; e != nil {
			w = items[e.ItemID]
		}
		def := 0
		if e := m.eq[0x10]; e != nil {
			def = items[e.ItemID].Def
		}
		o := effectiveLv(m.f, m.lv, w, def)
		if m.frag {
			n, o = n-partyFragileLv, o-partyFragileLv
		}
		minNew, minOld = min(minNew, n), min(minOld, o)
	}
	pNew, pOld := min(minNew+8, 74), min(minOld+8, 74)
	t.Logf("Ayla's engine party: party level old %d -> spots %v", pOld, spots(pOld, true))
	t.Logf("Ayla's engine party: party level new %d -> spots %v", pNew, spots(pNew, true))
	if pNew > 66 {
		t.Errorf("a lv74-79 party in lv1-20 gear rated %d: should farm well under its base level", pNew)
	}
	solo := []struct {
		name  string
		f, lv int
		eq    map[uint32]*InvItem
	}{
		{"Moon.fox", famAcolyte, 64, gsEquip(gsPiece{2305, 16, 0, nil}, gsPiece{1504, 2, 0, []int{4006}}, gsPiece{2503, 4, 0, nil}, gsPiece{2403, 64, 0, nil})},
		{"lily hunter64", famThief, 51, gsEquip(gsPiece{2305, 16, 0, nil}, gsPiece{2401, 64, 0, nil}, gsPiece{1104, 34, 0, nil})},
		{"Mint.boy", famMage, 36, gsEquip(gsPiece{2305, 16, 0, nil}, gsPiece{2211, 256, 0, nil}, gsPiece{2501, 4, 0, nil}, gsPiece{1207, 2, 0, nil}, gsPiece{2403, 64, 0, nil})},
	}
	for _, s := range solo {
		n := gearEffectiveLv(s.f, s.lv, rateGear(s.f, s.lv, s.eq))
		up := min(n+perfMax, s.lv+10)
		t.Logf("%s lv%d: gear-rated %d -> %v; after easy-farming steps (+%d) %d -> %v", s.name, s.lv, n, spots(n, false), perfMax, up, spots(up, false))
	}
}
