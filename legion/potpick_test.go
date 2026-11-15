package main

import (
	"encoding/binary"
	"testing"
	"time"
)

// shopPk builds an NPC sell packet: (price, item id) pairs in the layout listedPrice reads (13 bytes per entry).
func shopPk(pairs ...int) []byte {
	pk := make([]byte, 4)
	for i := 0; i+1 < len(pairs); i += 2 {
		e := make([]byte, 13)
		binary.LittleEndian.PutUint32(e[4:], uint32(pairs[i]))
		binary.LittleEndian.PutUint32(e[9:], uint32(pairs[i+1]))
		pk = append(pk, e...)
	}
	return pk
}

// setPotionItems: the real NPC Buy prices of the HP potions (rAthena db/re item_db_usable.yml: Red 10, Orange 50,
// Yellow 180, White 1200). The heal values are potHeal's (55 / 125 / 205 / 365).
func setPotionItems() {
	items[501] = &Item{ID: 501, Name: "Red Potion", Type: "Healing", Buy: 10, Weight: 70}
	items[502] = &Item{ID: 502, Name: "Orange Potion", Type: "Healing", Buy: 50, Weight: 100}
	items[503] = &Item{ID: 503, Name: "Yellow Potion", Type: "Healing", Buy: 180, Weight: 100}
	items[504] = &Item{ID: 504, Name: "White Potion", Type: "Healing", Buy: 1200, Weight: 100}
}

// allPots: the four HP potions in one shop at their real prices (the Tool Dealers of Prontera, Payon, Morocc ...)
var allPots = shopPk(10, 501, 50, 502, 180, 503, 1200, 504)

// QA 2026-11-15 (review of 2694e5d): a lv80 bot with 6,500z and 6,000 max HP. The minimum (25 potions) is affordable
// in Orange (1,250z) and Yellow (4,500z); Orange gives the most heal per zeny among them (125 HP for 50z).
func TestPickHPPotionLv80Rich(t *testing.T) {
	setPotionItems()
	if id, pr := pickHPPotion(allPots, 80, 6000, 6500, 25); id != itemOrangePotion || pr != 50 {
		t.Fatalf("lv80 6000 HP 6500z: picked %d at %d, want Orange at 50", id, pr)
	}
}

// the review's second case: 2,000z. Red (55 HP for 10z) gives the most HP per zeny; Yellow (205 HP for 180z) would have
// given 8 potions = 1,640 HP where 25 Red give 1,375 HP for 250z (the old fallback bought Yellow here).
func TestPickHPPotionTwoThousandZeny(t *testing.T) {
	setPotionItems()
	if id, pr := pickHPPotion(allPots, 65, 1600, 2000, 20); id != itemRedPotion || pr != 10 {
		t.Fatalf("lv65 1600 HP 2000z: picked %d at %d, want Red at 10", id, pr)
	}
}

// B1794773273mib: a lv85 bot with 9,700 max HP and 3,000z. Its minimum is 25 potions: Yellow (13 affordable) and White
// (2 affordable) cannot be bought in full, so Red is the minimum (the strongest tier is bought as emergency stock).
func TestPickHPPotionHighHPMinimumInRed(t *testing.T) {
	setPotionItems()
	if id, pr := pickHPPotion(allPots, 85, 9700, 3000, 25); id != itemRedPotion || pr != 10 {
		t.Fatalf("lv85 9700 HP 3000z: picked %d at %d, want Red at 10 (the minimum of 25 fits in no stronger tier)", id, pr)
	}
	// with 6,500z the full minimum fits in Yellow (4,500z) and Yellow heals 2% of 9,700: a worthy tier
	if id, pr := pickHPPotion(allPots, 85, 9700, 6500, 25); id != itemYellowPotion || pr != 180 {
		t.Fatalf("lv85 9700 HP 6500z: picked %d at %d, want Yellow at 180", id, pr)
	}
}

