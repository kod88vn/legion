package main

// junkgear.go (economy batch 22). Owner 2026-11-14 (RULES.md section 2): plain weapons/armor worth under 100z with no
// cards or refines (Cotton Shirt, Knife, Club, Sword...) are junk: they go to the NPC on town runs, unidentified ones
// too (piles of unidentified Clubs kept bots over the 45% potion cap: "out of potions" forever). Slotted [4] pieces,
// carded or refined gear, anything worth 100z+ (NPC sell price) and gear the bot will wear stay for players.

// junkGear: this inventory item is junk gear for the NPC (b.mu held)
func (b *Bot) junkGear(it *InvItem, x *Item) bool {
	if it == nil || x == nil || it.Count <= 0 || it.Equipped || (x.Type != "Weapon" && x.Type != "Armor") {
		return false
	}
	if sellPrice(x.ID) >= 100 || x.Buy <= 0 || it.Cards != [4]int{} || it.Refine > 0 {
		return false
	}
	// run 10 (life mind: plain Club [4] listed at 170-200z in 4 south-gate shops, nobody asking): a cheap [4] piece is
	// kept for players only while somebody asks for it on the market; otherwise it is junk like any plain piece
	if x.Slots >= 4 && marketDemand(x.ID) > 0 {
		return false
	}
	if b.keepsGear(x) && (!it.Unidentified || b.firstCopy(it)) {
		// an upgrade or a plan item it will wear soon. Run 10 (QA gear audit: Knife [4] 581, Club [4] 317, Sword [4]
		// 242 unidentified copies carried by online bots, ~158 weight each): an unidentified piece the build wants is
		// kept once (a Magnifier identifies it), every other unidentified copy is judged by its base value
		return false
	}
	return true
}

// firstCopy: no other unequipped copy of the item sits at a lower inventory index (b.mu held)
func (b *Bot) firstCopy(it *InvItem) bool {
	for _, o := range b.inv {
		if o != it && o.ItemID == it.ItemID && !o.Equipped && o.Count > 0 && o.Index < it.Index {
			return false
		}
	}
	return true
}

// npcSellable: junk loot (npcJunk) or junk gear
func (b *Bot) npcSellable(it *InvItem) bool {
	x := items[it.ItemID]
	// economy run 8: a brewer's Medicine Bowls, bottles and Scell are Etc worth < 1,500z ("junk") - both guild
	// alchemists bought 20 bowls with the guide and sold them to the Goods Merchant on the very next stop, before a
	// single brew. Scell is kept by everyone (the guild brewer's Blue Potions; parcel.go parcelHerbs).
	if x != nil && (it.ItemID == itemScell || b.canBrew() && brewMaterial(it.ItemID)) {
		return false
	}
	return x != nil && it.Count > 0 && (npcJunk(x) || b.junkGear(it, x))
}

// junkGearCount: pieces of junk gear in the bag (b.mu held)
func (b *Bot) junkGearCount() int {
	n := 0
	for _, it := range b.inv {
		if b.junkGear(it, items[it.ItemID]) {
			n++
		}
	}
	return n
}
