package main

import (
	"os"
	"testing"
)

// loadFitFixtures loads the real rAthena rows the fit reads (fit_fixture_test.go: Arrows 1750-1757, the Hydra Card 4035,
// Z Stone 9 4966 and Ice Crystal Orb 312087) and the real mobs and spawns of Payon Cave (mob_db.yml element and race;
// npc spawn lines of pay_dun01). The ids are the server's.
func loadFitFixtures(t *testing.T) {
	t.Helper()
	cardBonOnce.Do(loadCardBonuses) // the default rathena path has no DB here: a harmless no-op
	if cardBon == nil {
		cardBon = map[int]*cardBonus{}
	}
	dir := t.TempDir()
	ammo, cards := dir+"/item_db_ammo.yml", dir+"/item_db_cards.yml"
	if err := os.WriteFile(ammo, []byte(fitAmmoRowsYML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cards, []byte(fitCardRowsYML), 0644); err != nil {
		t.Fatal(err)
	}
	loadItemDB(ammo, cards)
	loadAmmoElements(ammo)
	loadBonusFile(cards)
	setPotionItems()
	for _, m := range []*Mob{
		{ID: 1015, Name: "Zombie", Element: "Undead", Race: "Undead", Level: 17},
		{ID: 1076, Name: "Skeleton", Element: "Undead", Race: "Undead", Level: 27},
		{ID: 1028, Name: "Soldier Skeleton", Element: "Undead", Race: "Undead", Level: 34},
		{ID: 1016, Name: "Archer Skeleton", Element: "Undead", Race: "Undead", Level: 50},
		{ID: 2649, Name: "Furious Soldier Skeleton", Element: "Undead", Race: "Undead", Level: 60},
		{ID: 1111, Name: "Drainliar", Element: "Dark", Race: "Brute", Level: 47},
		{ID: 1116, Name: "Eggyra", Element: "Ghost", Race: "Formless", Level: 53},
		{ID: 1078, Name: "Red Plant", Element: "Earth", Race: "Plant", Level: 1},
		{ID: 2248, Name: "Golden Poring", Element: "Water", Race: "Plant", Level: 15},
		{ID: 1023, Name: "Ork Warrior", Element: "Earth", Race: "Demihuman", Level: 22},
		{ID: 1177, Name: "Poring", Element: "Neutral", Race: "Plant", Level: 1},
	} {
		mobs[m.ID] = m
	}
	// pay_dun01 (Payon Cave 2): the real spawn lines of the npc file, counts summed as the loader does (Soldier Skeleton
	// 50 + 53, Archer Skeleton 30 + 32, Furious Soldier Skeleton 2, Drainliar 5, Eggyra 15 + 16, Red Plant 10, Golden
	// Poring 3): undead is about 77% of the spawns.
	spawnsByMap["pay_dun01"] = []Spawn{
		{Map: "pay_dun01", MobID: 1028, Count: 50}, {Map: "pay_dun01", MobID: 1028, Count: 53},
		{Map: "pay_dun01", MobID: 1016, Count: 30}, {Map: "pay_dun01", MobID: 1016, Count: 32},
		{Map: "pay_dun01", MobID: 2649, Count: 2}, {Map: "pay_dun01", MobID: 1111, Count: 5},
		{Map: "pay_dun01", MobID: 1116, Count: 15}, {Map: "pay_dun01", MobID: 1116, Count: 16},
		{Map: "pay_dun01", MobID: 1078, Count: 10}, {Map: "pay_dun01", MobID: 2248, Count: 3},
	}
	// test-only spawn lists of real mobs: a neutral map and an all-Demihuman map
	spawnsByMap["zz_neutral_fit"] = []Spawn{{Map: "zz_neutral_fit", MobID: 1177, Count: 10}}
	spawnsByMap["zz_orc_fit"] = []Spawn{{Map: "zz_orc_fit", MobID: 1023, Count: 20}}
	mapFitForget("pay_dun01", "zz_neutral_fit", "zz_orc_fit")
}

// mapFitForget: the hunt-fit cache of these maps is rebuilt from their spawn lists
func mapFitForget(maps ...string) {
	huntMu.Lock()
	defer huntMu.Unlock()
	for _, m := range maps {
		delete(huntCache, m)
	}
}

// withFitOff runs f with tuning fit_buy_off true, then restores the tuning in force
func withFitOff(f func()) {
	old := tune()
	nt := *old
	nt.FitBuyOff = true
	curTuning.Store(&nt)
	defer curTuning.Store(old)
	f()
}

// the arrow element comes from the item's own script (bonus bAtkEle), by id: 1755 is "Arrow of Wind" (Wind), 1757 the
// Immaterial Arrow (Ghost), 1751 the Silver Arrow (Holy). The plain Arrow has none.
func TestArrowElementsFromItemScripts(t *testing.T) {
	loadFitFixtures(t)
	want := map[int]string{1750: "", 1751: "Holy", 1752: "Fire", 1754: "Water", 1755: "Wind", 1756: "Earth", 1757: "Ghost"}
	for id, e := range want {
		if ammoEle[id] != e {
			t.Errorf("ammoEle[%d] = %q, want %q", id, ammoEle[id], e)
		}
	}
}

// Payon Cave 2 is about 77% undead: a Silver Arrow (Holy: 125% against Undead and Dark, 75% against Poison, 100%
// otherwise) gains about 20% over a plain Arrow. Tool Dealer#pay2 (payon_in02) sells Arrow at 1z and Silver Arrow at 5z.
func TestPayonUndeadBuysSilverArrows(t *testing.T) {
	loadFitFixtures(t)
	if got := ammoFit(1751, "pay_dun01"); got < 1.18 || got > 1.22 {
		t.Fatalf("Silver Arrow fit on pay_dun01 = %.3f, want about 1.20", got)
	}
	pk := shopPk(1, 1750, 5, 1751)
	if id, pr := arrowChoice(pk, "pay_dun01", 5000); id != 1751 || pr != 5 {
		t.Fatalf("pay_dun01 with 5000z: picked %d at %d, want Silver Arrow 1751 at 5", id, pr)
	}
	// a plain Arrow stays on a neutral map (Holy against neutral is 100%: 4z extra buys no damage)
	if id, _ := arrowChoice(pk, "zz_neutral_fit", 5000); id != 1750 {
		t.Fatalf("neutral map: picked %d, want the plain Arrow 1750", id)
	}
	// too poor for a useful elemental stock (30 arrows at 5z): plain
	if id, _ := arrowChoice(pk, "pay_dun01", 100); id != 1750 {
		t.Fatalf("100z: picked %d, want the plain Arrow", id)
	}
}

// an elemental arrow whose gain does not pay its extra price stays out: a Fire Arrow (125% against Undead) on a map that
// is a third undead and two thirds Dark gains less than the 4% per extra zeny, so the plain stock is bought
func TestElementalArrowMustPayItsPrice(t *testing.T) {
	loadFitFixtures(t)
	spawnsByMap["zz_mixed_fit"] = []Spawn{{Map: "zz_mixed_fit", MobID: 1015, Count: 10}, {Map: "zz_mixed_fit", MobID: 1111, Count: 20}}
	defer mapFitForget("zz_mixed_fit")
	pk := shopPk(1, 1750, 5, 1752) // Fire Arrow at 5z (sold in alb2trea, ein_in01 and lhz_in03 per the npc files)
	if id, _ := arrowChoice(pk, "zz_mixed_fit", 5000); id != 1750 {
		t.Fatalf("1/3 undead, 2/3 Dark: picked %d, want the plain Arrow (gain %.3f does not pay 4z)", id, ammoFit(1752, "zz_mixed_fit")-1)
	}
}

// the Demihuman race card (Hydra Card: bonus2 bAddRace,RC_DemiHuman,20) matches the server's "Demihuman" (mob_db);
// RC_All (Z Stone 9) hits every monster; Ele_All (Ice Crystal Orb: bAddEle Ele_All 4) too. Undead-race cards are not
// used here.
func TestDemihumanAndAllBonusesMatch(t *testing.T) {
	loadFitFixtures(t)
	if v := cardBon[4035].race["demihuman"]; v != 20 {
		t.Fatalf("Hydra Card demihuman race bonus = %d, want 20 (keys are lower-case)", v)
	}
	if v := cardBon[4966].race["all"]; v != 1 {
		t.Fatalf("Z Stone 9 RC_All bonus = %d, want 1", v)
	}
	if v := cardBon[312087].ele["all"]; v != 4 {
		t.Fatalf("Ice Crystal Orb Ele_All bonus = %d, want 4", v)
	}
	orc := &Bot{Goal: Goal{Map: "zz_orc_fit"}}
	if v := orc.huntFitValue(cardBon[4035]); v != 40 {
		t.Fatalf("Hydra Card on an all-Demihuman map: value %d, want 40 (20 x share 1.0 x 2)", v)
	}
	pay := &Bot{Goal: Goal{Map: "pay_dun01"}}
	if v := pay.huntFitValue(cardBon[4035]); v != 0 {
		t.Fatalf("Hydra Card on Payon Cave 2 (no Demihuman): value %d, want 0", v)
	}
	if v := pay.huntFitValue(cardBon[4966]); v <= 0 {
		t.Fatalf("Z Stone 9 (RC_All) on Payon Cave 2: value %d, want > 0", v)
	}
	if v := pay.huntFitValue(cardBon[312087]); v <= 0 {
		t.Fatalf("Ice Crystal Orb (Ele_All) on Payon Cave 2: value %d, want > 0", v)
	}
}

// the priest's staff with a crit card: no crit value for the spell styles, crit still counts for melee builds
func TestCritCardNoValueForCasters(t *testing.T) {
	loadFitFixtures(t)
	crit := &cardBonus{crit: 10}
	priest := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "fitpriest"}}}, Class: 8, Goal: Goal{Map: "pay_dun01"}}
	if v := priest.bonusValue(crit); v != 0 {
		t.Fatalf("priest crit card worth %d, want 0", v)
	}
	smith := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "fitsmith"}}}, Class: 0, Goal: Goal{Map: "pay_dun01"}}
	if v := smith.bonusValue(crit); v <= 0 {
		t.Fatalf("melee crit card worth %d, want > 0", v)
	}
}

