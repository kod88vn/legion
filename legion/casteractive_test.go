package main

import (
	"testing"
	"time"
)

func TestCasterSpellElement(t *testing.T) {
	wiz := map[int]int{wzJupitel: 10, mgFire: 10, mgCold: 10, mgLight: 10, wzEarthSpike: 3}
	learned := func(id int) int { return wiz[id] }
	cases := []struct {
		elem string
		want int
	}{
		{"Undead", wzJupitel},  // Soldier Skeleton: Jupitel (not resisted)
		{"Water", wzJupitel},   // Wind beats Water
		{"Wind", wzEarthSpike}, // resists Wind -> Earth Spike
		{"Earth", mgFire},      // resists Wind (0.5) -> Fire Bolt
		{"Neutral", wzJupitel},
	}
	for _, c := range cases {
		if id, lv := casterSpell(&Mob{Element: c.elem}, learned); id != c.want || lv <= 0 {
			t.Errorf("%s: got %d lv%d, want %d", c.elem, id, lv, c.want)
		}
	}
	mage := map[int]int{mgCold: 7, mgLight: 5}
	if id, lv := casterSpell(&Mob{Element: "Undead"}, func(id int) int { return mage[id] }); id != mgCold || lv != 7 {
		t.Errorf("mage without Fire Bolt/Jupitel: got %d lv%d, want Cold Bolt 7", id, lv)
	}
	if id, _ := casterSpell(&Mob{Element: "Wind"}, func(id int) int { return map[int]int{wzJupitel: 5}[id] }); id != wzJupitel {
		t.Errorf("only Jupitel known: got %d", id)
	}
	if id, _ := casterSpell(nil, func(int) int { return 0 }); id != 0 {
		t.Errorf("nothing learned: got %d", id)
	}
}

func TestPickBoltTarget(t *testing.T) {
	e := func(id uint32) *Entity { return &Entity{ID: id} }
	c := []boltCand{
		{e: e(1), x: 20, y: 0, fought: true, los: true},  // handed, but 20 cells off
		{e: e(2), x: 3, y: 0, los: true},                 // nobody fights it: never (pulls more)
		{e: e(3), x: 8, y: 0, onMember: true, los: true}, // on a member
		{e: e(4), x: 6, y: 0, onSupp: true, los: true},   // on the priest: first
		{e: e(5), x: 2, y: 0, onSupp: true, los: false},  // no line
		{e: e(6), x: 4, y: 0, fought: true, los: true},
	}
	if i := pickBoltTarget(c, 0, 0, 9, 1); i != 3 {
		t.Fatalf("handed out of range: got %d, want 3 (monster on the priest)", i)
	}
	c[0].x = 9
	if i := pickBoltTarget(c, 0, 0, 9, 1); i != 0 {
		t.Fatalf("handed in range: got %d, want 0", i)
	}
	if i := pickBoltTarget([]boltCand{c[1]}, 0, 0, 9, 99); i != -1 {
		t.Fatalf("only an unfought monster: got %d, want -1", i)
	}
	// nearest wins among equals
	two := []boltCand{{e: e(7), x: 7, y: 0, fought: true, los: true}, {e: e(8), x: 2, y: 2, fought: true, los: true}}
	if i := pickBoltTarget(two, 0, 0, 9, 99); i != 1 {
		t.Fatalf("nearest: got %d, want 1", i)
	}
}

func TestCasterSpendOK(t *testing.T) {
	if !casterSpendOK(1399, 1399, 47, 35) {
		t.Error("full SP must cast")
	}
	if casterSpendOK(400, 1399, 47, 35) {
		t.Error("under the 35% floor: leave it to the old path")
	}
	if casterSpendOK(100, 300, 70, 30) { // 100-70 < reserve 45
		t.Error("cast would eat the area-spell reserve")
	}
}

func TestWallCell(t *testing.T) {
	if x, y, ok := wallCell(10, 10, 14, 10); !ok || x != 11 || y != 10 {
		t.Errorf("wall in front of the monster: %d,%d %v", x, y, ok)
	}
	if _, _, ok := wallCell(10, 10, 11, 10); ok {
		t.Error("already on the priest: a wall can't stop it")
	}
	if _, _, ok := wallCell(0, 0, 9, 9); ok {
		t.Error("too far to matter")
	}
}

// TestCasterSPNotFullInFight is the CL16 sanity metric: a Wizard (1399 SP, ~200 SP/min regen standing) in a
// 10-minute party fight with a target in range whenever its skill is ready must spend its SP - the share of fight
// time at >=95% SP stays low, and it keeps casting (no rest at full SP). Old behaviour (measured): 5.3 casts/min,
// SP 93-100% the whole fight.
func TestCasterSPNotFullInFight(t *testing.T) {
	casterMeter.Lock()
	casterMeter.m = map[string]*casterSample{}
	casterMeter.logAt = time.Time{}
	casterMeter.Unlock()
	start := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	sp, maxSP := 1399, 1399
	regenPerSec := 200.0 / 60
	var ready time.Time
	acc := 0.0
	casts := 0
	for ms := 0; ms < 4*60*1000; ms += 250 { // 4 min (under the 5-min log/reset span)
		now := start.Add(time.Duration(ms) * time.Millisecond)
		acc += regenPerSec / 4
		if acc >= 1 {
			sp = min(maxSP, sp+int(acc))
			acc -= float64(int(acc))
		}
		casterMeterNote("wiz", sp, maxSP, ready, now)
		if !now.Before(ready) && casterSpendOK(sp, maxSP, 47, 35) {
			sp -= 47
			casts++
			ready = now.Add(time.Duration(casterCastMs(wzJupitel, 10)+150) * time.Millisecond)
		}
	}
	casterMeter.Lock()
	s := casterMeter.m["wiz"]
	casterMeter.Unlock()
	share, cpm := s.casterFullShare()
	if share > 0.15 {
		t.Errorf("SP >= 95%% for %.0f%% of the fight, want <= 15%%", 100*share)
	}
	if cpm < 8 {
		t.Errorf("%.1f casts per fight minute, want >= 8", cpm)
	}
	if line := func() string { casterMeter.Lock(); defer casterMeter.Unlock(); return casterMeterLineLocked(start) }(); line == "" {
		t.Error("no CASTERS line for a 4-minute fight")
	}
	// the old pattern: SP pinned full, few casts -> the metric flags it
	old := &casterSample{fight: 8 * time.Minute, full: 7 * time.Minute, casts: 42}
	if sh, c := old.casterFullShare(); sh < 0.8 || c > 6 {
		t.Errorf("old pattern not flagged: %.2f %.1f", sh, c)
	}
}
