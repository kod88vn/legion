package main

// Shop announcements (owner rule EC35, 2026-11-14): a merchant with an open vending shop now and then tells #trade
// about the rare things in it (cards, katana, slotted / drop-only gear, valuable materials), the way real vendors
// do: "S> Neo Mineral Card in my shop @ prontera 160,60, 45k". Only items the shop lists right now are named, so
// an item stops being announced once it sells. Each merchant speaks at most once every 10-20 minutes (jittered),
// the whole server at most one shop line every shop_announce_gap_sec, and every line also takes the economy's
// #trade slot (econ.lastLine / econLineGap), so B>/S>/shop lines together stay inside EC33's public-chat budget.
//
// Tuning (tuning.json): shop_announce_on (default true), shop_announce_gap_sec (default 45).
//
// Hook: econRound calls shopAnnounceRound(snaps, now) once a round with no lock held.
// Locking: shopAnn.mu, then econ.mu inside it (never the other way round). No bot lock is taken (snaps are copies).

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

const (
	shopAnnMinGap   = 10 * time.Minute // per merchant: next line between min and max after the last
	shopAnnMaxGap   = 20 * time.Minute
	shopAnnSettle   = 2 * time.Minute // a freshly opened shop sets up before it is advertised
	shopAnnFirstMax = 10 * time.Minute
)

var shopAnn = struct {
	sync.Mutex
	next  map[string]time.Time // merchant -> earliest next announcement
	last  time.Time            // last shop line, server-wide
	lines int64
}{next: map[string]time.Time{}}

func shopAnnounceOn() bool { return tune().ShopAnnounceOn }

func shopAnnounceGap() time.Duration {
	if s := tune().ShopAnnounceGapSec; s > 0 {
		return time.Duration(s) * time.Second
	}
	return 45 * time.Second
}

// rareShopItem: worth telling #trade about? Cards always; gear that is slotted or that no NPC sells, from 1k up;
// anything else (materials, Yggdrasil Berries ...) only when no NPC sells it and it is worth 3k+ and well above
// what an NPC pays. Potions, ammo and junk never.
func rareShopItem(id, price int) bool {
	it := items[id]
	if it == nil || price <= 0 || isHealPotionOrSP(id) {
		return false
	}
	npcSold := it.Buy > 0 && len(gearShops(id)) > 0
	npcPays := it.Buy / 2
	switch it.Type {
	case "Card":
		return true
	case "Weapon", "Armor":
		return price >= 1000 && (it.Slots > 0 || !npcSold)
	case "Ammo":
		return false
	}
	return !npcSold && price >= 3000 && price >= 4*max(npcPays, 1)
}

// rareListings: the shop's rare items, most valuable first.
func rareListings(shop []vendListing) []vendListing {
	var out []vendListing
	for _, l := range shop {
		if l.Count > 0 && rareShopItem(l.ItemID, l.Price) {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Price > out[j].Price })
	return out
}

func jitterDur(lo, hi time.Duration) time.Duration {
	if hi <= lo {
		return lo
	}
	return lo + time.Duration(crand(int((hi-lo)/time.Second)))*time.Second
}

// shopAnnounceRound (econRound, no lock held): at most one shop line per call.
func shopAnnounceRound(snaps []*econSnap, now time.Time) {
	if !shopAnnounceOn() {
		return
	}
	s, rare := shopAnnPick(snaps, now)
	if s == nil {
		return
	}
	go exchangeTradePost(s.b, styled(s.typing, shopAnnLine(rare, s.x, s.y)))
}

// shopAnnPick chooses the merchant that speaks now (or nil) and books the line.
func shopAnnPick(snaps []*econSnap, now time.Time) (*econSnap, []vendListing) {
	shopAnn.Lock()
	defer shopAnn.Unlock()
	seen := map[string]bool{}
	var due []*econSnap
	dueRare := map[string][]vendListing{}
	for _, s := range snaps {
		if len(s.shop) == 0 || s.b == nil {
			continue
		}
		seen[s.name] = true
		if !s.openedAt.IsZero() && now.Sub(s.openedAt) < shopAnnSettle {
			continue
		}
		next, ok := shopAnn.next[s.name]
		if !ok { // first sight (or after a restart): don't let every shop speak at once
			shopAnn.next[s.name] = now.Add(jitterDur(time.Minute, shopAnnFirstMax))
			continue
		}
		if now.Before(next) {
			continue
		}
		if r := rareListings(s.shop); len(r) > 0 {
			due = append(due, s)
			dueRare[s.name] = r
		}
	}
	for n := range shopAnn.next { // closed shops forget their schedule
		if !seen[n] {
			delete(shopAnn.next, n)
		}
	}
	if len(due) == 0 || now.Sub(shopAnn.last) < shopAnnounceGap() {
		return nil, nil
	}
	s := due[crand(len(due))]
	econ.mu.Lock()
	if now.Sub(econ.lastLine) < econLineGap {
		econ.mu.Unlock()
		return nil, nil
	}
	econ.lastLine = now
	econ.posted[s.name] = now
	econ.mu.Unlock()
	shopAnn.last = now
	shopAnn.next[s.name] = now.Add(jitterDur(shopAnnMinGap, shopAnnMaxGap))
	shopAnn.lines++
	return s, dueRare[s.name]
}

// shopAnnLine: one natural #trade line about one or two of the shop's rare items.
func shopAnnLine(rare []vendListing, x, y int) string {
	if len(rare) == 0 {
		return ""
	}
	// mostly the best item, sometimes another one, so a shop with several cards doesn't repeat itself
	a := rare[0]
	if len(rare) > 1 && cfloat() < 0.35 {
		a = rare[1+crand(len(rare)-1)]
	}
	n, p := itemName(a.ItemID), fmtZ(a.Price)
	at := pick(fmt.Sprintf("prontera %d,%d", x, y), fmt.Sprintf("prt %d %d", x, y), "south gate", "prt south gate",
		fmt.Sprintf("south gate %d,%d", x, y))
	if len(rare) > 1 && cfloat() < 0.3 {
		var b vendListing
		for _, l := range rare {
			if l.ItemID != a.ItemID {
				b = l
				break
			}
		}
		if b.ItemID != 0 {
			m, q := itemName(b.ItemID), fmtZ(b.Price)
			return pick(
				fmt.Sprintf("S> %s %s, %s %s @ %s", n, p, m, q, at),
				fmt.Sprintf("selling %s %s and %s %s, shop at %s", n, p, m, q, at),
				fmt.Sprintf("S> %s, %s in my shop @ %s", n, m, at),
			)
		}
	}
	return pick(
		fmt.Sprintf("S> %s in my shop @ %s, %s", n, at, p),
		fmt.Sprintf("selling %s %s, shop at %s", n, p, at),
		fmt.Sprintf("S> %s %s, vending at %s", n, p, at),
		fmt.Sprintf("WTS %s %s, come by my shop (%s)", n, p, at),
		fmt.Sprintf("%s for %s in my shop, %s", n, p, at),
		fmt.Sprintf("S> %s %s @ %s", n, p, at),
	)
}
