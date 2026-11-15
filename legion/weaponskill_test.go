package main

import (
	"testing"
	"time"
)

func TestArrowShowerCluster(t *testing.T) {
	grp := []aoeMob{{e: &Entity{ID: 1}, x: 10, y: 10}, {e: &Entity{ID: 2}, x: 11, y: 10}, {e: &Entity{ID: 3}, x: 10, y: 11}}
	if _, _, _, ok := bestCluster(grp, 4, 10, 9, 1, arrowShowerMin, true); !ok {
		t.Fatal("3 monsters in a 3x3 should get Arrow Shower")
	}
	if _, _, _, ok := bestCluster(grp[:2], 4, 10, 9, 1, arrowShowerMin, true); ok {
		t.Fatal("2 monsters: Double Strafe, not Arrow Shower")
	}
	if _, _, _, ok := bestCluster(grp, 30, 30, 9, 1, arrowShowerMin, true); ok {
		t.Fatal("out of reach")
	}
	spread := []aoeMob{{e: &Entity{}, x: 10, y: 10}, {e: &Entity{}, x: 12, y: 10}, {e: &Entity{}, x: 10, y: 12}}
	if _, _, _, ok := bestCluster(spread, 4, 10, 9, 1, arrowShowerMin, true); ok {
		t.Fatal("spread 2 cells apart is outside a 3x3")
	}
	if _, _, _, ok := bestCluster(spread, 4, 10, 9, arrowShowerRadius(6), arrowShowerMin, true); !ok {
		t.Fatal("lv6+ Arrow Shower covers 5x5")
	}
	ks := append([]aoeMob{}, grp...)
	ks[1].foreign = true
	if _, _, _, ok := bestCluster(ks, 4, 10, 9, 1, arrowShowerMin, false); ok {
		t.Fatal("no kill stealing")
	}
	risky := append([]aoeMob{}, grp...)
	risky[0].risky = true
	if _, _, _, ok := bestCluster(risky, 4, 10, 9, 1, arrowShowerMin, true); ok {
		t.Fatal("solo: no area shot on a boss group")
	}
	if _, _, _, ok := bestCluster(risky, 4, 10, 9, 1, arrowShowerMin, false); !ok {
		t.Fatal("in a party the risky group may be shot")
	}
}

func TestBuffLevelFitsSP(t *testing.T) {
	ic := func(lv int) int { return 20 + 5*lv }
	if lv := buffLevel(10, ic, 110, 110); lv != 4 {
		t.Fatalf("archer 110/110 SP: Improve Concentration lv %d, want 4", lv)
	}
	if lv := buffLevel(10, ic, 250, 250); lv != 10 {
		t.Fatalf("hunter 250 SP: lv %d, want 10", lv)
	}
	if lv := buffLevel(10, ic, 60, 110); lv != 0 {
		t.Fatalf("low SP: no buff, got lv %d", lv)
	}
}

func TestWorthlessPlants(t *testing.T) {
	if !(&Mob{HP: 5}).worthless() {
		t.Fatal("a plant (5 HP, no EXP) is worthless")
	}
	if (&Mob{HP: 5, Exp: 2}).worthless() || (&Mob{}).worthless() || (&Mob{HP: 500}).worthless() {
		t.Fatal("real monsters are not worthless")
	}
}

// every plan's skill goals must be in the class line's skill tree order: a goal must not be listed before its
// lower level of the same skill (prefix-compatible plans)
func TestPlanGoalsMonotonic(t *testing.T) {
	for id, b := range builds {
		seen := map[int]int{}
		for _, g := range b.Skills {
			if g.Lv < seen[g.ID] {
				t.Errorf("%s: skill %d goal %d after %d", id, g.ID, g.Lv, seen[g.ID])
			}
			seen[g.ID] = max(seen[g.ID], g.Lv)
		}
	}
}

func TestPecoRental(t *testing.T) {
	if x, y, p, ok := pecoBreeder(7); !ok || x != 55 || y != 350 || p != 2500 {
		t.Fatal("Knight breeder prontera 55,350 2500z")
	}
	if _, _, p, ok := pecoBreeder(4015); !ok || p != 3500 {
		t.Fatal("Paladin rents at the Crusader breeder")
	}
	if _, _, _, ok := pecoBreeder(11); ok {
		t.Fatal("Hunters don't ride")
	}
	b := &Bot{P: &Persona{}, Class: 7, Zeny: 10000, optionSeen: true, buffAt: map[string]time.Time{},
		skills: map[int]*SkillInfo{63: {ID: 63, Lv: 1}, 64: {ID: 64, Lv: 5}}}
	now := time.Now()
	if _, _, ok := b.wantsPeco(now); !ok {
		t.Fatal("a Knight with Riding + Cavalier 5 and money wants a Peco")
	}
	b.option = optRiding
	if _, _, ok := b.wantsPeco(now); ok {
		t.Fatal("already riding")
	}
	b.option, b.skills[64].Lv = 0, 1
	if _, _, ok := b.wantsPeco(now); !ok {
		t.Fatal("Cavalier Mastery 1 still rides (owner: Knights ride; Riding 1 is all rAthena needs)")
	}
}

func TestKeyBuffSPFloor(t *testing.T) {
	thq := func(lv int) int { return 10 + 4*lv }
	// a Knight at 120/234 SP: Two-Hand Quicken 10 (50 SP) leaves 29.9% - lv9 (46) leaves 31%
	if lv := buffLevelLeft(10, thq, 120, 234, skillSPFloor); lv != 9 {
		t.Fatalf("THQ at 120/234 SP: lv %d, want 9", lv)
	}
	if lv := buffLevel(10, thq, 120, 234); lv != 0 {
		t.Fatalf("the old 60%% rule never let it through: lv %d", lv)
	}
}

func TestCrowdTable(t *testing.T) {
	seen := map[int]bool{}
	for _, c := range crowdSkills {
		if seen[c.id] || c.radius == nil || c.reach <= 0 || c.fam == famMage {
			t.Errorf("bad crowd entry %d", c.id)
		}
		seen[c.id] = true
	}
	for _, id := range []int{acShower, 62, 57, 153, 79} {
		if !seen[id] {
			t.Errorf("crowd skill %d missing", id)
		}
	}
	// a Knight next to a pack: Bowling Bash on the monster in the middle
	grp := []aoeMob{{e: &Entity{ID: 1}, x: 11, y: 10}, {e: &Entity{ID: 2}, x: 12, y: 10}, {e: &Entity{ID: 3}, x: 13, y: 10}}
	_, _, ce, ok := bestCluster(grp, 10, 10, 2, 2, 3, true)
	if !ok || ce.ID != 1 && ce.ID != 2 {
		t.Fatalf("Bowling Bash centre: %v %v", ok, ce)
	}
}
