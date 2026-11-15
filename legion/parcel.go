package main

// parcel.go (economy batch 17): surplus gear goes to a vendor by mail. Weapons and armor never go to an NPC (owner
// rule), so field bots carried every Club and Knife they looted: 06:37 sample of lv30+ bots, spare weapons median 3,
// p90 7, max 15 (Taco_senpai 18 Clubs, 4-18% of the weight limit). Batch 16 stopped the looting of more copies
// (spareGearFull); this moves the copies beyond the first to a vendor: RODEX with item attachments (0x0a08 open ->
// 0x0a04 <index>.W <amount>.W per item -> 0x0a05 result -> 0x0a6e send -> 0x0a03), up to 5 items a mail. The
// receiver is the account's own vendor character (a "mule", as real players have; it collects on its next login via
// 0x09f3/0x09f4), else a vendor character of a guild mate. The vendor puts them in its cart and sells them at the
// south gate like any spare gear (vendable).

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"time"
)

var parcelNext sync.Map // *Bot -> time.Time

// parcelItems: inventory indices of surplus gear: unequipped weapons/armor it won't wear, no cards, +0, every copy
// beyond the first of its item id (the identified copy stays), max 5 (b.mu held). Batch 18: unidentified copies go
// too (07:15 scan of all 936 online field bots: 0 had 2+ identified copies - the piles are unidentified drops), but
// only when worth a vendor's Magnifier (identify.go vendIdentifyWorth: Club yes, Knife no).
func (b *Bot) parcelItems() []int {
	keep := map[int]int{} // item id -> index of the copy that stays
	var cand []int
	for idx, it := range b.inv {
		x := items[it.ItemID]
		if x == nil || it.Count <= 0 || it.Equipped || (x.Type != "Weapon" && x.Type != "Armor") ||
			it.Refine > 0 || it.Cards != [4]int{} || b.keepsGear(x) || it.Unidentified && !vendIdentifyWorth(x) ||
			b.junkGear(it, x) { // run 7: junk goes to the NPC; RODEX charges 100z per attached item
			continue
		}
		if lootStageOf(b.charName(), x.ID) < lfSell && !b.ownUse(x) {
			continue // lootflow.go (run 10): offered to the party / guild / guild stock first
		}
		if priceOf(x.ID) < 4*mailItemFee {
			continue // run 7: 4-slot Clubs / Swords (market ~110-130z) cost the sender 100z each to mail: not worth it
		}
		if k, ok := keep[it.ItemID]; !ok || b.inv[k].Unidentified && !it.Unidentified {
			keep[it.ItemID] = idx
		}
		cand = append(cand, idx)
	}
	sort.Ints(cand)
	var out []int
	for _, idx := range cand {
		if keep[b.inv[idx].ItemID] == idx {
			continue // one spare stays (econSpare offers it on #trade)
		}
		out = append(out, idx)
		if len(out) == 5 {
			break
		}
	}
	return out
}

// parcelHeal (run 8, owner: healing items first in the market): surplus healing food / herbs past keepFood, as
// index -> count, for the guild's open shop - only stacks worth a 100z stamp four times over (b.mu held). Yellow /
// White Herbs go to the guild alchemist through the market instead (alchemySpare) while it brews.
func (b *Bot) parcelHeal() map[int]int {
	out := map[int]int{}
	for idx, it := range b.inv {
		if it.Count <= keepFood || it.Equipped || foodHeal[it.ItemID] == 0 || it.ItemID == itemYellowHerb {
			continue
		}
		n := it.Count - keepFood
		if n*priceOf(it.ItemID) < 4*mailItemFee {
			continue
		}
		out[idx] = n
		if len(out) == 3 {
			break
		}
	}
	return out
}

var brewerSeen sync.Map // guild alchemist char name -> time.Time it was last seen online with the guide