// fit_buy_off true: the plain pre-fit behaviour - plain Arrows, no element fit, no card fit
func TestFitBuyOffIsPlainBehaviour(t *testing.T) {
	loadFitFixtures(t)
	pk := shopPk(1, 1750, 5, 1751)
	withFitOff(func() {
		if id, _ := arrowChoice(pk, "pay_dun01", 5000); id != 1750 {
			t.Errorf("flag off: picked %d, want the plain Arrow", id)
		}
		if got := ammoFit(1751, "pay_dun01"); got != 1 {
			t.Errorf("flag off: fit %.2f, want 1", got)
		}
		pay := &Bot{Goal: Goal{Map: "pay_dun01"}}
		if v := pay.huntFitValue(cardBon[4966]); v != 0 {
			t.Errorf("flag off: card fit %d, want 0", v)
		}
	})
}

// the attribute table is the server's level-1 table (attr_fix.yml): the rows the bots depend on
func TestElemMulIsServerTable(t *testing.T) {
	cases := []struct {
		spell, mob string
		want       float64
	}{
		{"Fire", "Undead", 1.25}, {"Water", "Fire", 1.5}, {"Fire", "Water", 0.9}, {"Holy", "Undead", 1.25},
		{"Holy", "Dark", 1.25}, {"Holy", "Poison", 0.75}, {"Ghost", "Ghost", 1.25}, {"Holy", "Holy", 0},
		{"Neutral", "Ghost", 0.9}, {"", "Undead", 1}, {"Fire", "Neutral", 1},
	}
	for _, c := range cases {
		if got := elemMul(c.spell, c.mob); got != c.want {
			t.Errorf("elemMul(%s, %s) = %v, want %v", c.spell, c.mob, got, c.want)
		}
	}
}
