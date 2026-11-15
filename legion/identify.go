package main

import (
	"fmt"
	"time"
)

// Unidentified gear. Lesson (2026-11-14): equipment a monster drops is UNIDENTIFIED, and rAthena refuses to equip it
// (clif_parse_EquipItem -> ITEM_EQUIP_ACK_FAIL). The bots didn't know: equipFromInventory tried the looted Club, the
// refusal marked the item ID (1501) gearFailed for the whole session, so the bot then also refused to BUY a Club, and
// weaponNeed sent it shopping for a Rod while two Clubs sat in its bag (Thorn.rose, Holy_bunny: "bought the Club twice,
// never equipped" - they were drops). Now: the identify flag is parsed (0x0b39 / 0x0b41), unidentified items never
// count as wearable, an equip refusal marks only that inventory item, and a Magnifier (Tool Dealer, 40z) identifies
// drops worth wearing.

const itemMagnifier = 611

// wearable: this inventory item can go on now (identified, not refused by the server, job/level ok).
func (b *Bot) wearable(it *InvItem, x *Item) bool {
	return it != nil && x != nil && it.Count > 0 && !it.Unidentified && !it.NoEquip && b.canEquip(x)
}

// identifyWant: the unidentified item in the bag that gains most once identified (b.mu held). Run 7: a piece that
// fills an EMPTY slot first (class QA 15:30: TheRyufox68, Knight 60, wore only Katana + Adventurer's Suit with an
// unidentified Hood, Sandals and Fancy Flower in the bag), then the biggest upgrade.
func (b *Bot) identifyWant() *InvItem {
	var best *InvItem
	bestGain := 0
	for _, it := range b.inv {
		if !it.Unidentified || it.Equipped || it.Count <= 0 {
			continue
		}
		x := items[it.ItemID]
		if x == nil || x.Loc == 0 {
			continue
		}
		// a vendor identifies drops worth selling: rAthena refuses a whole shop with one unidentified item in it
		if b.charRole() == "vendor" && vendIdentifyWorth(x) {
			return it
		}
		if !b.canEquip(x) {
			continue
		}
		cur := b.curIn(gearSlot(x.Loc))
		gain := b.itemScore(x) - b.itemScore(cur)
		if cur == nil {
			gain += 1000 // an empty slot
		}
		if gain > 0 && gain > bestGain {
			best, bestGain = it, gain
		}
	}
	return best
}

// magnifiersWanted: Magnifiers to buy at a shop listing them (pk = the 0x00c6 buy list): one per unidentified upgrade
// in the bag, at most 2 carried.
//
// Run 7 (class QA 15:30: 2,190 unidentified equipment pieces on 88% of 830 bots, 53 with one that fills an empty slot
// or beats a worn one, none carrying a Magnifier): a Magnifier (40z) is a standing basic utility like Fly Wings -
// every bot from lv10 keeps 2 and tops up on every shop visit that lists them, paid before the potion top-up's
// spare zeny (only the 300z floor), and 1 more per unidentified upgrade waiting (at most 4).
func (b *Bot) magnifiersWanted(pk []byte, budget int) int {
	if b.BaseLvOr() < 10 && b.identifyWant() == nil {
		return 0
	}
	have := 0
	if idx := b.invIndex(itemMagnifier); idx >= 0 {
		have = b.inv[idx].Count
	}
	want := 2
	if b.identifyWant() != nil {
		want = 4
	}
	for o := 4; o+13 <= len(pk); o += 13 {
		pr, id := int(le.Uint32(pk[o+4:])), int(le.Uint32(pk[o+9:]))
		if id == itemMagnifier && pr > 0 {
			return max(0, min(want-have, max(budget, b.Zeny-300)/pr))
		}
	}
	return 0
}

// identifyThink: use a Magnifier on an unidentified upgrade (progress(), b.mu held). The server answers with the
// list of unidentified items (0x0177); onIdentify picks ours.
func (b *Bot) identifyThink(now time.Time) {
	if now.Before(b.identAt) || b.target != 0 {
		return
	}
	it := b.identifyWant()
	if it == nil {
		return
	}
	idx := b.invIndex(itemMagnifier)
	if idx < 0 {
		return
	}
	b.identAt, b.identIdx = now.Add(15*time.Second), it.Index
	go b.useItem(idx)
}

