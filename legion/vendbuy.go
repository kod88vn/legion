package main

// Buying from vendors: a bot that asks the market for something (economy.go Want) and finds it in an open vending
// shop at Prontera's south gate at or below its bid walks up to the shop and buys it, like a player browsing the
// market (owner: "There must be a real economy", vendors at the south gate). Before this, vendors could only ever
// sell to humans.
//   CZ_REQ_BUY_FROMMC 0x0130 (vendor account id)
//   ZC_PC_PURCHASE_ITEMLIST_FROMMC 0x0b3d (PACKETVER 20210107; was 0x0800): len, AID u32, venderId u32, then 64-byte
//       entries: price u32, amount u16, index i16, type u8, itemId u32, identified u8, damaged u8, cards 4xu32,
//       options 5x(i16,i16,u8), location u32, viewSprite u16, refine u8, grade u8
//   CZ_PC_PURCHASE_ITEMLIST_FROMMC2 0x0801: len, AID u32, venderId u32, then (count i16, index i16)
//   ZC_PC_PURCHASE_RESULT_FROMMC 0x0135 (index, amount, result): only on failure
//   vendor side: ZC_DELETEITEM_FROM_MCSTORE2 0x09e5 (index u16, amount u16, buyer GID u32, date u32, zeny u32)

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type vendListing struct {
	Index, ItemID, Count, Price int
}

var vendSales, vendSalesZeny int64 // items sold by Legion vendors (to bots or humans), shown in /api/economy

type vendBuy struct {
	Vendor           *Bot
	VName            string
	ItemID, Count    int
	MaxEach          int
	Deadline         time.Time
	reqAt, boughtAt  time.Time
	tries            int
	zenyBefore       int
	savedGoal        Goal
	goalSet, sent    bool
	done             bool
	result, lastNote string
}

var vendBuys sync.Map // *Bot -> *vendBuy

// shopCustomer: which bot is buying item from this vendor right now (a vendBuy aimed at it), for the trade history
// (2026-11-14: every shop sale was recorded as "(shop customer)", so trade history and diaries could not be matched).
// Only fields set when the vendBuy is created are read (another bot's lock is not held).
func shopCustomer(vendor string, item int) string {
	who := "(shop customer)"
	vendBuys.Range(func(k, v any) bool {
		vb := v.(*vendBuy)
		if vb.VName == vendor && vb.ItemID == item && time.Now().Before(vb.Deadline) {
			who = k.(*Bot).charName()
			return false
		}
		return true
	})
	return who
}

// vendable: what a vendor puts in its cart: loot, cards, gear it won't wear, and an alchemist's brewed potions
func (b *Bot) vendable(it *InvItem, x *Item) bool {
	if it.Unidentified {
		return false // rAthena refuses the WHOLE shop if one item is unidentified (vending.cpp), batch 10
	}
	switch {
	case x.Type == "Card" || valuableEtc[x.ID]:
		return true
	case healItem(x.ID) && !(b.canBrew() && brewMaterial(x.ID)):
		return true // run 8 (owner): HP restorers are the first thing the market sells
	case b.canBrew() && brewMaterial(x.ID):
		return false // run 7: a brewing vendor's herbs, Scell, bottles and bowls are for its Pharmacy, not its shop
	case x.Type == "Etc":
		return !npcJunk(x) // Jellopy at 3z filled the first shop: worthless loot goes to the NPC on a trip
	case x.Type == "Weapon" || x.Type == "Armor":
		// batch 21 (owner: "do not sell junks, sell useful items like ss cards, katana"): starter gear worth under
		// 100z (Cotton Shirt 10z, Knife 50z: 507 + 747 listed at 09:10, 0 wanted) does not take shop slots
		if b.junkGear(it, x) { // junkgear.go: it goes to the NPC instead
			return false
		}
		return !it.Equipped && !b.keepsGear(x)
	case b.canBrew() && brewedKeep[x.ID] > 0:
		return true
	}
	return false
}

// shopListing: a copy of what this bot's open shop sells (b.mu held), nil when not vending
func (b *Bot) shopListing() []vendListing {
	if b.vend == nil || !b.vend.Opened || !b.Online || b.MapName != "prontera" || shopIsGhost(b) {
		return nil
	}
	var out []vendListing
	for _, l := range b.vend.Items {
		if l.Count > 0 {
			out = append(out, l)
		}
	}
	return out
}

