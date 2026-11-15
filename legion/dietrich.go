package main

// dietrich.go (economy batch 19): refine materials from the NPC when no player sells them cheaper. Stock rAthena
// (npc/re/merchants/refine.txt, feature.barter on) has Dietrich in Hollgrehenn's room (prt_in 63,69) selling Phracon
// 200z and Emveretarcon 1,000z through a market shop. 07:25: Phracon "too far / party" 12,647 match misses - party
// members wanting Phracon could never meet a seller - while players asked 250z, above the NPC. Owner/main decision
// (07:40): player offers stay first, but a refine visitor standing in the room buys from Dietrich when no open shop
// sells the material at or below the NPC price; player asks for both cap at 95% of the NPC price (npcMatCap).
//
// Packets (PACKETVER 20211103): talk 0x0090 -> mes/next (0x00b5, answered by answerStrayDialog) -> menu 0x00b7
// (answer 1 "View basic smelting ores") -> close2 0x00b6 (0x0146) -> callshop: ZC_NPC_MARKET_OPEN 0x0b7a
// {nameid.L type.B price.L qty.L weight.W location.L} (0x09d5 without location on older builds) -> CZ_NPC_MARKET_PURCHASE
// 0x09d6 len.W {nameid.L qty.L} -> ZC_NPC_MARKET_PURCHASE_RESULT 0x0b4e len.W result.W (0 = ok) -> close 0x09d8.

import (
	"fmt"
	"strings"
	"time"
)

var dietrichNPC = [2]int{63, 69}

// npcMatPrice: what Dietrich charges
var npcMatPrice = map[int]int{itemPhracon: 200, itemEmveretarcon: 1000}

// npcMatCap: a player's ask for a refine material never tops 95% of the NPC price (who pays a player 250 for a 200z
// Phracon?)
func npcMatCap(id, price int) int {
	if p := npcMatPrice[id]; p > 0 {
		return min(price, p*95/100)
	}
	return price
}

// dietrichBuyCount: how many of the material to buy from Dietrich on this visit (0 = none: a player shop sells it at
// or below the NPC price, or no money to spare) (b.mu held)
func (b *Bot) dietrichBuyCount(mat, need, fee int) int {
	price := npcMatPrice[mat]
	if price == 0 || need <= 0 || b.BaseLvOr() < 12 || b.charRole() == "vendor" || b.guideSaving() {
		return 0
	}
	if tn := tune(); !tn.DietrichEnabled && !nameSet(tn.DietrichTestBots)[strings.ToLower(b.charName())] {
		return 0 // off until tested live
	}
	if b.cheaperInShop(mat, price) {
		return 0 // players first: browseShops / the #trade want buys it from the shop
	}
	spare := b.Zeny - b.gearReserve() - 300
	return max(0, min(need, spare/(price+fee)))
}

// dietrichStep: during a refine visit in Hollgrehenn's room, buy the material first (b.mu held). True while busy.
func (b *Bot) dietrichStep(gm *GameMap, moving bool, now time.Time, t *refineTask) bool {
	if t.Buy <= 0 {
		return false
	}
	switch t.Diet {
	case "talk", "shop":
		if now.Sub(t.DietAt) > 15*time.Second {
			if t.Diet == "shop" {
				go b.send(newPkt(0x09d8).b)
			}
			econCount("refine material: Dietrich did not answer")
			t.Buy, t.Diet = 0, ""
			return false
		}
		b.setState("buying " + itemName(t.Mat) + " from Dietrich")
		return true
	}
	tx, ty, ok := dietrichNPC[0], dietrichNPC[1]-1, false
	if gm != nil {
		tx, ty, ok = talkCell(gm, b.X, b.Y, dietrichNPC[0], dietrichNPC[1])
	}
	if !ok {
		econCount("refine material: Dietrich can't be reached")
		t.Buy = 0
		return false
	}
	if dist(b.X, b.Y, dietrichNPC[0], dietrichNPC[1]) > 3 && !(b.X == tx && b.Y == ty) {
		if !moving && gm != nil {
			before := b.replanAt
			b.stepAlong(gm, tx, ty)
			if b.path == nil && b.replanAt != before {
				if t.NoPath++; t.NoPath >= 2 { // run 8: no path to Dietrich twice -> no material from him this visit
					econCount("refine material: no path to Dietrich")
					t.Buy, t.NoPath = 0, 0
					return false
				}
			}
		}
		b.setState("going to Dietrich for " + itemName(t.Mat))
		return true
	}
	if moving {
		return true
	}
	npc := b.npcAt(dietrichNPC[0], dietrichNPC[1])
	if npc == 0 {
		if now.Sub(t.Started) > 3*time.Minute {
			econCount("refine material: Dietrich not seen")
			t.Buy = 0
			return false
		}
		return true
	}
	t.Diet, t.DietAt = "talk", now
	go b.send(newPkt(0x0090).u32(npc).u8(1).b)
	return true
}

// dietrichMenu: the menu answer while talking to Dietrich (1 = basic smelting ores); 0 = not ours (b.mu held)
func (b *Bot) dietrichMenu() byte {
	if v, ok := refineTasks.Load(b); ok && v.(*refineTask).Diet == "talk" {
		return 1
	}
	return 0
}

// onMarket: an NPC market shop opened (0x0b7a / 0x09d5) or answered a purchase (0x0b4e / 0x09d7) (b.mu held)
func (b *Bot) onMarket(op uint16, pk []byte) {
	var t *refineTask
	if v, ok := refineTasks.Load(b); ok {
		t = v.(*refineTask)
	}
	switch op {
	case 0x0b7a, 0x09d5:
		if t == nil || t.Diet != "talk" || len(pk) < 4 {
			go b.send(newPkt(0x09d8).b) // a market window nobody drives
			return
		}
		sz := 19
		if op == 0x09d5 {
			sz = 15
		}
		price := 0
		for o := 4; o+sz <= len(pk); o += sz {
			if int(le.Uint32(pk[o:])) == t.Mat {
				price = int(le.Uint32(pk[o+5:]))
			}
		}
		n := t.Buy
		if price > 0 {
			n = min(n, (b.Zeny-300)/price)
		}
		if price <= 0 || price > npcMatPrice[t.Mat] || n < 1 {
			go b.send(newPkt(0x09d8).b)
			econCount("refine material: Dietrich price/zeny")
			t.Buy, t.Diet = 0, ""
			return
		}
		t.Diet, t.DietAt, t.DietN, t.DietPrice = "shop", time.Now(), n, price
		go b.send(newPkt(0x09d6).u16(0).u32(uint32(t.Mat)).u32(uint32(n)).varlen())
	case 0x0b4e, 0x09d7:
		go b.send(newPkt(0x09d8).b)
		if t == nil || t.Diet != "shop" || len(pk) < 5 {
			return
		}
		ok := op == 0x0b4e && le.Uint16(pk[4:]) == 0 || op == 0x09d7 && pk[4] == 1
		if ok {
			econCount("refine material: bought from Dietrich")
			b.event("info", "", fmt.Sprintf("bought %d %s from Dietrich for %dz (no player sold it cheaper)", t.DietN, itemName(t.Mat), t.DietN*t.DietPrice))
		} else {
			econCount("refine material: Dietrich refused")
		}
		t.Buy, t.Diet = 0, ""
	}
}