// onIdentify: 0x0177 identify list (len, then client indices) -> 0x0178 our pick; 0x0179 result (index, 0 = ok).
func (b *Bot) onIdentify(op uint16, pk []byte) {
	switch op {
	case 0x0177:
		pick := -1
		for o := 4; o+2 <= len(pk); o += 2 {
			idx := int(le.Uint16(pk[o:]))
			if idx == b.identIdx || pick < 0 {
				pick = idx
			}
		}
		if pick >= 0 {
			go b.send(newPkt(0x0178).u16(uint16(pick)).b)
		}
	case 0x0179:
		if len(pk) >= 5 && pk[4] == 0 {
			if it := b.inv[int(le.Uint16(pk[2:]))]; it != nil {
				it.Unidentified, it.NoEquip = false, false
				if x := items[it.ItemID]; x != nil {
					b.remember("identified %s", x.Name)
				}
			}
		}
		b.identIdx = -1
	}
}

// ---------------------------------------------------------------- vendors identify gear for the shop (batch 18)
// 07:15 sample: 59 of 74 online vendors held unidentified gear (161 items) and 2 had a Magnifier, so that gear could
// never be listed; and the field bots' spare piles (lv30+: median 2, p90 10, max 19 unidentified weapons, mostly Club
// 70 weight each) are unidentified too, so parcel.go mails them to a vendor, which identifies them here.

// vendIdentifyWorth: worth a 40z Magnifier to list it (Club 120z yes, Knife 50z no)
func vendIdentifyWorth(x *Item) bool {
	return x != nil && (x.Type == "Weapon" || x.Type == "Armor") && x.Loc != 0 && priceOf(x.ID) >= 100 &&
		(sellPrice(x.ID) >= 100 || x.Slots >= 4) // batch 22: cheaper plain gear is junk for the NPC (junkgear.go)
}

// vendUnidentified: unidentified gear in a vendor's bag worth identifying for the shop (b.mu held)
func (b *Bot) vendUnidentified() int {
	n := 0
	for _, it := range b.inv {
		if it.Unidentified && !it.Equipped && it.Count > 0 && vendIdentifyWorth(items[it.ItemID]) {
			n++
		}
	}
	return n
}

// vendMagnifierTrip: a vendor (shop closed, in Prontera) with gear to identify and no Magnifier walks to the south
// gate Tools Merchant (prontera 164,57) and buys one per item (max 15) - at most once per 20 min (b.mu held).
func (b *Bot) vendMagnifierTrip(now time.Time) bool {
	needMag := b.invIndex(itemMagnifier) < 0 && b.vendUnidentified() > 0
	junk := b.junkGearCount() >= 5 // batch 22: junk gear goes to the NPC (the trip sells first: town.go "sell")
	if b.trip != nil || now.Before(b.magNext) || b.Zeny < 300 && !junk || !needMag && !junk {
		return false
	}
	b.magNext = now.Add(20 * time.Minute)
	var shop *Shop
	for _, s := range gearShops(itemMagnifier) {
		if s.Map == "prontera" && (shop == nil || dist(s.X, s.Y, b.X, b.Y) < dist(shop.X, shop.Y, b.X, b.Y)) {
			shop = s
		}
	}
	if shop == nil {
		return false
	}
	b.trip = &Trip{Stage: "go", Town: "prontera", Shop: shop, Started: now, At: now, Why: "magnifier"}
	b.target, b.path, b.route = 0, nil, nil
	econCount("vendor magnifier trip")
	return true
}

// magnifierBuy: the buy list (0x00c6) on a vendor's Magnifier trip (b.mu held)
func (b *Bot) magnifierBuy(pk []byte) {
	if pr := listedPrice(pk, itemMagnifier); pr > 0 {
		if n := min(b.vendUnidentified(), 15, (b.Zeny-200)/pr); n > 0 {
			go b.send(newPkt(0x00c8).u16(0).u16(uint16(n)).u32(itemMagnifier).varlen())
			econCount("vendor magnifiers bought")
			b.event("info", "", fmt.Sprintf("bought %d Magnifiers to identify gear for my shop", n))
		}
	}
	if t := b.trip; t != nil {
		t.Stage, t.At = "done", time.Now()
	}
	b.npcDeal = nil
}