// startVendBuy gives the buyer its shopping task (takes the buyer's lock). False if it became busy.
func startVendBuy(buyer *Bot, name string, vendor *Bot, vname string, itemID, count, maxEach int, now time.Time) bool {
	buyer.mu.Lock()
	defer buyer.mu.Unlock()
	if buyer.charName() != name {
		return false
	}
	if !buyer.beginVendBuy(vendor, vname, itemID, count, maxEach, now, 8*time.Minute, "want") {
		return false
	}
	econCount("shop buy started: want board")
	return true
}

// beginVendBuy: the shopping task itself (b.mu held). src says why (counters).
func (b *Bot) beginVendBuy(vendor *Bot, vname string, itemID, count, maxEach int, now time.Time, life time.Duration, src string) bool {
	if !b.Online || b.Dead || b.trip != nil || b.kafra != nil || b.mkt != nil || b.Zeny < maxEach*count {
		return false
	}
	if v, ok := vendBuys.Load(b); ok && !v.(*vendBuy).done {
		return false
	}
	vb := &vendBuy{Vendor: vendor, VName: vname, ItemID: itemID, Count: count, MaxEach: maxEach,
		Deadline: now.Add(life), zenyBefore: b.Zeny}
	vendBuys.Store(b, vb)
	b.target, b.path = 0, nil
	b.event("info", vname, fmt.Sprintf("going to %s's shop to buy %dx %s (up to %dz each, %s)", vname, count, itemName(itemID), maxEach, src))
	return true
}

func (b *Bot) endVendBuy(vb *vendBuy, why string) {
	if !vb.done {
		r := why
		if i := strings.Index(r, " ("); i > 0 {
			r = r[:i] // "nothing bought (shop said 4)" -> "nothing bought"
		}
		econCount("shop buy: " + r)
	}
	vb.done, vb.result = true, why
	if vb.goalSet && b.Goal.Set == "vendbuy" {
		b.Goal, b.route, b.path = vb.savedGoal, nil, nil
	}
	vb.goalSet = false
}

// vendBuyThink walks to the shop and buys (b.mu held). True while it owns the bot.
func (b *Bot) vendBuyThink(gm *GameMap, moving bool, now time.Time) bool {
	v, ok := vendBuys.Load(b)
	if !ok {
		return false
	}
	vb := v.(*vendBuy)
	if vb.done {
		return false
	}
	if now.After(vb.Deadline) {
		b.endVendBuy(vb, "timed out")
		return false
	}
	if vb.sent {
		// no failure answer (0x0135) within 3 s and the zeny went down: bought
		if now.Sub(vb.boughtAt) > 3*time.Second {
			if b.Zeny < vb.zenyBefore {
				paid := vb.zenyBefore - b.Zeny
				b.event("market", vb.VName, fmt.Sprintf("bought %s from %s's shop for %dz", itemName(vb.ItemID), vb.VName, paid))
				noteMarketFlow("vendor_buy", paid, vb.ItemID, 0) // marketflow.go (run 11)
				noteBought(b, vb.ItemID) // refine.go: not offered again for an hour
				econ.mu.Lock()
				delete(econ.wants, b.charName())
				econ.nextWant[b.charName()] = now.Add(time.Minute)
				econ.mu.Unlock()
				b.endVendBuy(vb, "bought")
			} else {
				b.endVendBuy(vb, "nothing bought "+vb.lastNote)
			}
			return false
		}
		return true
	}
	ps, ok := peek(vb.Vendor.P.Name)
	if !ok || !ps.Online || ps.Name != vb.VName {
		b.endVendBuy(vb, "vendor left")
		return false
	}
	if b.underAttack(now) || b.target != 0 {
		b.hunt(gm, moving, now)
		if b.target != 0 || b.Sitting {
			return true
		}
	}
	if b.Sitting {
		b.wantSit = false
		go b.stand()
		return true
	}
	if b.MapName != ps.Map {
		if !vb.goalSet {
			vb.savedGoal, vb.goalSet = b.Goal, true
		}
		if b.Goal.Map != ps.Map || b.Goal.Set != "vendbuy" {
			b.Goal, b.route, b.path = Goal{Mode: "idle", Map: ps.Map, Set: "vendbuy"}, nil, nil
		}
		b.travel(gm, moving)
		if b.Goal.Map != ps.Map {
			b.endVendBuy(vb, "no route")
			return false
		}
		b.setState("going to " + vb.VName + "'s shop")
		return true
	}
	b.setState("shopping at " + vb.VName + "'s shop")
	if dist(b.X, b.Y, ps.X, ps.Y) > 2 {
		// batch 14: the nearest walkable cell by the vendor (talkCell), not always the one south of it: LilGhostx Shop
		// stood at 136,60 with 136,61 blocked, and 6 of 8 shop buys after the 05:06 deploy timed out on "no path"
		tx, ty := ps.X, ps.Y+1
		if gm != nil {
			if cx, cy, ok := talkCell(gm, b.X, b.Y, ps.X, ps.Y); ok {
				tx, ty = cx, cy
			}
		}
		if b.X != tx || b.Y != ty || dist(b.X, b.Y, ps.X, ps.Y) > 6 {
			if !moving && gm != nil {
				b.stepAlong(gm, tx, ty)
			}
			return true
		}
	}
	if !moving && now.Sub(vb.reqAt) > 5*time.Second {
		if vb.tries >= 3 {
			b.endVendBuy(vb, "shop did not open")
			markShopGhost(vb.Vendor, "buyer got no shop") // shopcheck.go: off the market for everyone
			return false
		}
		vb.tries++
		vb.reqAt = now
		go b.send(newPkt(0x0130).u32(ps.AID).b)
	}
	return true
}

