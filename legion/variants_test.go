package main

import (
	"math/rand"
	"testing"
	"time"
)

// owner 2026-11-14: "make sure we have all varieties of builds": new characters get the main plan most of the time and
// its variants the rest; a variant keeps its main plan's job branch and gear style.
func TestBuildVariantSpread(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	n := map[string]int{}
	for i := 0; i < 2000; i++ {
		n[planForClass(7, "M", r.Intn).ID]++
	}
	if n["knight"] < 650 || n["knight"] > 950 || n["knight_spear"] < 450 || n["knight_pierce"] < 450 { // run 5: main 40%
		t.Fatalf("knight spread %v", n)
	}
	for id, p := range builds {
		if p.Base == "" {
			continue
		}
		m := builds[p.Base]
		if m == nil || m.Class != p.Class || m.First != p.First || m.Branch != p.Branch {
			t.Fatalf("variant %s must share class/first/branch with %s", id, p.Base)
		}
	}
	if !planWeaponSub("knight_spear", "2hSpear") || planWeaponSub("knight_spear", "2hSword") || !planWeaponSub("crusader_gc", "1hSword") {
		t.Fatal("variant weapon types")
	}
}

// build variants run 4: every variant has its own gear table, its weapon rule, and a bow Rogue scores bows
func TestBuildVariantsRun4(t *testing.T) {
	for _, id := range []string{"sage_support", "monk_asura", "alchemist_battle", "alchemist_potion", "blacksmith_mace",
		"assassin_sonic", "assassin_dagger", "rogue_bow"} {
		if builds[id] == nil || builds[id].Base == "" {
			t.Fatalf("variant %s missing", id)
		}
	}
	if !planWeaponSub("assassin_sonic", "Katar") || planWeaponSub("assassin_sonic", "Dagger") ||
		!planWeaponSub("assassin_dagger", "Dagger") || planWeaponSub("assassin_dagger", "Katar") ||
		!planWeaponSub("rogue_bow", "Bow") || !planWeaponSub("blacksmith_mace", "Mace") || planWeaponSub("blacksmith_mace", "1hAxe") ||
		!planWeaponSub("assassin", "Katar") || !planWeaponSub("sage_support", "Staff") {
		t.Fatal("run 4 variant weapon types")
	}
	if buildStyle(builds["rogue_bow"]) != "bow" || buildStyle(builds["rogue"]) != "melee" || buildStyle(builds["sage_support"]) != "magic" {
		t.Fatal("build styles")
	}
	b := &Bot{Class: 17}
	b.P = &Persona{Chars: []CharSpec{{Plan: "rogue_bow", Role: "main"}}}
	if b.gearStyle() != "bow" {
		t.Fatalf("bow rogue gear style %q", b.gearStyle())
	}
	r := rand.New(rand.NewSource(2))
	n := map[string]int{}
	for i := 0; i < 3000; i++ {
		n[planForClass(18, "F", r.Intn).ID]++
	}
	if n["alchemist"] < 1000 || n["alchemist"] > 1400 || n["alchemist_potion"] < 700 || n["alchemist_battle"] < 700 { // run 5: 40/30/30
		t.Fatalf("alchemist spread %v", n)
	}
}

// the Asura Monk: Fury first, then Asura Strike at high SP on a tough monster; trash and the combo Monk get neither
func TestAsuraBurst(t *testing.T) {
	now := time.Now()
	mobs[99901] = &Mob{ID: 99901, Level: 60, HP: 9000}
	mobs[99902] = &Mob{ID: 99902, Level: 60, HP: 300}
	defer delete(mobs, 99901)
	defer delete(mobs, 99902)
	mk := func(plan string) *Bot {
		b := &Bot{Class: 15, skills: map[int]*SkillInfo{moFury: {Lv: 3}, moAsura: {Lv: 5}}, buffAt: map[string]time.Time{},
			skillFailUntil: map[int]time.Time{}, SP: 300, MaxSP: 320, spiritCasts: 5, X: 10, Y: 10}
		b.BaseLv = 60
		b.P = &Persona{Chars: []CharSpec{{Plan: plan, Role: "main"}}}
		return b
	}
	tough := &Entity{ID: 1, Class: 99901, X: 11, Y: 10, DX: 11, DY: 10}
	trash := &Entity{ID: 2, Class: 99902, X: 11, Y: 10, DX: 11, DY: 10}
	b := mk("monk_asura")
	if b.asuraBurst(trash, now) {
		t.Fatal("Asura on trash")
	}
	if !b.asuraBurst(tough, now) || b.buffAt["asura:fury"].Before(now) {
		t.Fatal("Fury first")
	}
	b.skillReady = time.Time{}
	if !b.asuraBurst(tough, now.Add(2*time.Second)) || !b.skillReady.After(now.Add(4*time.Second)) {
		t.Fatal("Asura Strike after Fury")
	}
	if mk("monk").asuraBurst(tough, now) {
		t.Fatal("combo Monk must not burst")
	}
	low := mk("monk_asura")
	low.SP = 100
	if low.asuraBurst(tough, now) {
		t.Fatal("Asura below 85% SP")
	}
}

