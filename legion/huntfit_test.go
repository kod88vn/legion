package main

import (
	"os"
	"testing"
)

// loadFitFixtures loads the real rAthena rows the fit reads (fit_fixture_test.go: Arrows 1750-1757, the Hydra Card 4035,
// Z Stone 9 4966 and Ice Crystal Orb 312087) and the real mobs and spawns of Payon Cave (mob_db.yml element and race;
// npc spawn lines of zz_payon_undead). The ids are the server's.
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
	// zz_payon_undead (Payon Cave 2): the real spawn lines of the npc file, counts summed as the loader does (Soldier Skeleton
	// 50 + 53, Archer Skeleton 30 + 32, Furious Soldier Skeleton 2, Drainliar 5, Eggyra 15 + 16, Red Plant 10, Golden
	// Poring 3): undead is about 77% of the spawns.
	spawnsByMap["zz_payon_undead"] = []Spawn{
		{Map: "zz_payon_undead", MobID: 1028, Count: 50}, {Map: "zz_payon_undead", MobID: 1028, Count: 53},
		{Map: "zz_payon_undead", MobID: 1016, Count: 30}, {Map: "zz_payon_undead", MobID: 1016, Count: 32},
		{Map: "zz_payon_undead", MobID: 2649, Count: 2}, {Map: "zz_payon_undead", MobID: 1111, Count: 5},
		{Map: "zz_payon_undead", MobID: 1116, Count: 15}, {Map: "zz_payon_undead", MobID: 1116, Count: 16},
		{Map: "zz_payon_undead", MobID: 1078, Count: 10}, {Map: "zz_payon_undead", MobID: 2248, Count: 3},
	}
	// test-only spawn lists of real mobs: a neutral map and an all-Demihuman map
	spawnsByMap["zz_neutral_fit"] = []Spawn{{Map: "zz_neutral_fit", MobID: 1177, Count: 10}}
	spawnsByMap["zz_orc_fit"] = []Spawn{{Map: "zz_orc_fit", MobID: 1023, Count: 20}}
	mapFitForget("zz_payon_undead", "zz_neutral_fit", "zz_orc_fit")
}

// mapFitForget: the hunt-fit cache of these maps is rebuilt from their spawn lists
func mapFitForget(maps ...string) {
	huntMu.Lock()
	defer huntMu.Unlock()
	for _, m := range maps {
		delete(huntCache, m)
	}
}

// withArrowGain runs f with tuning arrow_gain_per_z n, then restores the tuning in force
func withArrowGain(n int, f func()) {
	old := tune()
	nt := *old
	nt.ArrowGainPerZ = n
	curTuning.Store(&nt)
	defer curTuning.Store(old)
	f()
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
	pay := &Bot{Goal: Goal{Map: "zz_payon_undead"}}
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
	priest := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "fitpriest"}}}, Class: 8, Goal: Goal{Map: "zz_payon_undead"}}
	if v := priest.bonusValue(crit); v != 0 {
		t.Fatalf("priest crit card worth %d, want 0", v)
	}
	smith := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "fitsmith"}}}, Class: 0, Goal: Goal{Map: "zz_payon_undead"}}
	if v := smith.bonusValue(crit); v <= 0 {
		t.Fatalf("melee crit card worth %d, want > 0", v)
	}
}

