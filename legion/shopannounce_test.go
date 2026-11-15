package main

import (
	"strings"
	"testing"
	"time"
)

// EC35: vendors announce only the rare items their open shop lists, at most every 10-20 min each, within the
// economy's #trade gap.
func TestShopAnnounce(t *testing.T) {
	items[92001] = &Item{ID: 92001, Name: "Test Mineral Card", Type: "Card", Buy: 20}
	items[92002] = &Item{ID: 92002, Name: "Test Katana", Type: "Weapon", Buy: 2000, Slots: 3}
	items[92003] = &Item{ID: 92003, Name: "Test Jellopy", Type: "Etc", Buy: 6}
	items[92004] = &Item{ID: 92004, Name: "Test Arrow", Type: "Ammo", Buy: 1}
	items[92005] = &Item{ID: 92005, Name: "Test Cheap Sword", Type: "Weapon", Buy: 100}
	if !rareShopItem(92001, 45000) || !rareShopItem(92002, 1400) {
		t.Fatal("cards and slotted gear from 1k are rare")
	}
	if rareShopItem(501, 50) || rareShopItem(92003, 3) || rareShopItem(92004, 5000) || rareShopItem(92005, 200) {
		t.Fatal("potions, junk, ammo and cheap gear are not announced")
	}
	shop := []vendListing{{ItemID: 92003, Count: 50, Price: 3}, {ItemID: 92002, Count: 1, Price: 1400}, {ItemID: 92001, Count: 1, Price: 45000}}
	r := rareListings(shop)
	if len(r) != 2 || r[0].ItemID != 92001 {
		t.Fatalf("rareListings = %+v", r)
	}
	for i := 0; i < 200; i++ {
		l := shopAnnLine(r, 160, 60)
		if !strings.Contains(l, "Test Mineral Card") && !strings.Contains(l, "Test Katana") || strings.Contains(l, "Jellopy") {
			t.Fatalf("line %q", l)
		}
		for _, bad := range []string{"bot", "AI", "script", "auto"} {
			if strings.Contains(l, bad) {
				t.Fatalf("line %q has %q", l, bad)
			}
		}
	}

	// scheduling: first sight books a later time; then one line; then silence for 10+ min; gap respected
	shopAnn.next, shopAnn.last = map[string]time.Time{}, time.Time{}
	econ.mu.Lock()
	econ.lastLine = time.Time{}
	econ.mu.Unlock()
	now := time.Now()
	s := &econSnap{b: &Bot{}, name: "vendA", shop: shop, openedAt: now.Add(-time.Hour)}
	if got, _ := shopAnnPick([]*econSnap{s}, now); got != nil {
		t.Fatal("first sight must not speak at once")
	}
	now = now.Add(shopAnnFirstMax + time.Second)
	got, rare := shopAnnPick([]*econSnap{s}, now)
	if got != s || len(rare) != 2 {
		t.Fatal("due merchant with rare items speaks")
	}
	if g, _ := shopAnnPick([]*econSnap{s}, now.Add(9*time.Minute)); g != nil {
		t.Fatal("at most once every 10 min per merchant")
	}
	if g, _ := shopAnnPick([]*econSnap{s}, now.Add(shopAnnMaxGap+time.Second)); g != s {
		t.Fatal("speaks again after 20 min")
	}
	// sold out: the rare items are gone from the listing -> nothing to announce
	s.shop = []vendListing{{ItemID: 92003, Count: 50, Price: 3}}
	if g, _ := shopAnnPick([]*econSnap{s}, now.Add(2*shopAnnMaxGap+time.Minute)); g != nil {
		t.Fatal("no rare item left: silent")
	}
	// econ gap: a B>/S> line just went out
	s.shop = shop
	t2 := now.Add(4 * shopAnnMaxGap)
	econ.mu.Lock()
	econ.lastLine = t2.Add(-time.Second)
	econ.mu.Unlock()
	if g, _ := shopAnnPick([]*econSnap{s}, t2); g != nil {
		t.Fatal("must wait for the economy's #trade gap")
	}
}
