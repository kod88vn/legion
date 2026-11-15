package main

import (
	"fmt"
	"time"
)

// regendrop.go: a bot over the 50% regen line in town with no potion, nothing that heals it, and no way to lower its
// weight by selling, storing or buying (QA 2026-11-15 P6: Blacksmith and Wizard idled in town at 48-61% HP).
// Paths (owner rules: economy.md EC37 - low value per weight is dropped, high value kept to sell):
//   sell   - a potion shop here buys the loot: the trip logic sells it (town.go needTrip "selling in town");
//   store  - 200+ zeny and storable items: the Kafra takes them (storage.go storeNeed / storeThink);
//   buy    - 500+ zeny: the trip logic buys potions ("out of potions");
//   drop   - none of the above, and only junk (what an NPC buys and no protected kind): the lowest sell value per
//            weight stack is dropped, but only when dropping junk alone brings the bag under the line. Capped per visit.
// Dropped stacks are confirmed from the server's inventory list; a refused drop is remembered and never retried; the
// bot never picks up again what it dropped itself. Behind tuning.json town_drop_junk (default on, EC37).

const (
	regenDropCap     = 6               // stacks per town visit
	regenDropWait    = 5 * time.Second // the inventory list confirms (or refuses) a drop within this
	regenDropGap     = 2 * time.Second
	regenOwnRadius   = 3                // a dropped stack lands within this many cells of the drop
	regenOwnLifetime = 30 * time.Minute // the bot never picks up its own drop for this long
)

// regenDropState: the drop bookkeeping of one bot (b.mu held).
type regenDropState struct {
	pend    *regenPend
	refused map[string]bool // item key (index:id) the server refused: never retried
	visit   int             // stacks confirmed dropped on this town visit
	own     []regenOwnDrop  // what the bot dropped itself
}

type regenPend struct {
	idx, n, itemID int
	x, y           int
	at             time.Time
}

type regenOwnDrop struct {
	itemID int
	x, y   int
	at     time.Time
}

func regenKey(it *InvItem) string { return fmt.Sprintf("%d:%d", it.Index, it.ItemID) }

// regenLastResort (b.mu held): in town, over the regen line, no potion, and none of sell / store / buy can clear the
// weight here: no potion shop here buys the loot, under 500 zeny (no potions bought), and not storable on 200+ zeny.
func (b *Bot) regenLastResort() bool {
	if !isTown(b.MapName) || b.MaxWeight <= 0 || !b.overRegenWeight() || b.potionCount() != 0 {
		return false
	}
	if b.Zeny >= 500 {
		return false // buy path
	}
	if b.hasLoot() && potionShop(townOf(b.MapName)) != nil {
		return false // sell path
	}
	if b.Zeny >= 200 && len(b.storeCandidates()) > 0 {
		return false // store path
	}
	return true
}

// regenProtected (b.mu held): kinds the drop never touches, whatever their value per weight.
func (b *Bot) regenProtected(it *InvItem, x *Item) bool {
	id := it.ItemID
	switch {
	case it.Equipped || it.Count <= 0:
		return true
	case isHealPotion(id) || foodHeal[id] > 0 || healItem(id): // what the bot heals with
		return true
	case x.Type == "Card" || x.Type == "Ammo" || isArrow(id): // cards are worth keeping, ammo is the bow's
		return true
	case x.Buy <= 0 || sellPrice(id) <= 0: // quest and unsellable items
		return true
	case valuableEtc[id]: // gems, valuable materials
		return true
	case marketDemand(id) > 0: // a market buyer wants it
		return true
	case b.keepsGear(x) || b.keepsCard(id): // the gear path and card slots
		return true
	}
	if w := b.econWant(); w != nil && w.ItemID == id { // a want the bot is working on (economy)
		return true
	}
	return false
}