// onVendList: the shop's item list (0x0b3d) or a failed purchase (0x0135), buyer side (b.mu held)
func (b *Bot) onVendList(op uint16, pk []byte) {
	v, ok := vendBuys.Load(b)
	if !ok {
		return
	}
	vb := v.(*vendBuy)
	if vb.done {
		return
	}
	switch op {
	case 0x0135:
		if len(pk) >= 7 {
			vb.lastNote = fmt.Sprintf("(shop said %d)", pk[6])
		}
	case 0x0b3d:
		if vb.sent || len(pk) < 12 {
			return
		}
		aid, vid := le.Uint32(pk[4:]), le.Uint32(pk[8:])
		const entry = 64
		for o := 12; o+entry <= len(pk); o += entry {
			price, amount, idx := int(le.Uint32(pk[o:])), int(le.Uint16(pk[o+4:])), int(int16(le.Uint16(pk[o+6:])))
			id := int(le.Uint32(pk[o+9:]))
			if id != vb.ItemID || price > vb.MaxEach || price <= 0 {
				continue
			}
			n := min(vb.Count, amount, b.Zeny/price)
			if n <= 0 {
				continue
			}
			vb.sent, vb.boughtAt, vb.zenyBefore = true, time.Now(), b.Zeny
			go b.send(newPkt(0x0801).u16(0).u32(aid).u32(vid).u16(uint16(n)).u16(uint16(idx)).varlen())
			return
		}
		b.endVendBuy(vb, "not in the shop at that price any more")
	}
}