// step 2: the next piece is the best real upgrade on the build's table (a bow for a bow Rogue on a Knife), drop-only
// pieces are marked for farming or trade, and a complete band gives nothing
func TestNextBuildPiece(t *testing.T) {
	saved := map[int]*Item{}
	for _, id := range []int{98801, 98802, 98803} {
		saved[id] = items[id]
	}
	defer func() {
		for id, it := range saved {
			if it == nil {
				delete(items, id)
			} else {
				items[id] = it
			}
		}
	}()
	items[98801] = &Item{ID: 98801, Name: "TestKnife", Type: "Weapon", Loc: 0x2, Atk: 17, Sub: "Dagger"}
	items[98802] = &Item{ID: 98802, Name: "TestBow", Type: "Weapon", Loc: 0x22, Atk: 60, Sub: "Bow", Buy: 0}
	items[98803] = &Item{ID: 98803, Name: "TestHat", Type: "Armor", Loc: 0x100, Def: 1}
	buildGear.Lock()
	oldT, oldAt := buildGear.table, buildGear.builtAt
	buildGear.table = map[string][]gearBand{"rogue_bow": {{Min: 1, Max: 99, Slots: map[string][]gearEntry{
		"weapon": {{ID: 98802, Slot: "weapon"}}, "headgear": {{ID: 98803, Slot: "headgear"}}}}}}
	buildGear.builtAt = time.Now()
	buildGear.Unlock()
	defer func() { buildGear.Lock(); buildGear.table, buildGear.builtAt = oldT, oldAt; buildGear.Unlock() }()
	b := &Bot{Class: 17, invSeen: true, equipped: map[uint32]*InvItem{0x2: {ItemID: 98801}}, gearFailed: map[int]bool{}}
	b.BaseLv = 40
	b.P = &Persona{Chars: []CharSpec{{Plan: "rogue_bow", Role: "main"}}}
	np := b.nextBuildPiece()
	if np == nil || np.ItemID != 98802 || np.Source != "trade" {
		t.Fatalf("next piece %+v, want TestBow (trade)", np)
	}
	b.equipped[0x2] = &InvItem{ItemID: 98802}
	b.equipped[0x100] = &InvItem{ItemID: 98803}
	if np := b.nextBuildPiece(); np != nil {
		t.Fatalf("band complete, got %+v", np)
	}
}

// run 5 (owner 2026-11-14: "the builds of the legions still lack variety"): every class line has 2-3 builds, no build
// above ~40% of a line with 3+ builds, and each new variant's special rules hold.
func TestBuildVarietyRun5(t *testing.T) {
	fam := map[string]int{}
	for _, p := range builds {
		fam[p.base()]++
	}
	for base, n := range fam {
		if n < 2 {
			t.Errorf("class line %s has %d build(s)", base, n)
		}
	}
	for _, id := range []string{"knight_pierce", "crusader_spear", "wizard_fire", "wizard_frost", "sage_hindsight", "hunter_agi",
		"priest_battle", "bard_arrow", "dancer_arrow"} {
		p := builds[id]
		if p == nil || p.Base == "" || len(p.Skills) == 0 || len(p.Stats) == 0 {
			t.Fatalf("variant %s missing", id)
		}
	}
	r := rand.New(rand.NewSource(3))
	n := map[string]int{}
	for i := 0; i < 5000; i++ {
		n[planForClass(9, "M", r.Intn).ID]++ // Wizard: main + fire + frost
	}
	for id, c := range n {
		if c*100/5000 > 45 {
			t.Fatalf("wizard spread %v: %s over 40%%", n, id)
		}
	}
	if !planWeaponSub("crusader_spear", "2hSpear") || planWeaponSub("crusader_spear", "1hSword") || !planWeaponSub("priest_battle", "Mace") ||
		!planWeaponSub("sage_hindsight", "Book") || !planWeaponSub("knight_pierce", "1hSpear") {
		t.Fatal("run 5 variant weapon types")
	}
	if buildStyle(builds["priest_battle"]) != "melee" || buildStyle(builds["sage_hindsight"]) != "melee" || buildStyle(builds["wizard_fire"]) != "magic" {
		t.Fatal("run 5 build styles")
	}
	if !weaponFits("Spear", "2hSpear") || !weaponFits("Spear", "1hSpear") || weaponFits("Spear", "2hSword") || !weaponFits("Bow", "Bow") {
		t.Fatal("weaponFits")
	}
	// Hindsight: the spell learned at the highest level wins, the later (stronger) entry on a tie
	pk := []byte{0xfb, 0x0a, 20, 0, 19, 0, 0, 0, 14, 0, 0, 0, 20, 0, 0, 0, 91, 0, 0, 0}
	if got := autoSpellPick(pk, map[int]*SkillInfo{19: {Lv: 10}, 14: {Lv: 10}, 20: {Lv: 4}, 91: {Lv: 5}}); got != 14 {
		t.Fatalf("auto spell pick %d, want 14 (Cold Bolt 10, after Fire Bolt 10)", got)
	}
	// a Hindsight Sage fights in melee: no ranged bolt from bestAttack once Hindsight is learned
	s := &Bot{Class: 16, skills: map[int]*SkillInfo{mgFire: {Lv: 10}, 84: {Lv: 0}, saAutoSpell: {Lv: 5}}, skillFailUntil: map[int]time.Time{}}
	s.P = &Persona{Chars: []CharSpec{{Plan: "sage_hindsight", Role: "main"}}}
	if a := s.bestAttack(); a != nil {
		t.Fatalf("hindsight sage bestAttack %+v, want melee", a)
	}
	if s.gearStyle() != "melee" || s.spCaster() {
		t.Fatal("hindsight sage is a melee fighter")
	}
	// an arrow bard keeps its bow in a party (no support singing)
	bd := &Bot{Class: 19, skills: map[int]*SkillInfo{321: {Lv: 5}}}
	bd.P = &Persona{Chars: []CharSpec{{Plan: "bard_arrow", Role: "main"}}}
	if bd.songSupport() {
		t.Fatal("arrow bard must not sing")
	}
}
