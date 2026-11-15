package main

// Food and surplus consumables (QA 2026-11-14 05:48): party archers/acolytes sat at 71-80% weight for 20+ min.
// wolfclaw70 (Archer lv38, 77%) carried Novice Potion 162, Carrot 76, Apple 49, Fly Wing 43, Green Herb 43;
// Ghostfang (Acolyte lv37, 80%) Green Herb 101, Novice Potion 121, Fly Wing 24. Fruit and herbs are neither npcJunk
// nor potions, so they were never eaten or sold; Fly Wings (5.0 weight each) piled up from drops. Now: fruit/herbs
// (and starter Novice Potions past a cap at lv30+) are eaten as out-of-combat HP top-ups, ~10 Green Herbs are kept
// for poison, and Green Herbs / Fly Wings / food past their caps are sold as loot on the next shop visit.

const (
	keepGreenHerbs  = 10 // poison cures (poison.go)
	keepFlyWings    = 15 // escapes (danger.go); the shop tops up to 5
	keepFood        = 20 // more fruit than this is sold, not hauled
	keepNovicePots  = 30 // lv30+: Novice Potions (~55 HP) past this are drunk out of combat
	healKeepForShop = 80 // run 8: healing food past keepFood (up to this many more) is kept for the guild's shop
)

// foodHeal: HP-healing fruit and herbs (rAthena item db: Red/Yellow Herb, Apple, Banana, Carrot, Sweet Potato, Meat, Milk)
var foodHeal = map[int]int{507: 23, 508: 48, 512: 19, 513: 19, 515: 19, 516: 19, 517: 85, 519: 32}

// foodIndex (b.mu held): what to eat for an out-of-combat top-up: the weakest food first, then a surplus Novice
// Potion (lv30+). Never a bought potion: those are for fights.
func (b *Bot) foodIndex() int {
	missing := b.MaxHP - b.HP
	best, bh := -1, 1<<30
	for _, it := range b.inv {
		if h := foodHeal[it.ItemID]; h > 0 && it.Count > 0 && h <= missing && h < bh {
			best, bh = it.Index, h
		}
	}
	if best >= 0 {
		return best
	}
	if b.BaseLvOr() >= 30 && missing >= 55 {
		for _, it := range b.inv {
			if it.ItemID == 569 && it.Count > keepNovicePots {
				return it.Index
			}
		}
	}
	return -1
}

// surplusConsumables (b.mu held): inventory index -> count to sell: Green Herbs, Fly Wings and food past their caps
func (b *Bot) surplusConsumables() map[int]int {
	out := map[int]int{}
	keep := map[int]int{itemGreenHerb: keepGreenHerbs, itemFlyWing: keepFlyWings}
	for id := range foodHeal {
		keep[id] = keepFood
	}
	for _, it := range b.inv {
		k, ok := keep[it.ItemID]
		if !ok || it.Equipped || it.Count <= k {
			continue
		}
		if x := items[it.ItemID]; x == nil || x.Buy <= 0 {
			continue // nothing an NPC pays for
		}
		// run 8 (owner: healing items first in the market): surplus healing food waits for a parcel to the guild's
		// shop (parcel.go parcelHeal) instead of the NPC - unless the bag is over the regen line or the pile is huge
		if healItem(it.ItemID) && !b.overRegenWeight() && it.Count <= k+healKeepForShop {
			continue
		}
		out[it.Index] = it.Count - k
	}
	return out
}