// onVendSold: vendor side, something sold (0x09e5 / 0x0137; b.mu held)
func (b *Bot) onVendSold(op uint16, pk []byte) {
	if len(pk) < 6 || b.vend == nil {
		return
	}
	idx, amt := int(le.Uint16(pk[2:])), int(le.Uint16(pk[4:]))
	zeny := 0
	if op == 0x09e5 && len(pk) >= 18 {
		zeny = int(le.Uint32(pk[14:]))
	}
	item, price := 0, 0
	for i := range b.vend.Items {
		l := &b.vend.Items[i]
		if l.Index == idx {
			l.Count -= amt
			item, price = l.ItemID, l.Price
		}
	}
	if it := b.cart[idx]; it != nil {
		it.Count -= amt
		if it.Count <= 0 {
			delete(b.cart, idx)
		}
	}
	if zeny == 0 {
		zeny = price * amt
	}
	b.vend.Sold += amt
	b.vend.SoldZeny += zeny
	atomic.AddInt64(&vendSales, int64(amt))
	atomic.AddInt64(&vendSalesZeny, int64(zeny))
	noteMarketFlow("market_sell", zeny, item, amt) // marketflow.go (run 11)
	b.event("market", "", fmt.Sprintf("sold %dx %s in my shop for %dz", amt, itemName(item), zeny))
	left := 0
	for _, l := range b.vend.Items {
		left += max(l.Count, 0)
	}
	if left == 0 { // sold out: the real client closes the shop itself (CZ_REQ_CLOSESTORE 0x012e); restock / reopen later
		go b.send(newPkt(0x012e).b)
		b.vend.Opened, b.vend.TriedAt = false, time.Time{}
		b.event("market", "", "shop sold out")
	}
	if item != 0 && amt > 0 {
		econ.mu.Lock()
		marketSold(b.P.Name, item) // market.go: the markdown starts over
		econ.trades = append(econ.trades, Trade{T: time.Now(), Seller: b.charName(), Buyer: shopCustomer(b.charName(), item), ItemID: item,
			Item: itemName(item), Count: amt, Price: zeny, Map: b.MapName})
		if len(econ.trades) > 300 {
			econ.trades = econ.trades[len(econ.trades)-300:]
		}
		each := zeny / amt
		st := econ.stats[item]
		if st == nil {
			st = &itemStat{Min: each, Max: each}
			econ.stats[item] = st
		}
		st.N++
		st.Sum += int64(each)
		st.Min, st.Max, st.Last = min(st.Min, each), max(st.Max, each), each
		econ.mu.Unlock()
	}
}

// vendBuying: on its way to buy from a shop (b.mu held)
func (b *Bot) vendBuying() bool {
	if v, ok := vendBuys.Load(b); ok {
		return !v.(*vendBuy).done
	}
	return false
}

// openShops: every open Legion vending shop with its listing (for /api/economy)
func openShops() []map[string]interface{} {
	out := []map[string]interface{}{}
	bots, _ := allBots.Load().([]*Bot)
	for _, b := range bots {
		b.mu.Lock()
		if l := b.shopListing(); l != nil || (b.vend != nil && b.vend.Opened && b.Online && !shopIsGhost(b)) {
			var items []string
			for _, x := range l {
				items = append(items, fmt.Sprintf("%s x%d @%d", itemName(x.ItemID), x.Count, x.Price))
			}
			out = append(out, map[string]interface{}{"vendor": b.charName(), "map": b.MapName, "x": b.X, "y": b.Y,
				"title": b.vend.Title, "items": items, "sold": b.vend.Sold, "sold_zeny": b.vend.SoldZeny,
				"opened": b.vend.OpenedAt})
		}
		b.mu.Unlock()
	}
	return out
}

// ---------------------------------------------------------------- browsing the south gate on a town run (batch 9)

// shopRef: one open vending shop as the economy round saw it (econ.shops)
type shopRef struct {
	vendor     *Bot
	name, mapN string
	x, y       int
	items      []vendListing
}

// shopGearPick (economy batch 13): the gear a Prontera town trip would have bought from an NPC, skipped because a
// south-gate shop sells it for no more; browseShops buys it there at the end of the trip. 04:40 data: 70+ NPC gear
// buys (Adventurer's Suit, Sandals, Hood...) in 4 min while shops listed Sandals at 370 (NPC 400) and Suits at 950
// (NPC 1000), and "shop browse: nothing for me" was 75 vs 5 bought: browsing ran after the NPC had sold the gear.
var shopGearPick sync.Map // *Bot -> item id

// shopPotPick (run 11): the HP potion a Prontera trip skipped at the NPC because a shop sells it cheaper
var shopPotPick sync.Map // *Bot -> item id

// cheaperInShop: an open shop (not a ghost, not our own) lists this item at or below price (b.mu held; takes econ.mu,
// the same order as browseShops).
func (b *Bot) cheaperInShop(id, price int) bool {
	econ.mu.Lock()
	defer econ.mu.Unlock()
	for _, sh := range econ.shops {
		if sh.vendor == b || shopIsGhost(sh.vendor) {
			continue
		}
		for _, l := range sh.items {
			if l.ItemID == id && l.Count > 0 && l.Price > 0 && l.Price <= price {
				return true
			}
		}
	}
	return false
}

