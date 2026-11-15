package main

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// EC39 test kit: a Swordman build with three weapon and three armor steps (MinLv 1, 21, 41). Bands 61+ reuse the last
// step, so the gear level is interpolated between the band starts 1, 21, 41 and up to 99.
var msItems = map[int]*Item{
	9101: {ID: 9101, Name: "Sword I", Type: "Weapon", Loc: 0x2, Atk: 43, WLv: 1, MinLv: 1},
	9102: {ID: 9102, Name: "Sword II", Type: "Weapon", Loc: 0x2, Atk: 80, WLv: 1, MinLv: 21},
	9103: {ID: 9103, Name: "Sword III", Type: "Weapon", Loc: 0x2, Atk: 120, WLv: 1, MinLv: 41},
	9201: {ID: 9201, Name: "Armor I", Type: "Armor", Loc: 0x10, Def: 20, MinLv: 1},
	9202: {ID: 9202, Name: "Armor II", Type: "Armor", Loc: 0x10, Def: 37, MinLv: 21},
	9203: {ID: 9203, Name: "Armor III", Type: "Armor", Loc: 0x10, Def: 55, MinLv: 41},
	9301: {ID: 9301, Name: "Chain Mail", Type: "Armor", Loc: 0x10, Def: 55, MinLv: 70},
}

func msBuild() *Build {
	return &Build{ID: "test_ec39_sword", Class: 7, First: 1, Weapons: []int{9101, 9102, 9103}, Armors: []int{9201, 9202, 9203}}
}

