package main

import (
	"testing"
	"time"
)

// run 6 variants: strip Rogue, Occult tank Monk, trapper Hunter - plans exist, share their base, and their fight rules
// fire only where a sensible player would use them.
func TestRun6Variants(t *testing.T) {
	for id, base := range map[string]string{"rogue_strip": "rogue", "monk_occult": "monk", "hunter_trap": "hunter"} {
		if p := builds[id]; p == nil || p.Base != base {
			t.Fatalf("variant %s missing", id)
		}
	}
	now := time.Now()
	mobs[99911] = &Mob{ID: 99911, Level: 80, HP: 20000, Def: 40}
	mobs[99912] = &Mob{ID: 99912, Level: 20, HP: 300, Def: 2, Exp: 50}
	defer delete(mobs, 99911)
	defer delete(mobs, 99912)
	tough := &Entity{ID: 1, Type: objMob, Class: 99911, X: 11, Y: 10, DX: 11, DY: 10}
	trash := &Entity{ID: 2, Type: objMob, Class: 99912, X: 11, Y: 10, DX: 11, DY: 10}
	mk := func(class int, plan string, sk map[int]*SkillInfo) *Bot {
		b := &Bot{Class: class, skills: sk, buffAt: map[string]time.Time{}, skillFailUntil: map[int]time.Time{}, SP: 300, MaxSP: 320,
			X: 10, Y: 10, ents: map[uint32]*Entity{}, inv: map[int]*InvItem{}}
		b.BaseLv = 60
		b.P = &Persona{Chars: []CharSpec{{Plan: plan, Role: "main"}}}
		return b
	}
	// strip Rogue: Divest Weapon on the tough monster, never on trash, at most 2 tries per skill per monster
	r := mk(17, "rogue_strip", map[int]*SkillInfo{rgStripWeapon: {Lv: 5}, rgStripShield: {Lv: 5}})
	if r.stripTarget(trash, now) {
		t.Fatal("strip on trash")
	}
	n := 0
	for i := 0; i < 6; i++ {
		r.skillReady = time.Time{}
		if r.stripTarget(tough, now) {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("strip casts %d, want 2 weapon + 2 shield", n)
	}
	// Occult Monk: Occult Impaction on armour with a sphere, not on soft trash; Mental Strength when mobbed
	m := mk(15, "monk_occult", map[int]*SkillInfo{moInvestigate: {Lv: 5}, moSteelBody: {Lv: 5}})
	m.spiritCasts = 1
	if m.occultFight(trash, now) {
		t.Fatal("Occult on soft trash")
	}
	if !m.occultFight(tough, now) {
		t.Fatal("Occult on an armoured monster")
	}
	if mk(15, "monk", map[int]*SkillInfo{moInvestigate: {Lv: 3}}).occultMonk() {
		t.Fatal("combo Monk is not an Occult Monk")
	}
	// trapper: Claymore on the cell next to us a tough monster walks to; never without 2 Traps, never for trash
	h := mk(11, "hunter_trap", map[int]*SkillInfo{htClaymore: {Lv: 5}})
	walker := &Entity{ID: 3, Type: objMob, Class: 99911, X: 16, Y: 10, DX: 11, DY: 10, MoveAt: now}
	if h.trapCast(walker, now) {
		t.Fatal("trap without Traps")
	}
	h.inv[5] = &InvItem{Index: 5, ItemID: itemTrap, Count: 10}
	weak := &Entity{ID: 4, Type: objMob, Class: 99912, X: 16, Y: 10, DX: 11, DY: 10}
	if h.trapCast(weak, now) {
		t.Fatal("trap for one weak monster")
	}
	if !h.trapCast(walker, now) {
		t.Fatal("Claymore for a tough monster walking at us")
	}
	h.MaxWeight = 20000
	if !h.trapper() || h.trapsWanted(50, 10000) <= 0 {
		t.Fatal("trapper buys Traps")
	}
}

// run 6: an Assassin with Sonic Blow and no katar saves for the Jur before any other gear and buys it once it can
func TestCoreWeaponKatar(t *testing.T) {
	saved := shopsByMap
	defer func() { shopsByMap = saved }()
	shopsByMap = map[string][]*Shop{"prontera": {{Map: "prontera", X: 142, Y: 57, Name: "Weapons Merchant#t", ID: "wd", Items: []int{1250}}}}
	clear := func() { gearMu.Lock(); delete(gearShopsCache, 1250); gearMu.Unlock() }
	clear()
	defer clear()
	old1207, old1250 := items[1207], items[1250]
	defer func() {
		for id, it := range map[int]*Item{1207: old1207, 1250: old1250} {
			if it == nil {
				delete(items, id) // a nil entry would crash every loop over items in later tests
			} else {
				items[id] = it
			}
		}
	}()
	items[1207] = &Item{ID: 1207, Name: "Main Gauche", Type: "Weapon", Sub: "Dagger", Loc: 0x2, Atk: 43, Buy: 2400}
	items[1250] = &Item{ID: 1250, Name: "Jur", Type: "Weapon", Sub: "Katar", Loc: 0x22, Atk: 68, Buy: 19500}
	mg := &InvItem{Index: 2, ItemID: 1207, Count: 1, Loc: 0x2, Equipped: true}
	b := &Bot{P: &Persona{BaseLevel: 65, Chars: []CharSpec{{Name: "kt", Plan: "assassin", Role: "main"}}}, Class: 12, invSeen: true,
		Zeny: 3000, MaxWeight: 20000, MapName: "prontera", skills: map[int]*SkillInfo{136: {Lv: 10}},
		inv: map[int]*InvItem{2: mg}, equipped: map[uint32]*InvItem{0x2: mg}, gearFailed: map[int]bool{}, buffAt: map[string]time.Time{}}
	b.BaseLv = 65
	if !b.coreSaving() || !b.gearHold() {
		t.Fatal("a Sonic Blow Assassin without a katar saves for one (gearHold)")
	}
	if it, _ := b.classWeaponNeed(); it != nil {
		t.Fatal("too poor for the Jur: keep saving")
	}
	b.Zeny = 21000
	if it, s := b.classWeaponNeed(); it == nil || it.ID != 1250 || s == nil {
		t.Fatal("buys the Jur once it can")
	}
	b.inv[3] = &InvItem{Index: 3, ItemID: 1250, Count: 1, Loc: 0x22}
	if b.coreSaving() {
		t.Fatal("owns a katar: no more saving")
	}
	d := &Bot{P: &Persona{Chars: []CharSpec{{Plan: "assassin_dagger", Role: "main"}}}, Class: 12, invSeen: true, skills: map[int]*SkillInfo{136: {Lv: 1}}}
	if d.coreWeaponSubs() != nil {
		t.Fatal("the dagger Assassin needs no katar")
	}
}

// run 6 (QA 13:50): a Creamy Card in the bag with no slotted accessory - buy a Belt [1] first, then compound into it
func TestCreamyNeedsBelt(t *testing.T) {
	saved := shopsByMap
	defer func() { shopsByMap = saved }()
	shopsByMap = map[string][]*Shop{"prontera": {{Map: "prontera", X: 146, Y: 57, Name: "Armor Merchant#t", ID: "am", Items: []int{itemBelt1}}}}
	clear := func() { gearMu.Lock(); delete(gearShopsCache, itemBelt1); gearMu.Unlock() }
	clear()
	defer clear()
	olds := map[int]*Item{itemBelt1: items[itemBelt1], itemCreamyCard: items[itemCreamyCard]}
	defer func() {
		for id, it := range olds {
			if it == nil {
				delete(items, id)
			} else {
				items[id] = it
			}
		}
	}()
	items[itemBelt1] = &Item{ID: itemBelt1, Name: "Belt", Type: "Armor", Loc: 0x88, Buy: 20000, Slots: 1}
	items[itemCreamyCard] = &Item{ID: itemCreamyCard, Name: "Creamy Card", Type: "Card", Loc: 0x88}
	b := &Bot{P: &Persona{BaseLevel: 40, Chars: []CharSpec{{Name: "cr", Plan: "knight", Role: "main"}}}, Class: 7, invSeen: true,
		Zeny: 5000, MaxWeight: 20000, MapName: "prontera", inv: map[int]*InvItem{4: {Index: 4, ItemID: itemCreamyCard, Count: 1}},
		equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}, buffAt: map[string]time.Time{}}
	b.BaseLv = 40
	if !b.creamyNeedsSlot() || !b.teleSlotSaving() || !b.gearHold() {
		t.Fatal("a Creamy without a slotted accessory: save for the Belt [1]")
	}
	if b.creamyReady(12000) {
		t.Fatal("no Creamy purchase without a slot or the money for both")
	}
	b.Zeny = 30000
	if it, _ := b.nextGearBudget(25000); it == nil || it.ID != itemBelt1 {
		t.Fatalf("buys the Belt [1] first, got %v", it)
	}
	b.inv[6] = &InvItem{Index: 6, ItemID: itemBelt1, Count: 1, Loc: 0x88}
	if b.creamyNeedsSlot() || b.spareSlotFor(itemCreamyCard) == nil {
		t.Fatal("the Belt [1] in the bag takes the Creamy")
	}
}