// browseShops: a bot whose town trip just ended in Prontera (alone or on its party's town run - the party is in town
// anyway) looks over the open shops at the south gate and buys one thing it would use at a fair price: what it asked
// for on #trade, SP potions, a card for a free slot of its gear, or a gear upgrade. Before this, shop goods reached
// only bots that were "free" (no trip), and party members never walked to a shop: 0 vendor sales all day. b.mu held.
func (b *Bot) browseShops(now time.Time) {
	if townOf(b.MapName) != "prontera" || !b.invReady() || b.charRole() == "vendor" || b.vendBuying() || b.mkt != nil || b.Dead {
		return
	}
	econ.mu.Lock()
	shops := append([]shopRef(nil), econ.shops...)
	var w Want
	if x := econ.wants[b.charName()]; x != nil && !x.InDeal {
		w = *x
	}
	econ.mu.Unlock()
	if len(shops) == 0 {
		return
	}
	budget := b.Zeny - b.gearReserve()
	if budget <= 0 {
		return
	}
	spw := b.spPotionWant(false)
	var best *shopRef
	bestID, bestN, bestPrice, bestScore, bestWhy := 0, 0, 0, 0.0, ""
	for i := range shops {
		sh := &shops[i]
		for _, l := range sh.items {
			x := items[l.ItemID]
			if x == nil || l.Price <= 0 || l.Count <= 0 || l.Price > budget {
				continue
			}
			n, score, why := 0, 0.0, ""
			switch {
			case w.ItemID != 0 && wantAccepts(&w, l.ItemID) && l.Price <= wantCap(b, &w, budget): // pricedyn.go
				n, score, why = min(w.Count, l.Count), 1e6, w.Why
				if !w.Partial && l.Count < w.Count {
					n = 0
				}
			case spw != nil && wantAccepts(spw, l.ItemID) && l.Price <= spw.Each:
				n, score, why = min(spw.Count, l.Count), 5e5, "sp potions"
			case l.ItemID >= itemRedPotion && l.ItemID <= itemWhitePotion && !b.isHealer() && b.realPotCount() < b.hpPotTarget() &&
				l.Price*10 <= x.Buy*11:
				// run 8 (owner: healing items first): HP potions in a player shop at up to 110% of the NPC price (brewed
				// White / Yellow Potions are not NPC stock everywhere); score above gear, below SP potions
				n, score, why = min(b.hpPotTarget()-b.realPotCount(), l.Count), 4e5, "hp potions"
				if pid, ok := shopPotPick.Load(b); ok && pid.(int) == l.ItemID {
					score = 8e5 // run 11: the NPC was skipped for these (cheaper here)
				}
			case (l.ItemID == itemButterflyWing || l.ItemID == itemFlyWing) && x.Buy > 0 && l.Price < x.Buy && b.countItem(l.ItemID) < 3:
				// run 11: wings cheaper than the NPC (owner: Prontera vendors beat the NPC)
				n, score, why = min(3-b.countItem(l.ItemID), l.Count), 3e5, "wings"
			case x.Type == "Card":
				if b.ownsUnequipped(x.ID) || b.cardSlotFor(x.ID) == nil && b.spareSlotFor(x.ID) == nil {
					continue
				}
				v := b.cardValue(x.ID)
				if v < 2 || l.Price > b.wantBid(x.ID)*6/5 {
					continue
				}
				n, score, why = 1, float64(v)*1000/float64(l.Price), "card for my gear"
			case x.Type == "Weapon" || x.Type == "Armor":
				if !b.canEquip(x) || b.gearFailed[x.ID] || b.ownsUnequipped(x.ID) || l.Price > priceOf(x.ID)*6/5 {
					continue
				}
				slot := gearSlot(x.Loc)
				if slot == 0x20 && b.equipped[0x2] != nil && items[b.equipped[0x2].ItemID] != nil && items[b.equipped[0x2].ItemID].Loc == 0x22 {
					continue // shield with a two-handed weapon
				}
				gain := b.itemScore(x) - b.itemScore(b.curIn(slot))
				if gain <= 0 {
					continue
				}
				n, score, why = 1, float64(gain)*1000/float64(l.Price), "upgrade"
				if pid, ok := shopGearPick.Load(b); ok && pid.(int) == x.ID {
					score = 9e5 // the trip skipped the NPC for this one (tripGear): buy it here
				}
			}
			n = min(n, budget/l.Price)
			if n <= 0 || score <= bestScore {
				continue
			}
			best, bestID, bestN, bestPrice, bestScore, bestWhy = sh, l.ItemID, n, l.Price, score, why
		}
	}
	shopGearPick.Delete(b)
	shopPotPick.Delete(b)
	if best == nil {
		econCount("shop browse: nothing for me")
		return
	}
	if b.beginVendBuy(best.vendor, best.name, bestID, bestN, bestPrice, now, 3*time.Minute, "town run: "+bestWhy) {
		econCount("shop buy started: town run")
		typing, item := b.P.Typing, itemName(bestID)
		go func() { // partyOf takes the parties lock: never while holding b.mu
			if partyOf(b) != nil {
				b.partySay(styled(typing, pick("sec, checking a shop", "brb 1 min, buying "+item+" at the market", "wait, "+item+" in a shop here")))
			}
		}()
	}
}