func withMsItems(t *testing.T) {
	saved := map[int]*Item{}
	for id, x := range msItems {
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

// msBot: a Swordman at base level lv wearing the given weapon, body armor and card, with a character spec on the test build.
func msBot(name string, lv, weapon, armor int, cards ...int) *Bot {
	return &Bot{P: &Persona{Name: name, Risk: 0.5, BaseLevel: lv, Chars: []CharSpec{{Name: name, Plan: "test_ec39_sword"}}},
		BaseLv: lv, Class: 1, invSeen: true,
		equipped: gsEquip(gsPiece{weapon, 0x2, 0, cards}, gsPiece{armor, 0x10, 0, nil})}
}

func TestMilestonesReadTheRoutes(t *testing.T) {
	withMsItems(t)
	ms := milestonesFor(msBuild(), nil)
	if len(ms) != len(bandEdges) {
		t.Fatalf("%d milestone bands, want %d", len(ms), len(bandEdges))
	}
	want := map[int][2]int{1: {9101, 9201}, 21: {9102, 9202}, 41: {9103, 9203}}
	for _, m := range ms {
		if w, ok := want[m.Min]; ok && (m.WeaponItem != w[0] || m.ArmorItem != w[1]) {
			t.Errorf("band %d-%d: weapon %d armor %d, want %d and %d", m.Min, m.Max, m.WeaponItem, m.ArmorItem, w[0], w[1])
		}
	}
	if ms[2].Cards != 1 || ms[0].Cards != 0 {
		t.Errorf("cards per band: 1-20 %d, 41-60 %d; want 0 and 1", ms[0].Cards, ms[2].Cards)
	}
}

// regression (band 1-20 row error, knight Chain Mail DEF 55 at MinLv 70): nothing reachable means no milestone
func TestBandOneNeverListsEndGameArmor(t *testing.T) {
	withMsItems(t)
	p := &Build{ID: "test_ec39_knight", Class: 7, First: 1, Weapons: []int{9101}, Armors: []int{9301}}
	ms := milestonesFor(p, nil)
	if ms[0].ArmorItem != 0 || ms[0].Armor != 0 {
		t.Fatalf("band 1-20 armor = %d (%d DEF), want none: Chain Mail is MinLv 70", ms[0].ArmorItem, ms[0].Armor)
	}
}

// the gate is a continuous function of the weapon: a stronger weapon never lowers the gate (no cliff)
func TestGearGateIsMonotone(t *testing.T) {
	withMsItems(t)
	p := msBuild()
	prev := 0
	for atk := 43; atk <= 130; atk++ {
		b := msBot("ec39mono", 60, 9101, 9201)
		b.equipped[0x2].ItemID = 9101
		items[9101] = &Item{ID: 9101, Type: "Weapon", Loc: 0x2, Atk: atk, WLv: 1, MinLv: 1}
		g, _ := gearGateOf(b, p, 60)
		if g == 0 {
			g = 60
		}
		if g < prev {
			t.Fatalf("atk %d: gate %d below the gate %d of a weaker weapon", atk, g, prev)
		}
		prev = g
	}
}

// a lv60 bot with lv20-level gear (Sword I, Armor I) is gated 15 below base (the most) and its goal is Sword II
func TestLowGearLv60GetsSafeMapAndGearGoal(t *testing.T) {
	withMsItems(t)
	b := msBot("ec39low", 60, 9101, 9201)
	p := msBuild()
	gate, goal := gearGateOf(b, p, 60)
	if gate != 45 {
		t.Fatalf("gate = %d, want 45 (base-15, the most a bot is dropped)", gate)
	}
	if goal == nil || goal.Item != 9102 || goal.Slot != "weapon" {
		t.Fatalf("goal = %+v, want the weapon Sword II (9102)", goal)
	}
	raw := gearEffectiveLv(famSword, 60, rateGear(famSword, 60, b.equipped))
	if got := b.gearGatedLv(p, 60, 60); got != 45 {
		t.Fatalf("gated effective level %d (raw %d), want 45", got, raw)
	}
}

// the gate is the gear level plus one band: Sword II + Armor II (gear level ~28) at lv60 is gated at 48
func TestGateIsGearLevelPlusBand(t *testing.T) {
	withMsItems(t)
	b := msBot("ec39mid", 60, 9102, 9202)
	gate, goal := gearGateOf(b, msBuild(), 60)
	if gate != 48 {
		t.Fatalf("gate = %d, want 48 (gear level ~28 + one band)", gate)
	}
	if goal == nil || goal.Item != 9103 {
		t.Fatalf("goal = %+v, want Sword III (9103)", goal)
	}
}

// a bot less than one band behind its gear is not gated: lv45 with Sword II (gate 48) keeps its level
func TestOneBandBehindIsNotGated(t *testing.T) {
	withMsItems(t)
	b := msBot("ec39band", 45, 9102, 9202)
	if gate, goal := gearGateOf(b, msBuild(), 45); gate != 0 || goal != nil {
		t.Fatalf("lv45 with Sword II: gate %d goal %+v, want no gate", gate, goal)
	}
}

// the milestone gear for the band (Sword III + Armor III, carded) lifts the gate: no gate at lv60
func TestMilestoneGearLiftsTheGate(t *testing.T) {
	withMsItems(t)
	b := msBot("ec39full", 60, 9103, 9203, 4006)
	if gate, goal := gearGateOf(b, msBuild(), 60); gate != 0 || goal != nil {
		t.Fatalf("full milestone gear: gate %d goal %+v, want none", gate, goal)
	}
}

// the memo follows the worn gear: an equip change refreshes the gate
func TestGateMemoRefreshesOnEquipChange(t *testing.T) {
	withMsItems(t)
	p := msBuild()
	b := msBot("ec39memo", 60, 9101, 9201)
	if g, _ := b.gearGateFor(p, 60); g != 45 {
		t.Fatalf("before: gate %d, want 45", g)
	}
	b.equipped = gsEquip(gsPiece{9102, 0x2, 0, nil}, gsPiece{9202, 0x10, 0, nil})
	if g, _ := b.gearGateFor(p, 60); g != 48 {
		t.Fatalf("after equipping Sword II: gate %d, want 48 (memo must refresh)", g)
	}
}

// powerLv: the effective level through the real path (invReady, the build from the character spec) never exceeds the gate
func TestPowerLvIsCappedByGate(t *testing.T) {
	withMsItems(t)
	saved, had := builds[msBuild().ID]
	builds[msBuild().ID] = msBuild()
	t.Cleanup(func() {
		if had {
			builds[msBuild().ID] = saved
		} else {
			delete(builds, msBuild().ID)
		}
	})
	b := msBot("ec39power", 60, 9101, 9201)
	if got := b.powerLv(); got > 45 {
		t.Fatalf("powerLv %d for lv60 Sword I + Armor I, want <= 45", got)
	}
}

// a party's map follows its weakest-geared member: a gated member's gate caps the party's minimum level
func TestPartyFollowsWeakestGear(t *testing.T) {
	withMsItems(t)
	strong := &Bot{P: &Persona{Name: "ec39strong", Risk: 0.5}, BaseLv: 75}
	weak := &Bot{P: &Persona{Name: "ec39weak", Risk: 0.5}, BaseLv: 60}
	for _, c := range []struct {
		b *Bot
		s pubState
	}{
		{strong, pubState{Name: "ec39strong", Online: true, Known: true, Lv: 75, Power: 75}},
		{weak, pubState{Name: "ec39weak", Online: true, Known: true, Lv: 60, Power: 45, Gate: 45}},
	} {
		v := &atomic.Value{}
		v.Store(c.s)
		pubs.Store(c.b.P.Name, v)
		t.Cleanup(func() { pubs.Delete(c.b.P.Name) })
	}
	minLv, _, known, _ := partyFitLv([]*Bot{strong, weak})
	if known != 2 {
		t.Fatalf("known %d, want 2", known)
	}
	if minLv > 45 {
		t.Fatalf("party minimum level %d, want <= the weak member's gate 45", minLv)
	}
}

// the picker: gated bots with the same gear spread over the spots and never go to a spot far above their gate
func TestGatedBotsSpreadAndNeverIdle(t *testing.T) {
	swapFairTables(t)
	fairWorld()
	withMsItems(t)
	// two lv26-28 fields: fair for a base-40 bot (line 25) and fitting its gated level 25
	mobs[3001] = &Mob{ID: 3001, Level: 26}
	mobs[3002] = &Mob{ID: 3002, Level: 28}
	for _, m := range []string{"gef_fild01", "gef_fild02"} {
		maps[m] = &GameMap{}
		spawnsByMap[m] = []Spawn{{m, 3001, 20}, {m, 3002, 20}}
		huntSpots = append(huntSpots, HuntSpot{Map: m, MinLv: 20, MaxLv: 35, Mobs: 40})
	}
	saved, had := builds[msBuild().ID]
	builds[msBuild().ID] = msBuild()
	t.Cleanup(func() {
		if had {
			builds[msBuild().ID] = saved
		} else {
			delete(builds, msBuild().ID)
		}
	})
	starts := []string{"gef_fild01", "gef_fild02"}
	distinct := map[string]bool{}
	for i := 0; i < 9; i++ {
		b := msBot("ec39pick"+string(rune('a'+i)), 40, 9101, 9201)
		b.MapName = starts[i%len(starts)]
		b.rng = rand.New(rand.NewSource(int64(i + 1)))
		b.chooseHuntingGround()
		if b.Goal.Map == "" {
			t.Fatalf("bot %d idles: goal %+v", i, b.Goal)
		}
		if b.Goal.Map == "moc_fild02" {
			t.Fatalf("bot %d gated sent to moc_fild02 (lv55-60 monsters)", i)
		}
		distinct[b.Goal.Map] = true
	}
	if len(distinct) < 2 {
		b := msBot("ec39dbg", 40, 9101, 9201)
		t.Fatalf("9 gated bots all went to %v: they must spread over the spots (power %d, spots %v, set %q)", distinct, b.powerLv(), b.spotsFor(b.powerLv()), b.Goal.Set)
	}
}

// tuning gear_gate_on = false: no gate at all
func TestGearGateTuningOff(t *testing.T) {
	withMsItems(t)
	swapFairTables(t)
	off := *tune()
	off.GearGateOn = false
	curTuning.Store(&off)
	b := msBot("ec39off", 60, 9101, 9201)
	if g, _ := gearGateOf(b, msBuild(), 60); g != 0 {
		t.Fatalf("flag off: gate %d, want 0", g)
	}
}

// the Builds tab's file format: {"builds": {id: {"ranges": [{"min", "max", "slots": {slot: [ids]}}]}}} (written to a temp dir)
func TestMilestoneFileFormat(t *testing.T) {
	withMsItems(t)
	saved, had := builds[msBuild().ID]
	builds[msBuild().ID] = msBuild()
	t.Cleanup(func() {
		if had {
			builds[msBuild().ID] = saved
		} else {
			delete(builds, msBuild().ID)
		}
	})
	path := filepath.Join(t.TempDir(), "buildmilestones_gear.json")
	if err := writeMilestoneFile(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Builds map[string]struct {
			Ranges []struct {
				Min       int              `json:"min"`
				Max       int              `json:"max"`
				Slots     map[string][]int `json:"slots"`
				Milestone map[string]int   `json:"milestone"`
			} `json:"ranges"`
		} `json:"builds"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	b, ok := doc.Builds[msBuild().ID]
	if !ok || len(b.Ranges) != len(bandEdges) {
		t.Fatalf("build %s: %d ranges, want %d", msBuild().ID, len(b.Ranges), len(bandEdges))
	}
	r := b.Ranges[1]
	if r.Min != 21 || r.Max != 40 || r.Milestone["weapon"] != 9102 || r.Milestone["armor"] != 9202 {
		t.Fatalf("band 21-40 = %+v, want milestone weapon 9102 and armor 9202", r)
	}
	for _, name := range []string{"weapon", "shield", "armor", "garment", "shoes", "headgear", "accessory"} {
		if _, ok := r.Slots[name]; !ok {
			t.Fatalf("band 21-40 has no %s slot (the dashboard keeps every slot)", name)
		}
	}
	if !strings.Contains(string(raw), `"weapon_atk"`) || !strings.Contains(string(raw), `"cards"`) {
		t.Fatal("ranges lack the weapon_atk / cards fields")
	}
}

// a gated bot with enough zeny buys its failing armor piece first; once worn, the gate lifts and its ceiling moves up
func TestGatedBotBuysFailingPieceFirst(t *testing.T) {
	withMsItems(t)
	// Sword III (ATK 120) with Armor I (DEF 20): the armor fails, its goal is the milestone armor of band 21-40
	b := msBot("ec39buy", 60, 9103, 9201)
	b.MapName = "prontera"
	b.Zeny = 50000
	p := msBuild()
	builds[p.ID] = p // the bot's plan (gearGate reads it through the character spec)
	t.Cleanup(func() { delete(builds, p.ID) })

	gate0, goal0 := gearGateOf(b, p, 60)
	if goal0 == nil || goal0.Slot != "armor" || goal0.Item != 9202 {
		t.Fatalf("before: gate %d goal %+v, want the armor Armor II (9202)", gate0, goal0)
	}
	// the goal armor is sold by an NPC shop at 2000z (the item table is restored by withMsItems)
	items[9202] = &Item{ID: 9202, Name: "Armor II", Type: "Armor", Loc: 0x10, Def: 37, MinLv: 21, Buy: 2000}
	gearShopsCache[9202] = []*Shop{{Map: "prontera", Items: []int{9202}}}
	t.Cleanup(func() { delete(gearShopsCache, 9202); delete(items, 9202) })

	it, sh := b.gatedGearShop(b.Zeny - b.gearReserve())
	if it == nil || it.ID != 9202 || sh == nil {
		t.Fatalf("gated buy: item %v shop %v, want Armor II from a shop", it, sh)
	}
	// nextGear puts it first, before the plan's own list
	if got, _ := b.nextGear(); got == nil || got.ID != 9202 {
		t.Fatalf("nextGear = %v, want the gated Armor II first", got)
	}
	// bought and worn: the gate lifts (armor 37 -> gear level ~28 + band = 48) and the goal moves to Armor III
	b.equipped = gsEquip(gsPiece{9103, 0x2, 0, nil}, gsPiece{9202, 0x10, 0, nil})
	gate1, goal1 := gearGateOf(b, p, 60)
	if gate1 <= gate0 {
		t.Fatalf("after buying: gate %d, want above %d", gate1, gate0)
	}
	if goal1 == nil || goal1.Item != 9203 {
		t.Fatalf("after buying: goal %+v, want Armor III (9203)", goal1)
	}
	if got := b.gearGatedLv(p, 60, 60); got != gate1 {
		t.Fatalf("maps: gated level %d, want the new ceiling %d", got, gate1)
	}
}

// the necessary-purchase path (gearStepNeed) buys the gated piece, not another slot's step
func TestGatedBotNecessaryStepIsTheGatedPiece(t *testing.T) {
	withMsItems(t)
	b := msBot("ec39need", 60, 9103, 9201)
	b.MapName = "prontera"
	b.Zeny = 50000
	p := msBuild()
	builds[p.ID] = p
	t.Cleanup(func() { delete(builds, p.ID) })
	items[9202] = &Item{ID: 9202, Name: "Armor II", Type: "Armor", Loc: 0x10, Def: 37, MinLv: 21, Buy: 2000}
	gearShopsCache[9202] = []*Shop{{Map: "prontera", Items: []int{9202}}}
	t.Cleanup(func() { delete(gearShopsCache, 9202); delete(items, 9202) })
	if it, _ := b.gearStepNeed(); it == nil || it.ID != 9202 {
		t.Fatalf("gearStepNeed = %v, want the gated Armor II", it)
	}
}

// the economy wants (econWant) bid for nothing else while gated: the failing piece is the only gear want
func TestGatedBotWantsOnlyTheGatedPiece(t *testing.T) {
	withMsItems(t)
	b := msBot("ec39want", 60, 9103, 9201)
	b.MapName = "prontera"
	b.Zeny = 50000
	p := msBuild()
	builds[p.ID] = p
	t.Cleanup(func() { delete(builds, p.ID) })
	items[9202] = &Item{ID: 9202, Name: "Armor II", Type: "Armor", Loc: 0x10, Def: 37, MinLv: 21, Buy: 2000}
	gearShopsCache[9202] = []*Shop{{Map: "prontera", Items: []int{9202}}}
	t.Cleanup(func() { delete(gearShopsCache, 9202); delete(items, 9202) })
	if w := b.econWant(); w != nil && w.ItemID != 9202 {
		t.Fatalf("econWant = item %d, want nothing or the gated Armor II (9202)", w.ItemID)
	}
}

// save-hold: the gated piece costs more than the purse allows: nothing else is bought meanwhile
func TestGatedBotSavesForItsPiece(t *testing.T) {
	withMsItems(t)
	b := msBot("ec39save", 60, 9103, 9201)
	b.MapName = "prontera"
	p := msBuild()
	builds[p.ID] = p
	t.Cleanup(func() { delete(builds, p.ID) })
	items[9202] = &Item{ID: 9202, Name: "Armor II", Type: "Armor", Loc: 0x10, Def: 37, MinLv: 21, Buy: 2000}
	gearShopsCache[9202] = []*Shop{{Map: "prontera", Items: []int{9202}}}
	t.Cleanup(func() { delete(gearShopsCache, 9202); delete(items, 9202) })
	b.Zeny = 1000
	if it, _ := b.nextGear(); it != nil {
		t.Fatalf("saving for Armor II (2000z) with 1000z: nextGear bought %d", it.ID)
	}
	b.Zeny = 50000
	if it, _ := b.nextGear(); it == nil || it.ID != 9202 {
		t.Fatalf("with enough zeny nextGear = %v, want Armor II", it)
	}
}

// the reserve never swallows the gated piece's price: a purse just above the price still buys it
func TestGearReserveLetsTheGatedPriceThrough(t *testing.T) {
	withMsItems(t)
	b := msBot("ec39reserve", 60, 9103, 9201)
	b.MapName = "prontera"
	p := msBuild()
	builds[p.ID] = p
	t.Cleanup(func() { delete(builds, p.ID) })
	items[9202] = &Item{ID: 9202, Name: "Armor II", Type: "Armor", Loc: 0x10, Def: 37, MinLv: 21, Buy: 2000}
	gearShopsCache[9202] = []*Shop{{Map: "prontera", Items: []int{9202}}}
	t.Cleanup(func() { delete(gearShopsCache, 9202); delete(items, 9202) })
	b.Zeny = 2100
	if it, _ := b.gatedGearShop(b.Zeny - b.gearReserve()); it == nil || it.ID != 9202 {
		t.Fatalf("2100z, Armor II 2000z: reserve %d leaves no budget for the gated piece", b.gearReserve())
	}
}

// the legacy power path (gear_score_on off) is gated too: Sword III rates 56 by weapon and armor I drops it to 45
func TestLegacyPowerLvIsGated(t *testing.T) {
	withMsItems(t)
	saved := *tune()
	off := saved
	off.GearScoreOn = false
	curTuning.Store(&off)
	t.Cleanup(func() { curTuning.Store(&saved) })
	b := msBot("ec39legacy", 60, 9103, 9201)
	p := msBuild()
	builds[p.ID] = p
	t.Cleanup(func() { delete(builds, p.ID) })
	if got := b.powerLv(); got > 45 {
		t.Fatalf("legacy powerLv %d for Sword III + Armor I at lv60, want <= 45", got)
	}
}

// the save-hold has a cap: a gated piece beyond max(3 x budget, 5000) does not block the other steps
func TestGatedSaveHoldIsCapped(t *testing.T) {
	withMsItems(t)
	b := msBot("ec39cap", 60, 9103, 9201)
	b.MapName = "prontera"
	p := msBuild()
	builds[p.ID] = p
	t.Cleanup(func() { delete(builds, p.ID) })
	items[9202] = &Item{ID: 9202, Name: "Armor II", Type: "Armor", Loc: 0x10, Def: 37, MinLv: 21, Buy: 60000}
	gearShopsCache[9202] = []*Shop{{Map: "prontera", Items: []int{9202}}}
	t.Cleanup(func() { delete(gearShopsCache, 9202); delete(items, 9202) })
	if b.gatedSaveHold(1000) {
		t.Fatalf("60000z piece, budget 1000z: beyond the cap, no hold expected")
	}
	items[9202].Buy = 2000
	if !b.gatedSaveHold(1000) {
		t.Fatalf("2000z piece, budget 1000z: within the cap, the hold must hold")
	}
}

// gatedPrice honours gearFailed: a piece that failed to equip is no saving goal
func TestGatedPriceHonoursGearFailed(t *testing.T) {
	withMsItems(t)
	b := msBot("ec39failed", 60, 9103, 9201)
	b.MapName = "prontera"
	p := msBuild()
	builds[p.ID] = p
	t.Cleanup(func() { delete(builds, p.ID) })
	items[9202] = &Item{ID: 9202, Name: "Armor II", Type: "Armor", Loc: 0x10, Def: 37, MinLv: 21, Buy: 2000}
	gearShopsCache[9202] = []*Shop{{Map: "prontera", Items: []int{9202}}}
	t.Cleanup(func() { delete(gearShopsCache, 9202); delete(items, 9202) })
	if _, ok := b.gatedPrice(); !ok {
		t.Fatal("the gated Armor II should be a saving goal before it fails")
	}
	b.gearFailed = map[int]bool{9202: true}
	if _, ok := b.gatedPrice(); ok {
		t.Fatal("a gated piece in gearFailed must not be a saving goal")
	}
}

// the town shop visit (bestGearIn) buys no lesser gear while saving for the gated piece
func TestTownVisitRespectsSaveHold(t *testing.T) {
	withMsItems(t)
	b := msBot("ec39town", 60, 9103, 9201)
	b.MapName = "prontera"
	b.Zeny = 1000
	p := msBuild()
	builds[p.ID] = p
	t.Cleanup(func() { delete(builds, p.ID) })
	items[9202] = &Item{ID: 9202, Name: "Armor II", Type: "Armor", Loc: 0x10, Def: 37, MinLv: 21, Buy: 2000}
	gearShopsCache[9202] = []*Shop{{Map: "prontera", Items: []int{9202}}}
	t.Cleanup(func() { delete(gearShopsCache, 9202); delete(items, 9202) })
	// a cheap weapon sold in the town: it would be bought without the hold
	items[9401] = &Item{ID: 9401, Name: "Cheap Blade", Type: "Weapon", Loc: 0x2, Atk: 10, WLv: 1, MinLv: 1, Buy: 100}
	shopsByMap["prontera"] = append(shopsByMap["prontera"], &Shop{Map: "prontera", Items: []int{9401}})
	t.Cleanup(func() {
		delete(items, 9401)
		list := shopsByMap["prontera"]
		shopsByMap["prontera"] = list[:len(list)-1]
	})
	if it := b.bestGearIn("prontera"); it != nil {
		t.Fatalf("saving for Armor II (2000z) with 1000z: the town visit bought %s", it.Name)
	}
}
