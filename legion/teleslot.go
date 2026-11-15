package main

// teleslot.go: the Teleport card needs a slotted accessory (build-and-strategy run 6; owner rule RULES.md 3b "every
// bot should aim to have [the Creamy] card").
//
// QA 13:50: 7 Creamy Cards sat uncompounded in bags (one bought for 12k), 0 Clips (2607) on the whole server, 0
// accessories with any card. The gear paths named Clip [1], which no loaded NPC shop sells and only a lone Dragon Fly
// (moc_fild18) or 0.05% Alarm drops drop - unobtainable. The realistic slotted accessory here is the Belt [1] (2627,
// NPC 20,000z, base level 25, every job) at Prontera's south gate (and Al De Baran, Amatsu, Ayothaya, Moscovia ...).
//   - buildgear.json / the extras routes: Belt [1] is the accessory step (Clip kept only where already owned);
//   - a bot holding a Creamy Card without a free-slot accessory buys a Belt [1] first (nextGearBudget bonus) and saves
//     for it (gearHold) - the card is worth nothing in the bag;
//   - creamyWant asks #trade for the card only when a free-slot accessory is owned, or the purse covers both;
//   - cardThink compounds the Creamy into an unworn slotted accessory too (cards.go spareSlotFor), then wears it.

const (
	itemBelt1   = 2627 // Belt [1]: NPC 20,000z, lv25, all jobs
	beltLevel   = 25
	accLocMask  = 0x88 // left / right accessory
	teleAccGain = 800  // nextGearBudget bonus for a slotted accessory while a Creamy waits in the bag
)

// freeSlotAccessory (b.mu held): an accessory with a free card slot that this character owns (worn or in the bag).
func (b *Bot) freeSlotAccessory() *InvItem {
	for _, it := range b.inv {
		x := items[it.ItemID]
		if x == nil || it.Count <= 0 || x.Loc&accLocMask == 0 || !freeCardSlot(it) || it.Unidentified || it.NoEquip {
			continue
		}
		if it.Equipped || b.canEquip(x) {
			return it
		}
	}
	return nil
}

// creamyInBag (b.mu held): an uncompounded Creamy Card in the bag.
func (b *Bot) creamyInBag() bool { return b.countItem(itemCreamyCard) > 0 }

// creamyNeedsSlot (b.mu held): the Creamy waits in the bag and no free-slot accessory is owned.
func (b *Bot) creamyNeedsSlot() bool {
	return b.invReady() && b.creamyInBag() && b.freeSlotAccessory() == nil
}

// teleSlotSaving (b.mu held): saving for the Belt [1] the Creamy needs (gearHold) - from base level 25 (Belt's level).
func (b *Bot) teleSlotSaving() bool {
	if !b.creamyNeedsSlot() || b.BaseLvOr() < beltLevel || b.gearFailed[itemBelt1] {
		return false
	}
	x := items[itemBelt1]
	return x != nil && x.Buy > 0 && b.Zeny-b.gearReserve() < x.Buy
}

// creamyReady (b.mu held): buying the Creamy now makes sense - a free-slot accessory is owned, or the purse covers the
// card's bid plus a Belt [1].
func (b *Bot) creamyReady(bid int) bool {
	if b.freeSlotAccessory() != nil {
		return true
	}
	belt := 20000
	if x := items[itemBelt1]; x != nil && x.Buy > 0 {
		belt = x.Buy
	}
	return b.BaseLvOr() >= beltLevel && b.Zeny-b.gearReserve() >= bid+belt
}

// hasCardIn: the item carries this card.
func hasCardIn(it *InvItem, card int) bool {
	if it == nil {
		return false
	}
	for _, c := range it.Cards {
		if int(c) == card {
			return true
		}
	}
	return false
}
