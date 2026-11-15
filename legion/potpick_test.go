package main

import (
	"encoding/binary"
	"testing"
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

// QA 2026-11-15 (b1794773273mib): lv85 bots with 3,000-9,700 max HP restocked Red Potions (+45 HP) because they could
// not afford a full White/Yellow stock. With 3,000z at 9,700 max HP the Yellow stock is the buy, not Red.
func TestPickHPPotionNotRedAtHighHP(t *testing.T) {
	pk := shopPk(50, 501, 200, 502, 500, 503, 1200, 504)
	if id, pr := pickHPPotion(pk, 85, 9700, 3000); id != itemYellowPotion || pr != 500 {
		t.Fatalf("lv85 9700 HP 3000z: picked %d at %d, want Yellow at 500", id, pr)
	}
	// rich: White heals enough for 9,700 max HP and the stock is affordable
	if id, _ := pickHPPotion(pk, 85, 9700, 20000); id != itemWhitePotion {
		t.Fatalf("lv85 9700 HP 20000z: picked %d, want White", id)
	}
	// a 3,000 HP lv70 with 5,000z: Yellow (205 HP) clears 3% of max HP, Red does not
	if id, _ := pickHPPotion(pk, 70, 3000, 5000); id != itemYellowPotion {
		t.Fatalf("lv70 3000 HP 5000z: picked %d, want Yellow", id)
	}
}

// the level gates stay: Orange from lv30, Yellow from lv60, White from lv80; Red for everyone
func TestPickHPPotionLevelGates(t *testing.T) {
	pk := shopPk(50, 501, 200, 502, 500, 503, 1200, 504)
	if id, _ := pickHPPotion(pk, 45, 1600, 2000); id != itemOrangePotion {
		t.Fatalf("lv45 1600 HP 2000z: picked %d, want Orange", id)
	}
	// 1,000z cannot pay for a 5-potion Orange stock: the Red stock it can pay for
	if id, _ := pickHPPotion(pk, 45, 1600, 1000); id != itemRedPotion {
		t.Fatalf("lv45 1600 HP 1000z: picked %d, want Red", id)
	}
	if id, _ := pickHPPotion(pk, 25, 900, 20000); id != itemRedPotion {
		t.Fatalf("lv25: picked %d, want Red", id)
	}
	// a shop with only Red, a poor lv70: Red is the only thing bought
	if id, pr := pickHPPotion(shopPk(50, 501), 70, 3000, 5000); id != itemRedPotion || pr != 50 {
		t.Fatalf("Red-only shop: picked %d at %d", id, pr)
	}
	// nothing affordable: nothing bought
	if id, _ := pickHPPotion(pk, 70, 3000, 100); id != 0 {
		t.Fatalf("100z: picked %d, want none", id)
	}
}

// the trip goes to the town shop with the strongest potion, not the first one with Red
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

// the potion a bot restocks: the strongest its level allows that heals enough for its max HP
func TestPotTierFor(t *testing.T) {
	for _, c := range []struct{ lv, hp, want int }{
		{85, 9700, itemWhitePotion}, {70, 3000, itemYellowPotion}, {45, 1600, itemOrangePotion},
		{25, 400, itemRedPotion}, {85, 1000, itemWhitePotion}, {60, 9700, itemYellowPotion},
	} {
		if got := potTierFor(c.lv, c.hp); got != c.want {
			t.Errorf("potTierFor(%d, %d) = %d, want %d", c.lv, c.hp, got, c.want)
		}
	}
}