// regenJunk (b.mu held): the droppable bag stacks, lowest sell value per weight first. Junk only: what an NPC buys
// (npcSellable = npcJunk or junkGear) and that no protected kind covers.
func (b *Bot) regenJunk() []*InvItem {
	var out []*InvItem
	for _, it := range b.inv {
		if it == nil {
			continue
		}
		x := items[it.ItemID]
		if x == nil || b.regenProtected(it, x) || !b.npcSellable(it) || b.regenRefused(it) {
			continue
		}
		out = append(out, it)
	}
	ratio := func(it *InvItem) float64 {
		w := 1
		if x := items[it.ItemID]; x != nil && x.Weight > 0 {
			w = x.Weight
		}
		return float64(sellPrice(it.ItemID)) / float64(w)
	}
	for i := 1; i < len(out); i++ { // insertion sort: the bag is small
		for j := i; j > 0 && ratio(out[j]) < ratio(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func (b *Bot) regenRefused(it *InvItem) bool { return b.rdrop.refused[regenKey(it)] }

// regenDropPlan (b.mu held): the junk stacks to drop, lowest value per weight first, only when they bring the bag under
// the line. Empty when junk alone cannot do it (nothing is dropped then; the party or guild or a store trip must).
func (b *Bot) regenDropPlan() []*InvItem {
	need := b.Weight - b.MaxWeight*(tune().WeightRegenPct-1)/100 // bring the weight to below the regen line
	if need <= 0 {
		return nil
	}
	var plan []*InvItem
	got := 0
	for _, it := range b.regenJunk() {
		x := items[it.ItemID]
		plan = append(plan, it)
		got += x.Weight * it.Count
		if got >= need {
			return plan
		}
	}
	return nil // junk alone is not enough: drop nothing
}

// regenDrop (b.mu held, lowHPGuard): the drop path. True while it owns the tick (a drop is in flight, or it dropped).
func (b *Bot) regenDrop(now time.Time) bool {
	st := &b.rdrop
	if p := st.pend; p != nil {
		if now.Sub(p.at) < regenDropWait {
			b.setState(fmt.Sprintf("low HP (%d%%), over the regen weight: dropping junk", b.hpPct()))
			return true
		}
		st.pend = nil
		it := b.inv[p.idx]
		if it != nil && it.ItemID == p.itemID && it.Count >= p.n {
			if st.refused == nil {
				st.refused = map[string]bool{}
			}
			st.refused[fmt.Sprintf("%d:%d", p.idx, p.itemID)] = true // the server kept it: never again
		} else {
			st.visit++
			st.own = append(st.own, regenOwnDrop{itemID: p.itemID, x: p.x, y: p.y, at: now})
		}
	}
	if !tune().TownDropJunk || !b.regenLastResort() || st.visit >= regenDropCap {
		return false
	}
	if now.Before(b.potAt) {
		return false
	}
	plan := b.regenDropPlan()
	if len(plan) == 0 {
		return false // junk cannot clear the weight: say nothing, the party or a store trip has to
	}
	it := plan[0]
	b.potAt = now.Add(regenDropGap)
	st.pend = &regenPend{idx: it.Index, n: it.Count, itemID: it.ItemID, x: b.X, y: b.Y, at: now}
	b.setState(fmt.Sprintf("low HP (%d%%), over the regen weight: dropping junk (item %d)", b.hpPct(), it.ItemID))
	go b.dropItem(it.Index, it.Count)
	return true
}

// regenOwnDrop (b.mu held): true for a ground stack the bot dropped itself (never picked up again).
func (b *Bot) regenOwnDrop(g *GroundItem, now time.Time) bool {
	for _, o := range b.rdrop.own {
		if o.itemID == g.ItemID && now.Sub(o.at) < regenOwnLifetime && dist(o.x, o.y, g.X, g.Y) <= regenOwnRadius {
			return true
		}
	}
	return false
}

// dropItem: CZ_ITEM_THROW2 0x0363 <index>.W <amount>.W (rathena clif_packetdb.hpp: parseable_packet(0x0363,6,
// clif_parse_DropItem,2,4)); the index is the one the engine uses for every item action (useItem).
func (b *Bot) dropItem(idx, n int) { b.send(newPkt(0x0363).u16(uint16(idx)).u16(uint16(n)).b) }

// hpPct: HP in whole percent (state labels).
func (b *Bot) hpPct() int { return int(b.hpFrac() * 100) }