// parcelHerbs (run 8): the guild alchemist bought the Potion Creation Guide and then had ~1.5k zeny - too little to
// bid for herbs (alchemyWant needs 5+ affordable), so nothing was brewed. Guild mates mail it their brewing herbs
// (Yellow / White / Blue Herb) and Scell (Blue Potions), as real guildies feed their brewer; the potions come back
// as gifts and shop stock. Stacks of 3+, the brewer online with its guide in the last 2 min. b.mu held.
func (b *Bot) parcelHerbs(guild string, now time.Time) bool {
	ga := guildAlchemistOf(guild)
	if ga == nil || ga.Char == "" || strings.EqualFold(ga.Char, b.charName()) {
		return false
	}
	if t, ok := brewerSeen.Load(ga.Char); !ok || now.Sub(t.(time.Time)) > 2*time.Minute {
		return false
	}
	var idx []int
	for i, it := range b.inv {
		switch it.ItemID {
		case itemYellowHerb, itemWhiteHerb, itemBlueHerb, itemScell:
			if it.Count >= 3 && !it.Equipped {
				idx = append(idx, i)
			}
		}
	}
	if len(idx) == 0 {
		return false
	}
	sort.Ints(idx)
	if len(idx) > 4 {
		idx = idx[:4]
	}
	to := ga.Char
	n := len(idx)
	m := &mailOut{To: to, Title: "herbs for brewing", Body: "for the guild potions", Items: idx, stage: 1, at: now,
		onDone: func(ok bool, note string) {
			if ok {
				econCount("herb parcels to the brewer")
				b.event("market", to, fmt.Sprintf("mailed %d herb stacks to %s for brewing", n, to))
			}
		}}
	mailOuts.Store(b, m)
	go b.send(newPkt(0x0a08).str(to, 24).b)
	return true
}

// parcelTarget: a guild mate's vendor with an open shop in Prontera (stable per sender), "" when none
func (b *Bot) parcelTarget(guild string) string {
	me := b.charName()
	// run 7: only a guild mate's vendor whose shop is open in Prontera now. The account's own vendor character is
	// offline while this one plays, and the hashed guild vendor was often an account that never logs in (the first
	// live parcels, 15:24, went to Mochi Deals and Darkrose Trade: both accounts offline, last login NULL / 04:05).
	if guild == "" {
		return ""
	}
	var open []string
	econ.mu.Lock()
	for _, sh := range econ.shops {
		if sh.vendor != b && sh.vendor.P.Guild == guild && sh.mapN == "prontera" {
			open = append(open, sh.name)
		}
	}
	econ.mu.Unlock()
	if len(open) == 0 {
		return ""
	}
	if t := guildTrader(guild); t != "" { // guildstock.go: the guild's trader collects the surplus first
		for _, n := range open {
			if n == t {
				return t
			}
		}
	}
	sort.Strings(open)
	h := fnv.New32a()
	h.Write([]byte(me))
	return open[int(h.Sum32())%len(open)]
}

// parcelThink: from the economy round (b.mu held). At most one parcel per 30 min, only when nothing else is going on
// (a mail being written blocks potion use and trades, mail.go).
func (b *Bot) parcelThink(guild string, now time.Time) {
	if b.isBrewer() {
		brewerSeen.Store(b.charName(), now) // herbs are mailed to an online brewer (parcelHerbs)
	}
	if tn := tune(); !tn.ParcelsEnabled && !nameSet(tn.ParcelTestBots)[strings.ToLower(b.charName())] {
		return // off until tested live (tuning.json parcels_enabled / parcel_test_bots)
	}
	if b.charRole() == "vendor" || b.Dead || b.mkt != nil || b.trip != nil || b.kafra != nil || b.tradeFrom != "" ||
		b.target != 0 || b.underAttack(now) || b.mailBusy() || !b.invReady() {
		return
	}
	if t, ok := parcelNext.Load(b); ok && now.Before(t.(time.Time)) {
		return
	}
	parcelNext.Store(b, now.Add(30*time.Minute))
	if b.parcelHerbs(guild, now) {
		return // one mail at a time: the gear / healing parcel waits for the next round
	}
	idx := b.parcelItems()
	heal := b.parcelHeal()
	for i := range heal {
		idx = append(idx, i)
	}
	// run 7: the fee is now 100z per item (was 2,500z), so one piece worth 250z+ to the NPC is worth a mail too
	if len(idx) == 0 || len(idx) == 1 && len(heal) == 0 && sellPrice(b.inv[idx[0]].ItemID) < 250 {
		return
	}
	to := b.parcelTarget(guild)
	if to == "" {
		return
	}
	names := ""
	for i, j := range idx {
		if i > 0 {
			names += ", "
		}
		names += itemName(b.inv[j].ItemID)
	}
	m := &mailOut{To: to, Title: "gear for the shop", Body: "spare gear, sell it at the gate", Items: idx, Counts: heal, stage: 1, at: now,
		onDone: func(ok bool, note string) {
			if ok {
				b.event("market", to, fmt.Sprintf("mailed %d spare items (%s) to %s for the shop", len(idx), names, to))
			}
		}}
	mailOuts.Store(b, m)
	go b.send(newPkt(0x0a08).str(to, 24).b)
}
