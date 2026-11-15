package main

import (
	"strings"
	"testing"
	"time"
)

// regendrop_test.go: the town junk drop (regendrop.go). Every case goes through the real lowHPGuard hook. A case that
// must not drop asserts no "dropping" state; a drop case asserts the stack and the bag state.

const (
	testJunkID   = 9101 // Etc, cheap, heavy: junk an NPC buys
	testCardID   = 9102 // Card: never dropped
	testAmmoID   = 9103 // Ammo: never dropped
	testZeroID   = 9104 // Etc, Buy 0 (quest / unsellable): never dropped
	testMarketID = 9105 // Etc junk a market buyer wants: never dropped
	testSwordID  = 9106 // Weapon: equipped / the gear path: never dropped
	testPotID    = 501  // Red Potion
	testFoodID   = 507  // Red Herb (food)
)

func regenItems() {
	items[testJunkID] = &Item{ID: testJunkID, Name: "Junk", Type: "Etc", Buy: 4, Weight: 30}
	items[testCardID] = &Item{ID: testCardID, Name: "Card", Type: "Card", Buy: 1000, Weight: 10}
	items[testAmmoID] = &Item{ID: testAmmoID, Name: "Arrow", Type: "Ammo", Buy: 2, Weight: 10}
	items[testZeroID] = &Item{ID: testZeroID, Name: "Quest Thing", Type: "Etc", Buy: 0, Weight: 40}
	items[testMarketID] = &Item{ID: testMarketID, Name: "Wanted Junk", Type: "Etc", Buy: 4, Weight: 30}
	items[testSwordID] = &Item{ID: testSwordID, Name: "Sword", Type: "Weapon", Buy: 9000, Weight: 50}
}

// overweightTown: a Blacksmith in town at 58% HP, already resting, the weight taken from its bag, bag cap 1000.
func overweightTown(inv map[int]*InvItem) *Bot {
	b := townBlacksmith(2233, false)
	b.MaxWeight = 1000
	b.inv = inv
	b.invSeen = true
	b.buffAt["lowrest"] = time.Now()
	setInvWeight(b)
	return b
}

// setInvWeight: the bag weight is the sum of the bag (the server's weight is the inventory's).
func setInvWeight(b *Bot) {
	w := 0
	for _, it := range b.inv {
		if x := items[it.ItemID]; x != nil && it.Count > 0 {
			w += x.Weight * it.Count
		}
	}
	b.Weight = w
}

// hookDrops: one tick of the real hook; true when it dropped (state "dropping").
func hookDrops(b *Bot, now time.Time) bool {
	b.State = "" // a tick that returns early leaves the old state: only this tick may say "dropping"
	b.lowHPGuard(nil, false, now)
	return strings.Contains(b.State, "dropping")
}

// withTuning runs f with town_drop_junk set to on, then restores the defaults.
func withTownDrop(t *testing.T, on bool) {
	t.Helper()
	nt := defaultTuning
	nt.TownDropJunk = on
	curTuning.Store(&nt)
	t.Cleanup(func() { curTuning.Store(&defaultTuning) })
}

// SELL: a potion shop here buys the loot -> the trip logic sells it; the hook does not drop.
func TestRegenHookSellPath(t *testing.T) {
	regenItems()
	withTownDrop(t, true)
	saved := shopsByMap
	t.Cleanup(func() { shopsByMap = saved })
	shopsByMap = map[string][]*Shop{"aldebaran": {{Map: "aldebaran", Items: []int{itemRedPotion}}}}
	b := overweightTown(map[int]*InvItem{10: {Index: 10, ItemID: testJunkID, Count: 25}}) // 750 weight of junk
	b.Zeny = 0
	if b.regenLastResort() {
		t.Fatal("sell path: a shop here buys the loot, the drop must not own the case")
	}
	if hookDrops(b, time.Now()) {
		t.Fatalf("sell path: dropped with a shop to sell to (state %q)", b.State)
	}
}

// STORE: 200+ zeny and storable things (a card, never junk) -> the Kafra takes them; the hook does not drop.
func TestRegenHookStorePath(t *testing.T) {
	regenItems()
	withTownDrop(t, true)
	b := overweightTown(map[int]*InvItem{10: {Index: 10, ItemID: testCardID, Count: 80}}) // 800 weight of cards
	b.Zeny = 300
	if len(b.storeCandidates()) == 0 {
		t.Fatal("store path: the cards must be storable")
	}
	if b.regenLastResort() {
		t.Fatal("store path: the Kafra stores them, the drop must not own the case")
	}
	if hookDrops(b, time.Now()) {
		t.Fatalf("store path: dropped cards while the Kafra can store them (state %q)", b.State)
	}
}