// the review's band (2026-11-15, 5bdf081): the survival minimum comes first. A poor bot buys its minimum in Red, not
// 1-4 strong potions; a stronger tier is taken only when the full minimum fits in it.
func TestPickHPPotionSurvivalBand(t *testing.T) {
	setPotionItems()
	for _, c := range []struct {
		name                  string
		lv, maxHP, zeny, minN int
	}{
		{"lv85 9700HP 700z", 85, 9700, 700, 25},
		{"lv85 9700HP 800z", 85, 9700, 800, 25},
		{"lv85 9700HP 1300z", 85, 9700, 1300, 25},
		{"lv85 9700HP 2000z", 85, 9700, 2000, 25},
		{"lv70 9700HP 1000z", 70, 9700, 1000, 25},
		{"lv45 3000HP 700z", 45, 3000, 700, 15},
	} {
		if id, pr := pickHPPotion(allPots, c.lv, c.maxHP, c.zeny, c.minN); id != itemRedPotion || pr != 10 {
			t.Errorf("%s: picked %d at %d, want Red at 10", c.name, id, pr)
		}
	}
}

// the level gates stay: Orange from lv30, Yellow from lv60, White from lv80; Red for everyone
func TestPickHPPotionLevelGates(t *testing.T) {
	setPotionItems()
	if id, _ := pickHPPotion(allPots, 25, 900, 20000, 12); id != itemRedPotion {
		t.Fatalf("lv25: picked %d, want Red", id)
	}
	if id, _ := pickHPPotion(shopPk(10, 501, 50, 502), 45, 1600, 2000, 15); id != itemRedPotion {
		t.Fatalf("lv45 Red+Orange shop: picked %d, want Red (the most heal per zeny)", id)
	}
	// nothing affordable: nothing bought
	if id, _ := pickHPPotion(allPots, 70, 3000, 100, 12); id != 0 {
		t.Fatalf("100z: picked %d, want none", id)
	}
}

// the emergency stack (2026-11-15, third pass): the strongest tier above the bulk tier with at least 3 potions that fit
// the potion budget (1,000z floor) and the zeny beyond the survival minimum. A lv85 bot at 3,000z: White (1,200z) does
// not fit, so Yellow: the Red minimum plus 5 Yellow (900z). A lv60 bot at 9,700 HP and 2,000z: the same.
func TestEmergencyStackLv85RichPoor(t *testing.T) {
	setPotionItems()
	b := &Bot{P: &Persona{BaseLevel: 85, Chars: []CharSpec{{Name: "emlv85"}}}, invSeen: true, Zeny: 3000,
		MaxHP: 9700, HP: 9700, MaxWeight: 20000, inv: map[int]*InvItem{}, equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
	bulk, _ := pickHPPotion(allPots, 85, 9700, 3000, b.potMin())
	if bulk != itemRedPotion {
		t.Fatalf("lv85 9700 HP 3000z bulk %d, want the Red minimum", bulk)
	}
	survival := b.potMin() * 10
	id, pr, n := emergencyStack(b, allPots, 85, bulk, potBudgetFor(0), b.Zeny-potPickReserve-survival)
	if id != itemYellowPotion || pr != 180 || n != potEmergencyN {
		t.Fatalf("lv85 3000z emergency: %d at %d x%d, want Yellow at 180 x5", id, pr, n)
	}
	// the planned spend: minimum Red + emergency Yellow stays inside the zeny (packet total never exceeds it)
	hpN, spN, arrN := b.potBuyPlanEm(bulk, 10, 0, 0, 0, n*pr)
	if total := hpN*10 + spN*0 + arrN*0 + n*pr; total > b.Zeny-500 || hpN < b.potMin() {
		t.Fatalf("plan: %d Red + %d Yellow costs %d, zeny %d (minimum %d)", hpN, n, total, b.Zeny, b.potMin())
	}
}

func TestEmergencyStackLv60Poor(t *testing.T) {
	setPotionItems()
	b := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "emlv60"}}}, invSeen: true, Zeny: 2000,
		MaxHP: 9700, HP: 9700, MaxWeight: 20000, inv: map[int]*InvItem{}, equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
	bulk, _ := pickHPPotion(allPots, 60, 9700, 2000, b.potMin())
	if bulk != itemRedPotion {
		t.Fatalf("lv60 9700 HP 2000z bulk %d, want the Red minimum", bulk)
	}
	id, pr, n := emergencyStack(b, allPots, 60, bulk, potBudgetFor(0), b.Zeny-potPickReserve-b.potMin()*10)
	if id != itemYellowPotion || n != potEmergencyN || pr != 180 {
		t.Fatalf("lv60 2000z emergency: %d at %d x%d, want Yellow x5", id, pr, n)
	}
}