// stockerReady: a vendor that can open a shop but has (almost) nothing to put in it, with money to buy stock. b.mu held.
func (b *Bot) stockerReady() bool {
	if s := b.skills[mcVending]; s == nil || s.Lv == 0 {
		return false
	}
	if b.vend != nil && b.vend.Opened || len(b.cart) >= 3 || b.BaseLvOr() < 15 || b.Zeny < 1500 || b.pin != nil {
		return false
	}
	return true
}

// stockWant: what a stocking vendor buys from field bots: the most valuable card / material / slotted or drop-only
// gear on the offers board that someone sells below 90% of the standard (it resells at 95-130%). b.mu held.
func (b *Bot) stockWant() *Want {
	budget := b.Zeny - 800 // keeps the cart rental and a few potions
	need, listed := stockDemand()
	var best *Want
	bestGap := 0
	for _, o := range boardOffers() {
		x := items[o.ItemID]
		if x == nil {
			continue
		}
		heal := healItem(x.ID)
		if glutted(x.ID) {
			continue // market.go (run 8): no stock for a surplus
		}
		gGap := guildGap(b.P.Guild, x.ID) // guildstock.go: the trader buys what its guild's stock lacks first
		ok := heal || gGap || x.Type == "Card" || valuableEtc[x.ID] || (x.Type == "Weapon" || x.Type == "Armor") && (x.Slots > 0 || len(gearShops(x.ID)) == 0)
		if !ok {
			continue
		}
		// batch 10: stock what buyers want and the shops lack. Shops were full of Phracon/Emveretarcon/Opal nobody
		// uses (no refining) while buyers asked for Hornet/Fabre/Lunatic Cards: 11 of 12 shop browses found nothing.
		// A card is worth one listing even without a buyer yet (cards sold 4 of 4); materials and gear need a buyer.
		gap := need[x.ID] - listed[x.ID]
		if x.Type == "Card" && need[x.ID] == 0 && listed[x.ID] == 0 {
			gap = 1
		}
		if heal {
			gap = max(gap, 60-listed[x.ID]) // run 8 (owner): healing items are stocked first, ~60 units on the gate
		}
		if gGap {
			gap = max(gap, 1)
		}
		if gap <= 0 {
			continue
		}
		bid := stockBid(b, x.ID, budget) // pricedyn.go (EC48): fixed 0.9 x priceOf unless dynamic_price_on
		if bid <= 0 || o.LowAsk > bid || wantsFor(x.ID, b.charName()) >= o.Sellers {
			continue
		}
		n := min(o.Qty, 3, budget/bid, gap)
		if heal {
			n = min(o.Qty, 50, budget/bid, gap)
		}
		if n < 1 {
			continue
		}
		if heal && (best == nil || !healItem(best.ItemID)) {
			best, bestGap = &Want{ItemID: x.ID, Count: n, Each: bid, Need: b.wantNeed(x.ID), Why: "stock for my shop", Partial: true}, gap
			continue
		}
		if best != nil && healItem(best.ItemID) && !heal {
			continue
		}
		if best == nil || gap > bestGap || gap == bestGap && bid*n > best.Each*best.Count {
			best, bestGap = &Want{ItemID: x.ID, Count: n, Each: bid, Need: b.wantNeed(x.ID), Why: "stock for my shop", Partial: true}, gap
		}
	}
	return best
}