// BUY: 500+ zeny with no potion -> the trip logic buys potions; the hook does not drop junk.
func TestRegenHookBuyPath(t *testing.T) {
	regenItems()
	withTownDrop(t, true)
	saved := shopsByMap
	t.Cleanup(func() { shopsByMap = saved })
	shopsByMap = map[string][]*Shop{} // no shop to sell to here
	b := overweightTown(map[int]*InvItem{10: {Index: 10, ItemID: testJunkID, Count: 25}})
	b.Zeny = 800
	if b.regenLastResort() {
		t.Fatal("buy path: 800 zeny buys potions, the drop must not own the case")
	}
	if hookDrops(b, time.Now()) {
		t.Fatalf("buy path: dropped junk while the bot can buy potions (state %q)", b.State)
	}
}

// DROP: no shop, under 200 zeny, nothing storable but junk. Junk alone clears the line -> the lowest value per weight
// stack goes, and the bag ends under the line.
func TestRegenHookDropPath(t *testing.T) {
	regenItems()
	withTownDrop(t, true)
	saved := shopsByMap
	t.Cleanup(func() { shopsByMap = saved })
	shopsByMap = map[string][]*Shop{}
	b := overweightTown(map[int]*InvItem{
		10: {Index: 10, ItemID: testJunkID, Count: 20},                 // 600 weight, sells for 2 each
		11: {Index: 11, ItemID: testSwordID, Count: 1, Equipped: true}, // 50 weight: kept
		12: {Index: 12, ItemID: testPotID, Count: 0},                   // empty slot
	})
	b.Zeny = 0
	if !b.regenLastResort() {
		t.Fatalf("drop path: must be the last resort (store %v, loot %v)", b.storeCandidates(), b.hasLoot())
	}
	if !hookDrops(b, time.Now()) {
		t.Fatalf("drop path: junk clears the line, nothing dropped (state %q)", b.State)
	}
	if b.rdrop.pend == nil || b.rdrop.pend.itemID != testJunkID || b.rdrop.pend.n != 20 {
		t.Fatalf("drop path: the in-flight drop should be the junk stack, got %+v", b.rdrop.pend)
	}
}

// Only junk that brings the bag under the line is dropped: the bag is 790 with 90 of junk (the rest protected cards),
// the line needs 300 gone -> drop nothing.
func TestRegenDropNothingWhenJunkInsufficient(t *testing.T) {
	regenItems()
	withTownDrop(t, true)
	b := overweightTown(map[int]*InvItem{
		0: {Index: 10, ItemID: testJunkID, Count: 3},
		1: {Index: 11, ItemID: testCardID, Count: 70},
	})
	b.Zeny = 0
	if hookDrops(b, time.Now()) {
		t.Fatalf("junk alone cannot clear the line: nothing may be dropped (state %q)", b.State)
	}
	if b.rdrop.pend != nil {
		t.Fatal("no drop may be in flight")
	}
}

// Protected kinds: a bag of only that kind, over the line, no shop, no zeny: nothing is dropped.
func TestRegenDropProtectedKinds(t *testing.T) {
	regenItems()
	withTownDrop(t, true)
	saved := shopsByMap
	t.Cleanup(func() { shopsByMap = saved })
	shopsByMap = map[string][]*Shop{}
	var valuable int
	for id := range valuableEtc {
		if items[id] == nil {
			items[id] = &Item{ID: id, Name: "Gem", Type: "Etc", Buy: 2000, Weight: 10}
		}
		valuable = id
		break
	}
	marketCase := testMarketID
	market2.Lock()
	if market2.demand == nil {
		market2.demand = map[int]int{}
	}
	market2.demand[marketCase] = 3
	market2.Unlock()
	t.Cleanup(func() {
		market2.Lock()
		delete(market2.demand, marketCase)
		market2.Unlock()
	})
	kinds := []struct {
		name string
		inv  *InvItem
		prep func(b *Bot)
	}{
		{"card", &InvItem{ItemID: testCardID, Count: 80}, nil},
		{"ammo", &InvItem{ItemID: testAmmoID, Count: 80}, nil},
		{"buy 0", &InvItem{ItemID: testZeroID, Count: 20}, nil},
		{"valuable etc", &InvItem{ItemID: valuable, Count: 80}, nil},
		{"market-wanted junk", &InvItem{ItemID: testMarketID, Count: 27}, nil},
		{"equipped", &InvItem{ItemID: testSwordID, Count: 1, Equipped: true}, nil},
		{"potion", &InvItem{ItemID: testPotID, Count: 1}, nil},
		{"food", &InvItem{ItemID: testFoodID, Count: 1}, nil},
		{"gear the gear path keeps", &InvItem{ItemID: testSwordID, Count: 16}, func(b *Bot) { b.equipWanted = testSwordID }},
	}
	for _, k := range kinds {
		b := overweightTown(map[int]*InvItem{0: k.inv})
		b.Zeny = 0
		if k.prep != nil {
			k.prep(b)
		}
		if n := len(b.regenJunk()); n != 0 {
			t.Errorf("%s: protected kind listed as droppable junk (%d stacks)", k.name, n)
		}
		if hookDrops(b, time.Now()) {
			t.Errorf("%s: dropped a protected kind (state %q)", k.name, b.State)
		}
	}
}