// natural reset: the Hypnotist menu choices, and storage in / out
func TestResetHypnotist(t *testing.T) {
	if stripColor("Reset skills^000000") != "Reset skills" || stripColor("^FF0000Stat/Skill Reset?") != "Stat/Skill Reset?" {
		t.Fatal("colour codes")
	}
	b := &Bot{inv: map[int]*InvItem{2: {Index: 2, ItemID: 501, Count: 30}, 3: {Index: 3, ItemID: 1201, Count: 1}},
		buffAt: map[string]time.Time{}, P: &Persona{Chars: []CharSpec{{Name: "rs", Plan: "knight", Role: "main"}}}}
	saved := *dataDir
	*dataDir = t.TempDir()
	defer func() { *dataDir = saved }()
	b.reset = &resetTask{Char: "rs", From: "knight", To: "knight_pierce", Stage: "store"}
	b.kafra = &kafraTrip{Store: true}
	if !b.resetOnStorage() || len(b.reset.Stored) != 2 {
		t.Fatalf("stores everything: %+v", b.reset.Stored)
	}
	// rAthena repeats 0x00f2 after every item moved: no second deposit round (run 7: 967 "stored" entries live)
	if !b.resetOnStorage() || len(b.reset.Stored) != 2 {
		t.Fatalf("stores everything: %+v", b.reset.Stored)
	}
	b.reset.Stage = "fetch"
	b.resetStorageItem(7, &InvItem{Index: 7, ItemID: 501, Count: 30})
	b.resetStorageItem(8, &InvItem{Index: 8, ItemID: 4040, Count: 1}) // stored earlier by the bot: stays
	b.resetStorageItem(9, &InvItem{Index: 9, ItemID: 1201, Count: 1})
	b.kafra = &kafraTrip{Store: true}
	if !b.resetOnStorage() || b.reset.Stage != "done" {
		t.Fatalf("takes back what it stored: stage %s", b.reset.Stage)
	}
}
