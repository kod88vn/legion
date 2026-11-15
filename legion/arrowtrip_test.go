package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// QA 2026-11-14 07:50: arrow trips looped (Geffen/Alberta: 0 of 41 bots got arrows). The engine read the static shop
// files, but rAthena's Dealer_Update.txt (feature.barter) unloads the arrow-selling Tool Dealers and strips Arrows from
// the Weapon Dealers. Arrow trips must go to a shop that really sells Arrows, and a failed visit must never loop.
func TestArrowShopChoiceAndNoLoop(t *testing.T) {
	saved := shopsByMap
	defer func() { shopsByMap = saved }()
	shopsByMap = map[string][]*Shop{}
	add := func(m string, x, y int, id string, its ...int) *Shop {
		s := &Shop{Map: m, X: x, Y: y, Name: id, ID: id, Items: its}
		shopsByMap[m] = append(shopsByMap[m], s)
		return s
	}
	add("geffen_in", 77, 167, "Tool Dealer#gef", 1750, 501)
	extGef := add("geffen_in", 77, 167, "Tool Dealer#Extended_Gef", 501, 601)
	wgef := add("geffen_in", 30, 178, "Weapon Dealer#gef2", 1750, 1751, 1701)
	add("prontera", 164, 57, "Tools Merchant#lgToo0", 501, 502)
	lgWea := add("prontera", 142, 57, "Weapons Merchant#lgWea0", 1701, 1750)
	add("izlude_in", 57, 110, "Tool Dealer#iz", 501, 1750)

	f := filepath.Join(t.TempDir(), "Dealer_Update.txt")
	os.WriteFile(f, []byte(`OnInit:
	if ( getbattleflag("feature.barter") ) {
		unloadnpc "Tool Dealer#gef";
		npcshopdelitem "Weapon Dealer#gef2",1750,1751;
	} else {
		unloadnpc "Tool Dealer#Extended_Gef";
	}
`), 0o644)
	applyBarterDealers(f)
	if len(shopsByMap["geffen_in"]) != 2 || shopsByMap["geffen_in"][0] != extGef {
		t.Fatalf("Tool Dealer#gef must be unloaded, Extended kept: %+v", shopsByMap["geffen_in"])
	}
	if shopSells(wgef, 1750) || !shopSells(wgef, 1701) {
		t.Fatalf("Weapon Dealer#gef2 items %v", wgef.Items)
	}
	if arrowShop("geffen", nil) != nil {
		t.Fatal("Geffen sells no arrows on this server")
	}
	if arrowShop("prontera", nil) != lgWea {
		t.Fatal("Prontera's arrows: the south gate Weapons Merchant")
	}

	items[1750] = &Item{ID: 1750, Name: "Arrow", Type: "Ammo", Sub: "Arrow", Buy: 1, Weight: 1}
	items[1701] = &Item{ID: 1701, Name: "Bow", Type: "Weapon", Sub: "Bow", Loc: 0x22, Atk: 15}
	items[501] = &Item{ID: 501, Name: "Red Potion", Type: "Healing", Buy: 50, Weight: 70}
	bow := &InvItem{Index: 2, ItemID: 1701, Count: 1, Loc: 0x22, Equipped: true}
	b := &Bot{P: &Persona{BaseLevel: 14, Chars: []CharSpec{{Name: "arrowtest"}}}, invSeen: true, Zeny: 5000, MaxWeight: 10000, Weight: 1000, MapName: "prontera",
		inv: map[int]*InvItem{2: bow, 5: {Index: 5, ItemID: 501, Count: 20}}, equipped: map[uint32]*InvItem{0x2: bow},
		gearFailed: map[int]bool{}}
	if got := b.arrowTown("geffen"); got != "prontera" {
		t.Fatalf("arrowTown(geffen) from prontera = %q", got)
	}
	if b.needTrip() != "no arrows" {
		t.Fatalf("needTrip = %q", b.needTrip())
	}
	// the visit gives no arrows (the buy list lacks 1750): the shop is marked failed, the need rests 20 min
	b.trip = &Trip{Stage: "buying", Town: "prontera", Shop: lgWea, Why: "ammo", Started: time.Now(), At: time.Now()}
	b.npcDeal = &npcDeal{npc: 1, want: "buy"}
	pk := make([]byte, 4+13)
	binary.LittleEndian.PutUint32(pk[4+4:], 50)
	binary.LittleEndian.PutUint32(pk[4+9:], 501)
	b.onDeal(0x00c6, pk)
	if !b.arrowFailed[shopKey(lgWea)] {
		t.Fatal("a shop without arrows must be marked failed")
	}
	if b.needTrip() == "no arrows" {
		t.Fatal("no-arrows need must rest 20 min after a failed visit (no loop)")
	}
	if arrowShop("prontera", b.arrowFailed) != nil {
		t.Fatal("the failed shop is skipped")
	}
	b.arrowNeedAt = time.Now().Add(-time.Second)
	if b.needTrip() != "no arrows" {
		t.Fatal("after the cooldown the need is back")
	}
	// QA 08:40: a bow user with no arrows and 97-468z must qualify AND buy (arrows first, before the 500z potion reserve)
	b.Zeny = 97
	if b.needTrip() != "no arrows" || b.arrowsWanted(1, false) != 47 {
		t.Fatalf("97z: need %q, wanted %d", b.needTrip(), b.arrowsWanted(1, false))
	}
	iz := arrowShop("izlude", b.arrowFailed)
	b.Zeny = 399
	b.trip = &Trip{Stage: "buying", Town: "izlude", Shop: iz, Why: "ammo", Started: time.Now(), At: time.Now()}
	b.npcDeal = &npcDeal{npc: 1, want: "buy"}
	pk2 := make([]byte, 4+26)
	binary.LittleEndian.PutUint32(pk2[4+4:], 50)
	binary.LittleEndian.PutUint32(pk2[4+9:], 501)
	binary.LittleEndian.PutUint32(pk2[17+4:], 1)
	binary.LittleEndian.PutUint32(pk2[17+9:], 1750)
	b.arrowNeedAt = time.Time{}
	b.onDeal(0x00c6, pk2)
	if b.arrowFailed[shopKey(iz)] || !b.arrowNeedAt.IsZero() {
		t.Fatal("399z at a real arrow seller must buy arrows, not fail")
	}
	// too poor to buy a useful stack: no futile trip
	b.Zeny = 60
	if b.needTrip() == "no arrows" {
		t.Fatal("a trip that can't buy arrows is futile")
	}
}