// stockDemand: per item, how many buyers want it (wants that aren't shop stock, variants count, plus the times it
// sold) and how many units the open shops list. Takes econ.mu.
func stockDemand() (need, listed map[int]int) {
	need, listed = map[int]int{}, map[int]int{}
	econ.mu.Lock()
	defer econ.mu.Unlock()
	return stockDemandEcon(need, listed)
}

func stockDemandEcon(need, listed map[int]int) (map[int]int, map[int]int) {
	for _, sh := range econ.shops {
		for _, l := range sh.items {
			listed[l.ItemID] += max(l.Count, 0)
		}
	}
	for _, o := range econ.offers {
		for _, w := range econ.wants {
			if w.Why != "stock for my shop" && wantAccepts(w, o.ItemID) {
				need[o.ItemID] += w.Count
			}
		}
		if st := econ.stats[o.ItemID]; st != nil {
			need[o.ItemID] += st.N
		}
	}
	return need, listed
}

// ---------------------------------------------------------------- courier (economy batch 11)
// A vendor whose shop lists what a party member wants closes the shop, takes the item out of the cart and delivers
// it (a delivery deal, Deal.Courier). 2026-11-14: most bots are in parties (647 chars) and party members trade only on
// their own map, so "too far: both in parties" (1,227) and "party member, partner can't travel" (643) were the top
// misses while the wanted cards sat in south-gate shops. Vendors are merchant alts: walking an order out is their job.

var courierNext sync.Map // *Bot -> time.Time: one delivery trip per vendor per 40 min (run 10: shop churn)

// courierReady: a vendor with an open shop that may leave it for a delivery (b.mu held)
func (b *Bot) courierReady(now time.Time) bool {
	if b.charRole() != "vendor" || b.vend == nil || !b.vend.Opened || b.MapName != "prontera" || b.BaseLvOr() < 15 ||
		b.pin != nil || b.Dead || b.mkt != nil || b.trip != nil || b.kafra != nil || now.Sub(b.vend.OpenedAt) < 5*time.Minute {
		return false
	}
	if t, ok := courierNext.Load(b); ok && now.Before(t.(time.Time)) {
		return false
	}
	return true
}

// courierSpare: the shop's listing as holdings (b.mu held)
func (b *Bot) courierSpare() []holding {
	var out []holding
	for _, l := range b.vend.Items {
		if l.Count > 0 && l.Price > 0 {
			out = append(out, holding{ItemID: l.ItemID, Count: l.Count, Ask: l.Price})
		}
	}
	return out
}

// courierTake: the deal starts: close the shop and move the item from the cart to the bag (b.mu held)
func (b *Bot) courierTake(itemID, count int, now time.Time) bool {
	var src *InvItem
	for _, it := range b.cart {
		if it.ItemID == itemID && it.Count >= count && !it.Unidentified {
			src = it
		}
	}
	if src == nil || b.vend == nil {
		return false
	}
	idx := src.Index
	go func() {
		b.send(newPkt(0x012e).b) // CZ_REQ_CLOSESTORE
		time.Sleep(500 * time.Millisecond)
		b.send(newPkt(0x0127).u16(uint16(idx)).u32(uint32(count)).b) // cart -> bag
	}()
	b.vend.Opened, b.vend.TriedAt = false, now
	courierNext.Store(b, now.Add(40*time.Minute))
	b.setState("closing shop for a delivery")
	return true
}

// courierEnd: the deal is over; whatever of the item is still in the bag goes back into the cart (b.mu held)
func (b *Bot) courierEnd(itemID int) {
	for _, it := range b.inv {
		if it.ItemID == itemID && it.Count > 0 && !it.Equipped {
			idx, n := it.Index, it.Count
			go b.send(newPkt(0x0126).u16(uint16(idx)).u32(uint32(n)).b)
			return
		}
	}
}