// fit_buy_off true: the plain pre-fit behaviour - no element fit, no card fit, no market arrow want
func TestFitBuyOffIsPlainBehaviour(t *testing.T) {
	loadFitFixtures(t)
	withFitOff(func() {
		if got := ammoFit(1751, "zz_payon_undead"); got != 1 {
			t.Errorf("flag off: fit %.2f, want 1", got)
		}
		bow := &Bot{Goal: Goal{Map: "zz_payon_undead"}, MapName: "zz_payon_undead", P: &Persona{BaseLevel: 80, Chars: []CharSpec{{Name: "offwant"}}}}
		if w := bow.fitArrowWant(); w != nil {
			t.Errorf("flag off: market want %+v, want none", w)
		}
		pay := &Bot{Goal: Goal{Map: "zz_payon_undead"}}
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

// NPC shops sell plain Arrows only on this server (barter setup; rule G09 forbids adding elemental arrows): even a shop
// that lists Silver Arrows at 5z gets a plain Arrow from the buy step.
func TestArrowChoiceIsPlainOnly(t *testing.T) {
	loadFitFixtures(t)
	pk := shopPk(1, 1750, 5, 1751, 5, 1752)
	if id, pr := arrowChoice(pk); id != 1750 || pr != 1 {
		t.Fatalf("arrowChoice on a list with Silver and Fire: picked %d at %d, want the plain Arrow at 1", id, pr)
	}
}

// the equip path: a bot carrying Silver Arrows and plain Arrows, hunting zz_payon_undead (Undead), equips the Silver stack;
// on a neutral map both fit the same and the stack carried is kept (bestArrowStack picks the best fit only).
func TestCarriedSilverArrowIsEquipped(t *testing.T) {
	loadFitFixtures(t)
	b := &Bot{Goal: Goal{Map: "zz_payon_undead"}, MapName: "zz_payon_undead", inv: map[int]*InvItem{
		1: {Index: 1, ItemID: 1750, Count: 500},
		2: {Index: 2, ItemID: 1751, Count: 100},
	}}
	if a := bestArrowStack(b, "zz_payon_undead"); a == nil || a.ItemID != 1751 {
		t.Fatalf("zz_payon_undead: best stack %+v, want Silver Arrow 1751", a)
	}
	if a := bestArrowStack(&Bot{inv: map[int]*InvItem{}}, "zz_payon_undead"); a != nil {
		t.Fatalf("no arrows carried: best stack %+v, want nil", a)
	}
}

// the market path: a bow user in the field of a fully undead map wants Silver Arrows (the lowest id among the arrows
// that fit 125% on Undead: 1751 Holy, 1752 Fire tie; the lower id wins), not in town, and not when no arrow fits better
func TestMarketWantsFittingArrow(t *testing.T) {
	loadFitFixtures(t)
	spawnsByMap["zz_pure_undead"] = []Spawn{{Map: "zz_pure_undead", MobID: 1028, Count: 40}}
	defer mapFitForget("zz_pure_undead")
	bow := &Bot{Goal: Goal{Map: "zz_pure_undead"}, MapName: "zz_pure_undead", Zeny: 5000,
		P:        &Persona{BaseLevel: 80, Chars: []CharSpec{{Name: "wantarrow"}}},
		inv:      map[int]*InvItem{},
		equipped: map[uint32]*InvItem{0x2: {Index: 9, ItemID: 1701, Count: 1, Equipped: true}}}
	items[1701] = &Item{ID: 1701, Name: "Bow", Type: "Weapon", Sub: "Bow", Loc: 0x2, Atk: 15}
	// the market bids 11z for a Silver Arrow here (no NPC sells it: the rarity price), 10z over plain: +25% does not pay
	// 4% per zeny, so no want
	if w := bow.fitArrowWant(); w != nil {
		t.Fatalf("pure undead field at an 11z bid: want %+v, want none (the gain does not pay the price)", w)
	}
	// with the gate at 1% per zeny the same Silver Arrow pays and is wanted (lowest id on the tie)
	withArrowGain(1, func() {
		if w := bow.fitArrowWant(); w == nil || w.ItemID != 1751 || w.Why != "ammo" || w.Count != 50 {
			t.Errorf("gate 1%%: want %+v, want Silver Arrow 1751 x50 (Why ammo)", w)
		}
	})
	bow.MapName = "prontera" // a town: the shop runs and the field want don't mix
	if w := bow.fitArrowWant(); w != nil {
		t.Fatalf("in town: want %+v, want none", w)
	}
}
