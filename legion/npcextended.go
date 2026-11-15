package main

// npcextended.go (economy run 11). Owner decision 2026-11-14 (RULES.md 2 "Stock NPC shops stay as the server ships
// them"): the official Renewal "Extended" Tool Dealers (Dealer_Update.txt: prt_in 126,76, morocc, payon_in01 5,49,
// yuno 218,97, alberta_in ...) sell White Potion 1,200z, Yggdrasil Leaf 12,000z and Blue Gemstone 450z. The engine
// already loads those shops (gearShops, priceOf caps at the NPC price), but nothing bought gemstones or leaves, so
// Magnus Exorcismus / Safety Wall / Warp Portal only fired with dropped gems.

const (
	// itemBlueGem (717) is declared in weaponskill.go
	itemYggLeaf = 610

	skSafetyWall = 12
	skWarpPortal = 27
	skMagnus     = 79
	skResurrect  = 54
)

// gemTarget: Blue Gemstones this character keeps for its skills (b.mu held)
func (b *Bot) gemTarget() int {
	n := 0
	if b.skillLv(skMagnus) > 0 {
		n = 15 // 4+ undead in a crowd, one gem per cast
	}
	if b.skillLv(skSafetyWall) > 0 {
		n = max(n, 8)
	}
	if b.skillLv(skWarpPortal) > 0 {
		n = max(n, 5)
	}
	for _, sk := range []int{285, 286, 287} { // Sage Volcano / Deluge / Whirlwind
		if b.skillLv(sk) > 0 {
			n = max(n, 6)
		}
	}
	return n
}

// gemsWanted: how many Blue Gemstones to buy from this list within budget and the weight cap (count, price)
func (b *Bot) gemsWanted(pk []byte, budget int) (int, int) {
	pr := listedPrice(pk, itemBlueGem)
	t := b.gemTarget()
	if pr <= 0 || t == 0 || budget < pr {
		return 0, pr
	}
	need := t - b.countItem(itemBlueGem)
	if need < max(2, t/3) {
		return 0, pr // top up only when a third is used
	}
	n := min(need, budget/pr)
	if b.MaxWeight > 0 {
		n = min(n, max(0, (b.MaxWeight*45/100-b.Weight)/30)) // 3.0 weight each
	}
	return max(n, 0), pr
}

// yggWanted: one Yggdrasil Leaf (resurrect a party mate) for a lv60+ party member with money to spare, when the party
// has no Resurrection of its own (b.mu held)
func (b *Bot) yggWanted(budget, price int) bool {
	if b.BaseLvOr() < 60 || b.countItem(itemYggLeaf) > 0 || budget < price+b.gearReserve()+20000 || b.skillLv(skResurrect) > 0 {
		return false
	}
	return b.pin != nil || b.Goal.Mode == "party"
}
