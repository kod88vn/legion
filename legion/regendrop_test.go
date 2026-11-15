package main

import (
	"testing"
	"time"
)

// regendrop_test.go: the four paths for a bot over the regen line in town (regendrop.go). Each test names the path that
// owns the case: sell, store and buy are the trip logic's (regenLastResort false); drop is the last resort (tick loop).

const (
	testJunkID  = 9101 // Etc, cheap and heavy: npcJunk, sellable to an NPC
	testCardID  = 9102 // Card: not junk, storable, not sold to an NPC
	testWeapon1 = 9103 // Weapon, cheap, heavy: not sellable (junkGear may still say otherwise, see the drop test)
	testWeapon2 = 9104 // Weapon, valuable per weight: the drop must keep it
)

func regenItems() {
	items[testJunkID] = &Item{ID: testJunkID, Name: "Junk", Type: "Etc", Buy: 4, Weight: 30}
	items[testCardID] = &Item{ID: testCardID, Name: "Card", Type: "Card", Buy: 1000, Weight: 10}
	items[testWeapon1] = &Item{ID: testWeapon1, Name: "Heavy Card", Type: "Card", Buy: 10, Weight: 500}
	items[testWeapon2] = &Item{ID: testWeapon2, Name: "Good Sword", Type: "Weapon", Buy: 9000, Weight: 50}
}

// overweightTown: a Blacksmith in town at 58% HP, already resting, weight over the line from its bag.
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

// SELL: loot to sell -> the trip logic sells it; the drop path does not touch it.
func TestRegenPathSell(t *testing.T) {
	regenItems()
	b := overweightTown(map[int]*InvItem{0: {Index: 0, ItemID: testJunkID, Count: 40}})
	b.Zeny = 0
	if !b.hasLoot() {
		t.Fatal("junk Etc items must count as loot to sell")
	}
	if b.regenLastResort() {
		t.Fatal("sell path: loot in the bag, the trip logic sells it, not the drop")
	}
	if b.regenDrop(time.Now()) {
		t.Fatal("sell path: nothing dropped while there is loot")
	}
}

// STORE: no loot, but storable things and 200+ zeny -> the Kafra takes them (storeNeed / storeThink).
func TestRegenPathStore(t *testing.T) {
	regenItems()
	b := overweightTown(map[int]*InvItem{0: {Index: 0, ItemID: testCardID, Count: 30}})
	b.Zeny = 300
	if b.hasLoot() || len(b.storeCandidates()) == 0 {
		t.Fatalf("store path: want storable items and no loot (loot %v, store %v)", b.hasLoot(), b.storeCandidates())
	}
	if b.regenLastResort() {
		t.Fatal("store path: the Kafra stores them, the bot must not drop them")
	}
}

// BUY: no loot, nothing to store, 500+ zeny and no potion -> the trip logic buys potions ("out of potions").
func TestRegenPathBuy(t *testing.T) {
	regenItems()
	b := overweightTown(map[int]*InvItem{0: {Index: 0, ItemID: testWeapon2, Count: 1, Equipped: true}})
	b.Zeny = 800
	if b.regenLastResort() {
		t.Fatal("buy path: 800 zeny buys potions, the bot must not drop anything")
	}
	if b.potionCount() != 0 || b.Zeny < 500 {
		t.Fatalf("buy path setup: potions %d zeny %d", b.potionCount(), b.Zeny)
	}
}

// DROP: no loot, nothing to store, under 200 zeny, no potion, over the line -> drop the lowest value per weight item
// (never equipped, never a potion) one stack at a time until under 50%, and the bot stops sitting for a regen that
// never comes.
func TestRegenPathDrop(t *testing.T) {
	regenItems()
	b := overweightTown(map[int]*InvItem{
		10: {Index: 10, ItemID: testWeapon1, Count: 1}, // 500 weight, sells for 5: lowest ratio
		11: {Index: 11, ItemID: testWeapon2, Count: 4}, // 200 weight, sells for 4500 each: keep
		12: {Index: 12, ItemID: testWeapon2, Count: 1, Equipped: true},
	})
	b.Zeny = 0
	if !b.regenLastResort() {
		t.Fatalf("drop path: no loot, no potion, 0 zeny, over the line must be the last resort (loot %v, store %v)",
			b.hasLoot(), b.storeCandidates())
	}
	first := b.regenDropPick()
	if first == nil || first.ItemID != testWeapon1 {
		t.Fatalf("drop path: want the lowest value per weight (item %d) first, got %+v", testWeapon1, first)
	}
	drops := 0
	now := time.Now()
	for i := 0; i < 200 && b.overRegenWeight(); i++ {
		now = now.Add(time.Second)
		if b.lowHPGuard(nil, false, now) && b.State != "" && b.inv[10] != nil && b.inv[10].Count == 0 {
			drops++
		}
		setInvWeight(b)
	}
	if drops == 0 {
		t.Fatal("drop path: nothing was dropped")
	}
	if b.overRegenWeight() {
		t.Fatalf("drop path: still over the regen line (%d%%) after dropping", b.weightPct())
	}
	if b.potionCount() != 0 || b.inv[11].Count != 4 || b.inv[12].Count != 1 {
		t.Fatal("drop path: the kept items (sword, equipped gear) were dropped")
	}
}
