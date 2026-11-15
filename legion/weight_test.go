package main

import "testing"

// weight.go: over the regen line, food is eaten at any HP loss and enough sellable loot is a town-run need.
func TestRegenWeightNeedAndSnack(t *testing.T) {
	items[512] = &Item{ID: 512, Name: "Apple", Type: "Healing", Buy: 15, Weight: 20}
	items[517] = &Item{ID: 517, Name: "Meat", Type: "Healing", Buy: 50, Weight: 150}
	items[569] = &Item{ID: 569, Name: "Novice Potion", Type: "Healing", Weight: 10}
	items[90920] = &Item{ID: 90920, Name: "TestJellopy", Type: "Etc", Buy: 6, Weight: 10}
	items[90921] = &Item{ID: 90921, Name: "TestClub", Type: "Weapon", Buy: 1000, Weight: 700, Loc: 0x2}
	b := &Bot{P: &Persona{BaseLevel: 38, Chars: []CharSpec{{Name: "weighttest"}}}, invSeen: true, MaxHP: 1000, HP: 990,
		MaxWeight: 10000, Weight: 4000, inv: map[int]*InvItem{
			1: {Index: 1, ItemID: 512, Count: 50}, 2: {Index: 2, ItemID: 517, Count: 2}, 3: {Index: 3, ItemID: 569, Count: 100}}}
	if b.snackIndex() != -1 {
		t.Fatal("under the line, 10 HP missing: Apple (19) would overheal, nothing to eat")
	}
	if b.regenWeightNeed() != "" {
		t.Fatal("under the line: no weight need")
	}
	b.Weight = 6000
	if b.snackIndex() != 2 {
		t.Fatalf("over the line: heaviest food first, got %d", b.snackIndex())
	}
	delete(b.inv, 2)
	if b.snackIndex() != 1 {
		t.Fatal("over the line: Apple next")
	}
	delete(b.inv, 1)
	if b.snackIndex() != 3 {
		t.Fatal("over the line, lv20+: surplus Novice Potions")
	}
	b.HP = b.MaxHP
	if b.snackIndex() != -1 {
		t.Fatal("full HP: nothing eaten")
	}
	// owner: 50% weight triggers a town run at once - with anything to sell; a bag heavy only from gear gains nothing
	b.inv[4] = &InvItem{Index: 4, ItemID: 90921, Count: 1}
	b.Weight = 5100
	if got := b.regenWeightNeed(); got != "" {
		t.Fatalf("nothing sellable over the line: %q", got)
	}
	b.inv[5] = &InvItem{Index: 5, ItemID: 90920, Count: 1}
	// a trip at once: this bot has no Zeny for a store visit, so the sell trip (weight.go regenWeightNeed)
	if got := b.regenWeightNeed(); got != "bag is heavy" {
		t.Fatalf("51%% with loot: %q", got)
	}
	b.Weight = 4900
	if got := b.regenWeightNeed(); got != "" {
		t.Fatalf("under the line: %q", got)
	}
}
