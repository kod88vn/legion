package main

import (
	"strings"
	"testing"
)

func TestTradeLineNoTypo(t *testing.T) {
	for i := 0; i < 2000; i++ {
		for _, st := range []string{"casual slang, abbreviations", "lowercase", "terse", "proper"} {
			out := styled(st, "B> Hat 930z, Rare Sandals please")
			l := strings.ToLower(out)
			if !strings.Contains(l, "hat 930z") || !strings.Contains(l, "rare sandals") {
				t.Fatalf("trade line scrambled: %q (%s)", out, st)
			}
		}
	}
	if got := replaceWord("are you aware of Rare items", "are", "r"); got != "r you aware of Rare items" {
		t.Fatalf("replaceWord: %q", got)
	}
	for i := 0; i < 500; i++ {
		out := typo("selling my Katana now")
		if !strings.Contains(out, "Katana") || !strings.Contains(out, "my") {
			t.Fatalf("typo hit an item name: %q", out)
		}
	}
}

func TestTitleMatchesListing(t *testing.T) {
	if len(items) == 0 {
		t.Skip("no item db")
	}
	id := func(name string) int {
		for i, x := range items {
			if x != nil && x.Name == name {
				return i
			}
		}
		return 0
	}
	apple, red, poring := id("Apple"), id("Red Potion"), id("Poring Card")
	if apple == 0 || red == 0 || poring == 0 {
		t.Skip("items missing")
	}
	l := []vendListing{{ItemID: apple, Count: 5}}
	if titleMatchesListing("S> Red Potion, Poring Card", l) {
		t.Fatal("stale title accepted")
	}
	if !titleMatchesListing("S> Apple", l) || !titleMatchesListing("cheap stuff here!", l) {
		t.Fatal("good title refused")
	}
	if got := listingTitle([]vendListing{{ItemID: apple}, {ItemID: poring}}); !strings.Contains(got, "Poring Card") || !strings.HasPrefix(got, "S> ") {
		t.Fatalf("listingTitle %q", got)
	}
}

func TestLootSellableSoloKeepsOne(t *testing.T) {
	var card int
	for id, x := range items {
		if x != nil && x.Type == "Card" {
			card = id
			break
		}
	}
	if card == 0 {
		t.Skip("no item db")
	}
	b := &Bot{P: &Persona{BaseLevel: 30, Chars: []CharSpec{{Name: "LootTest"}}}, invSeen: true,
		inv: map[int]*InvItem{2: {Index: 2, ItemID: card, Count: 3}}}
	if n := b.lootSellable(card, 3, true); n != 2 {
		t.Fatalf("solo: %d sellable, want 2", n)
	}
	if n := b.lootSellable(card, 3, false); n != 0 {
		t.Fatalf("in a guild/party, new loot: %d sellable, want 0 until the flow reaches the sale", n)
	}
}
