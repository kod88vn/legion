package main

import "testing"

// QA 2026-11-15 (b1794773273mfy): Payon Cave undead took plain Arrows. A Fire Arrow listed in the same shop is bought
// for a map of Undead, and a plain Arrow stays for a neutral map.
func TestArrowChoiceFitsHuntingMap(t *testing.T) {
	items[1750] = &Item{ID: 1750, Name: "Arrow", Type: "Ammo", Sub: "Arrow", Buy: 1, Weight: 1}
	items[1751] = &Item{ID: 1751, Name: "Fire Arrow", Type: "Ammo", Sub: "Arrow", Buy: 5, Weight: 1}
	mobs[9301] = &Mob{ID: 9301, Name: "Zombie", Element: "Undead", Race: "Undead", Level: 30}
	mobs[9302] = &Mob{ID: 9302, Name: "Poring", Element: "Water", Race: "Plant", Level: 1}
	spawnsByMap["zz_undead_fit"] = []Spawn{{Map: "zz_undead_fit", MobID: 9301, Count: 10}}
	spawnsByMap["zz_neutral_fit"] = []Spawn{{Map: "zz_neutral_fit", MobID: 9302, Count: 10}}
	pk := shopPk(1, 1750, 5, 1751)
	if id, pr := arrowChoice(pk, "zz_undead_fit", 5000); id != 1751 || pr != 5 {
		t.Fatalf("undead map: picked %d at %d, want the Fire Arrow at 5", id, pr)
	}
	if id, _ := arrowChoice(pk, "zz_neutral_fit", 5000); id != 1750 {
		t.Fatalf("water map: picked %d, want the plain Arrow (Fire vs Water is no gain)", id)
	}
	// too poor for a useful Fire Arrow stock: plain
	if id, _ := arrowChoice(pk, "zz_undead_fit", 100); id != 1750 {
		t.Fatalf("100z: picked %d, want the plain Arrow", id)
	}
	if got := ammoFit(1751, "zz_undead_fit"); got <= 1 {
		t.Fatalf("Fire Arrow fit on undead %.2f, want > 1", got)
	}
}

// the Payon priest's staff: a crit card gives nothing to a spell-casting style; a race card that hits the hunting map
// gives value
func TestCardValueHuntFit(t *testing.T) {
	items[30001] = &Item{ID: 30001, Name: "Undead Card", Type: "Card"}
	mobs[9303] = &Mob{ID: 9303, Name: "Skeleton", Element: "Undead", Race: "Undead", Level: 20}
	spawnsByMap["zz_fit_undead"] = []Spawn{{Map: "zz_fit_undead", MobID: 9303, Count: 5}}
	spawnsByMap["zz_fit_none"] = []Spawn{{Map: "zz_fit_none", MobID: 9302, Count: 5}}
	priest := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "fitpriest"}}}, Class: 8, Goal: Goal{Map: "zz_fit_undead"}}
	crit := &cardBonus{crit: 10}
	if v := priest.bonusValue(crit); v != 0 {
		t.Fatalf("priest crit card worth %d, want 0", v)
	}
	smith := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "fitsmith"}}}, Class: 0, Goal: Goal{Map: "zz_fit_undead"}}
	if v := smith.bonusValue(crit); v <= 0 {
		t.Fatalf("swordman crit card worth %d, want > 0", v)
	}
	race := &cardBonus{race: map[string]int{"Undead": 20}}
	if v := priest.huntFitValue(race); v <= 0 {
		t.Fatalf("undead race card on an undead map: %d", v)
	}
	priest.Goal.Map = "zz_fit_none"
	if v := priest.huntFitValue(race); v != 0 {
		t.Fatalf("undead race card on a water map: %d, want 0", v)
	}
}
