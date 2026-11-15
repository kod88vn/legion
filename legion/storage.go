package main

// Kafra storage (owner 2026-11-14: "50% weight triggers a town run at once"; QA 13:25-13:30: 21 of 27 bots at 50%+
// stayed heavy - their bags held only things they keep for players (cards, ores, spare gear), so "bag is heavy" never
// fired (it needed NPC-sellable loot) and a town visit sold nothing). At >= 50% weight with such items, a bot goes to
// town and puts them in its Kafra storage: Kafra menu "Use Storage" (needs Basic Skill 6, a small fee), then
// CZ_MOVE_ITEM_FROM_BODY_TO_STORE (0x0364 index.W amount.L, rAthena PACKETVER > 20180307) per item, and
// CZ_CLOSE_STORE (0x0193).

import (
	"fmt"
	"time"
)

// storeCandidates (b.mu held): inventory indexes worth putting in storage - kept items (not NPC junk), not equipped,
// not consumables / ammo the bot uses.
func (b *Bot) storeCandidates() []int {
	var out []int
	for idx, it := range b.inv {
		if it == nil || it.Equipped || it.Count <= 0 || b.npcSellable(it) {
			continue
		}
		x := items[it.ItemID]
		if x == nil {
			continue
		}
		switch x.Type {
		case "Etc", "Card", "Weapon", "Armor", "ShadowGear":
		default:
			continue // potions, wings, food, ammo, gems in use stay in the bag
		}
		if it.ItemID == itemButterflyWing || it.ItemID == itemFlyWing {
			continue
		}
		out = append(out, idx)
	}
	return out
}

// canStoreIn: a store visit to this town can actually store - a Kafra here, base level 6 or more, 200z or more and kept
// items to store (the same conditions as storeNeed and storeThink, which stores in a Kafra town).
func (b *Bot) canStoreIn(town string) bool {
	return len(kafraSpots[town]) > 0 && b.basicLv() >= 6 && b.Zeny >= 200 && len(b.storeCandidates()) > 0
}

// storeNeed (b.mu held): over the regen line with nothing an NPC buys but things to store.
func (b *Bot) storeNeed() bool {
	if len(kafraSpots[b.MapName]) > 0 || b.kafra != nil && b.kafra.Store {
		return false // in a Kafra town already (or storing now): storeThink does it here, no trip (QA 14:00 Frostqueen45:
		// a "bag is heavy (store)" trip started in Payon replaced the storage visit, again and again)
	}
	if len(kafraSpots[nearestTown(b.MapName)]) == 0 {
		return false // the trip's town has no Kafra we know: a store trip would only loop
	}
	return b.MaxWeight > 0 && b.overRegenWeight() && !b.hasLoot() && b.basicLv() >= 6 && b.Zeny >= 200 &&
		len(b.storeCandidates()) > 0
}

// storeThink (b.mu held, in town): heavy with things to store and a Kafra here -> walk to it and store them.
func (b *Bot) storeThink(gm *GameMap, moving bool, now time.Time) bool {
	if b.kafra != nil {
		if b.kafra.Store {
			return b.kafraThink(gm, moving, now)
		}
		return false
	}
	spots := kafraSpots[b.MapName]
	if len(spots) == 0 || !b.overRegenWeight() || b.basicLv() < 6 || b.Zeny < 200 || b.underAttack(now) ||
		now.Before(b.buffAt["storeNext"]) || len(b.storeCandidates()) == 0 {
		return false
	}
	b.buffAt["storeNext"] = now.Add(3 * time.Minute)
	sp := spots[0]
	for _, s := range spots[1:] {
		if dist(b.X, b.Y, s.X, s.Y) < dist(b.X, b.Y, sp.X, sp.Y) {
			sp = s
		}
	}
	b.kafra = &kafraTrip{Store: true, Spot: sp, Town: b.MapName, Stage: "walk", Started: now, At: now}
	b.event("info", "", fmt.Sprintf("bag at %d%%: storing kept items at the Kafra in %s", b.Weight*100/b.MaxWeight, b.MapName))
	return b.kafraThink(gm, moving, now)
}

// onStorageOpen (b.mu held, 0x00f2): the storage window opened - move the candidates in and close it.
func (b *Bot) onStorageOpen() {
	if b.resetOnStorage() { // reset.go: everything in for the Hypnotist, or everything back out
		return
	}
	k := b.kafra
	if k == nil || !k.Store || k.Stored {
		go b.send(newPkt(0x0193).b) // a storage we didn't ask for: close it (an open storage blocks walking)
		return
	}
	k.Stored = true
	noteGuildStored(b, b.storeCandidates()) // guildstore.go: a leader's / treasurer's storage holds guild stock (GD05)
	n := 0
	for _, idx := range b.storeCandidates() {
		it := b.inv[idx]
		cnt := it.Count
		go b.send(newPkt(0x0364).u16(uint16(idx)).u32(uint32(cnt)).b)
		n++
	}
	go func() {
		time.Sleep(1500 * time.Millisecond)
		b.send(newPkt(0x0193).b)
	}()
	b.kafra = nil
	b.lastTripOverLine = b.overRegenWeight() // the store step is the last one of the trip: did it clear the bag?
	b.event("info", "", fmt.Sprintf("stored %d kinds of items in Kafra storage (%s)", n, k.Town))
}