// A refused drop (the server keeps the stack) is remembered and never retried.
func TestRegenDropRefusedNeverRetried(t *testing.T) {
	regenItems()
	withTownDrop(t, true)
	saved := shopsByMap
	t.Cleanup(func() { shopsByMap = saved })
	shopsByMap = map[string][]*Shop{}
	b := overweightTown(map[int]*InvItem{10: {Index: 10, ItemID: testJunkID, Count: 25}})
	b.Zeny = 0
	now := time.Now()
	if !hookDrops(b, now) {
		t.Fatal("first drop must go out")
	}
	// the server refuses: the stack stays in the bag. After the wait the bot remembers it.
	now = now.Add(regenDropWait + time.Second)
	hookDrops(b, now)
	if !b.regenRefused(b.inv[10]) {
		t.Fatal("a stack still in the bag after the wait must be remembered as refused")
	}
	for i := 0; i < 20; i++ {
		now = now.Add(regenDropGap + time.Second)
		if hookDrops(b, now) {
			t.Fatalf("a refused stack was dropped again (state %q)", b.State)
		}
	}
}

// A confirmed drop (the stack leaves the bag) counts toward the visit cap, and the bot never picks it up again.
func TestRegenDropConfirmedAndNeverPickedUpAgain(t *testing.T) {
	regenItems()
	withTownDrop(t, true)
	saved := shopsByMap
	t.Cleanup(func() { shopsByMap = saved })
	shopsByMap = map[string][]*Shop{}
	b := overweightTown(map[int]*InvItem{10: {Index: 10, ItemID: testJunkID, Count: 25}})
	b.Zeny = 0
	b.X, b.Y = 140, 120
	now := time.Now()
	if !hookDrops(b, now) {
		t.Fatal("drop must go out")
	}
	// the server confirms: the stack is gone from the bag
	delete(b.inv, 10)
	setInvWeight(b)
	now = now.Add(regenDropWait + time.Second)
	hookDrops(b, now)
	if b.rdrop.visit != 1 {
		t.Fatalf("a confirmed drop must count on the visit (got %d)", b.rdrop.visit)
	}
	near := &GroundItem{ItemID: testJunkID, X: 141, Y: 121, Seen: now}
	if !b.regenOwnDrop(near, now) {
		t.Fatal("the bot's own drop must not be picked up again")
	}
	far := &GroundItem{ItemID: testJunkID, X: 200, Y: 200, Seen: now}
	if b.regenOwnDrop(far, now) {
		t.Fatal("a stack of the same kind far from the drop is not the bot's own")
	}
	if !b.regenOwnDrop(near, now.Add(regenOwnLifetime-time.Minute)) || b.regenOwnDrop(near, now.Add(regenOwnLifetime+time.Minute)) {
		t.Fatal("the own-drop memory lasts for regenOwnLifetime")
	}
}

// The visit cap: after regenDropCap confirmed stacks the bot drops nothing more on this visit.
func TestRegenDropVisitCap(t *testing.T) {
	regenItems()
	withTownDrop(t, true)
	saved := shopsByMap
	t.Cleanup(func() { shopsByMap = saved })
	shopsByMap = map[string][]*Shop{}
	b := overweightTown(map[int]*InvItem{10: {Index: 10, ItemID: testJunkID, Count: 25}})
	b.Zeny = 0
	b.rdrop.visit, b.rdrop.town = regenDropCap, b.MapName // this visit has used its cap
	if hookDrops(b, time.Now()) {
		t.Fatal("the visit cap must stop the drops")
	}
}

// tuning.json town_drop_junk false: the drop is off, the bot waits for the trip logic as before.
func TestRegenDropFlagOff(t *testing.T) {
	regenItems()
	withTownDrop(t, false)
	saved := shopsByMap
	t.Cleanup(func() { shopsByMap = saved })
	shopsByMap = map[string][]*Shop{}
	b := overweightTown(map[int]*InvItem{10: {Index: 10, ItemID: testJunkID, Count: 25}})
	b.Zeny = 0
	if hookDrops(b, time.Now()) {
		t.Fatal("town_drop_junk off: nothing may be dropped")
	}
}