// too poor for 3 emergency potions of any stronger tier: no stack (the zeny goes to the bulk minimum)
func TestEmergencyStackNoneWhenPoor(t *testing.T) {
	setPotionItems()
	b := &Bot{Zeny: 800, inv: map[int]*InvItem{}}
	if id, _, n := emergencyStack(b, allPots, 85, itemRedPotion, potBudgetFor(0), 800-potPickReserve-250); id != 0 || n != 0 {
		t.Fatalf("800z lv85: emergency %d x%d, want none", id, n)
	}
	// the strongest tier is the bulk tier: no stack above it
	if id, _, _ := emergencyStack(b, allPots, 85, itemWhitePotion, 1000, 5000); id != 0 {
		t.Fatalf("bulk White: emergency %d, want none", id)
	}
}

// the bulk tier a restock keeps zeny for (gearKeep): lv80 at 6,000 max HP is Orange; lv25 is Red; the flag off is Red
func TestPotTierForBulk(t *testing.T) {
	setPotionItems()
	if got := potTierFor(80, 6000); got != itemOrangePotion {
		t.Fatalf("potTierFor(80, 6000) = %d, want Orange", got)
	}
	if got := potTierFor(25, 400); got != itemRedPotion {
		t.Fatalf("potTierFor(25, 400) = %d, want Red", got)
	}
	withFitOff(func() {
		if got := potTierFor(80, 6000); got != itemRedPotion {
			t.Errorf("fit_buy_off: potTierFor(80, 6000) = %d, want the pre-fit Red", got)
		}
	})
}

// the potion budget: 15% of the zeny per hour the bot really earns, never under the floor
func TestPotBudgetShareOfZenyPerHour(t *testing.T) {
	if got := potBudgetFor(0); got != 1000 {
		t.Fatalf("no measured hour: budget %d, want the 1000z floor", got)
	}
	if got := potBudgetFor(40000); got != 6000 {
		t.Fatalf("40,000 z/h: budget %d, want 6,000", got)
	}
	b := &Bot{}
	b.meter.sessFrom = time.Now().Add(-time.Hour)
	b.meter.record(time.Now(), func(s *meterSlot) { s.ZIn += 40000 })
	if got := b.potBudget(); got != 6000 {
		t.Fatalf("bot earning 40,000z in its last hour: budget %d, want 6,000", got)
	}
}

// tuning fit_buy_off true: the pre-fit pick (the highest tier the level allows, Red when a full stock is not affordable)
func TestFitBuyOffKeepsLegacyPotionPick(t *testing.T) {
	setPotionItems()
	b := &Bot{P: &Persona{BaseLevel: 65, Chars: []CharSpec{{Name: "legacypot"}}}, invSeen: true, Zeny: 2000,
		MaxHP: 1600, HP: 1600, inv: map[int]*InvItem{}, equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
	if id, pr := legacyHPPotion(b, allPots); id != itemRedPotion || pr != 10 {
		t.Fatalf("legacy lv65 2000z: picked %d at %d, want the Red fallback", id, pr)
	}
}

// QA 2026-11-14 / 2026-11-15: a shop with the strongest potion beats the first shop with Red in the same town
func TestPotionShopPrefersStrongestPotion(t *testing.T) {
	const m = "zz_potion_test"
	red := &Shop{Map: m, Name: "red", Items: []int{501}}
	white := &Shop{Map: m, Name: "white", Items: []int{501, 504}}
	shopsByMap[m] = []*Shop{red, white}
	defer delete(shopsByMap, m)
	if got := potionShop(m); got != white {
		t.Fatalf("potionShop picked %v, want the White seller", got)
	}
	if got := potionShop("zz_no_town"); got != nil {
		t.Fatalf("a town with no shop: %v", got)
	}
}
