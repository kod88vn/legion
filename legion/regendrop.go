package main

import (
	"fmt"
	"time"
)

// regendrop.go: a bot over the 50% regen line in town with nothing that lowers its weight and nothing that heals it
// (QA 2026-11-15 P6: Blacksmith and Wizard idled in town at 48-61% HP). The order of the paths, as the owner's rules
// (economy.md EC37: low value per weight is dropped, high value kept to sell) allow:
//   sell   - loot to sell: the trip logic sells it (town.go needTrip "selling in town");
//   store  - things to keep stored: the Kafra takes them (storage.go storeNeed / storeThink);
//   buy    - potions the bot can pay for: the trip logic buys them (town.go needTrip "out of potions");
//   drop   - none of the above: the lowest value-per-weight item not equipped and not a potion or food is dropped,
//            one stack per 2 s, until the bag is back under the line.
// regenLastResort is true only for the drop case; the other three are the trip logic's (lowHPGuard releases the tick).

// regenLastResort (b.mu held): in town, over the regen line, no potion, nothing to sell, too poor to buy potions and
// nothing to store.
func (b *Bot) regenLastResort() bool {
	return isTown(b.MapName) && b.MaxWeight > 0 && b.overRegenWeight() && b.potionCount() == 0 && !b.hasLoot() &&
		b.Zeny < 500 && (b.Zeny < 200 || len(b.storeCandidates()) == 0)
}

// regenDropPick (b.mu held): the bag item with the lowest sell value per weight unit (EC37). Never equipped, potions or
// food: those are what the bot heals with.
func (b *Bot) regenDropPick() *InvItem {
	var best *InvItem
	bestRatio := 0.0
	for _, it := range b.inv {
		if it == nil || it.Equipped || it.Count <= 0 || isHealPotion(it.ItemID) || foodHeal[it.ItemID] > 0 {
			continue
		}
		w := 1
		if x := items[it.ItemID]; x != nil && x.Weight > 0 {
			w = x.Weight
		}
		r := float64(sellPrice(it.ItemID)) / float64(w)
		if best == nil || r < bestRatio || (r == bestRatio && it.Index < best.Index) {
			best, bestRatio = it, r
		}
	}
	return best
}

// regenDrop (b.mu held, lowHPGuard): the drop path. True while it owns the tick (dropping, or waiting for the next drop).
func (b *Bot) regenDrop(now time.Time) bool {
	if !b.regenLastResort() {
		return false
	}
	it := b.regenDropPick()
	if it == nil {
		return false // nothing droppable: the bot can only wait for regen, which never comes (state says why)
	}
	b.setState(fmt.Sprintf("low HP (%d%%), over the regen weight: dropping %d x item %d", b.hpPct(), it.Count, it.ItemID))
	if now.Before(b.potAt) {
		return true
	}
	b.potAt = now.Add(2 * time.Second)
	n := it.Count
	it.Count = 0 // the server removes the stack; the next inventory list confirms it
	go b.dropItem(it.Index, n)
	return true
}

// dropItem: CZ_ITEM_THROW2 0x0363 <index>.W <amount>.W (rathena clif_packetdb.hpp: parseable_packet(0x0363,6,
// clif_parse_DropItem,2,4)); the index is the one the engine uses for every item action (useItem).
func (b *Bot) dropItem(idx, n int) { b.send(newPkt(0x0363).u16(uint16(idx)).u16(uint16(n)).b) }

// hpPct: HP in whole percent (state labels).
func (b *Bot) hpPct() int { return int(b.hpFrac() * 100) }
