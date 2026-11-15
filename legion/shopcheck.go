package main

// shopcheck.go: the engine's open shops must be the server's open shops (economy batch 12).
//
// 2026-11-14 QA: 32 of 41 shop buys failed "shop did not open"; 21 different bots walked to LilPixelfang Bazaar, whose
// cart held unidentified Swords/Clubs (rAthena refused the whole shop) while the engine listed it as open; the DB
// `vendings` table had 17 rows vs 23 shops in /api/economy. Three checks:
//   1. a buyer whose shop request got no answer marks the vendor a ghost for everyone (shopGhost): off the market
//      until the vendor really reopens (ZC_ACK_OPENSTORE2 0x0a28 result 0);
//   2. every ~2 min the shops are checked against what the server shows players: ZC_STORE_ENTRY 0x0131 (a shop sign,
//      sent to everyone in view when it opens and to whoever comes into view later). An "open" shop whose sign no bot
//      saw since it opened, while another bot stands next to it, is a ghost. (Legion has no DB access; the signs are
//      the server's own word. ZC_DISAPPEAR_ENTRY 0x0132 is also sent when a shop merely leaves a viewer's sight, so
//      it proves nothing.)
//   3. a persisted shop title that names goods the cart doesn't hold is replaced by one made from the cart.

import (
	"strings"
	"sync"
	"time"
)

var (
	shopGhost  sync.Map // *Bot (vendor) -> time.Time: marked closed until it reopens for real
	storeSigns sync.Map // vendor AID (uint32) -> time.Time: last 0x0131 any bot saw
	storeGone  sync.Map // vendor AID -> time.Time: last 0x0132 (also sent when a shop leaves a viewer's sight: info only)
	shopCheck  = struct {
		sync.Mutex
		at time.Time
	}{}
)

func markShopGhost(vendor *Bot, why string) {
	if _, loaded := shopGhost.LoadOrStore(vendor, time.Now()); !loaded {
		econCount("shop ghost: " + why)
	}
}

func shopIsGhost(vendor *Bot) bool {
	_, ok := shopGhost.Load(vendor)
	return ok
}

// onStoreSign: 0x0131 <aid>.L <title>.80B / 0x0132 <aid>.L, seen by any bot (b.mu held; only sync.Maps touched)
func onStoreSign(op uint16, pk []byte) {
	if len(pk) < 6 {
		return
	}
	aid := le.Uint32(pk[2:])
	if op == 0x0131 {
		storeSigns.Store(aid, time.Now())
	} else {
		storeGone.Store(aid, time.Now())
	}
}

// checkShops runs from the economy round: at most every 2 min, every shop open for 2+ min is checked against the
// signs (snaps: this round's snapshot; s.aid / s.openedAt filled for open shops).
func checkShops(snaps []*econSnap, now time.Time) {
	shopCheck.Lock()
	if now.Sub(shopCheck.at) < 2*time.Minute {
		shopCheck.Unlock()
		return
	}
	shopCheck.at = now
	shopCheck.Unlock()
	for _, s := range snaps {
		if len(s.shop) == 0 || s.aid == 0 || now.Sub(s.openedAt) < 2*time.Minute {
			continue
		}
		if t, ok := storeSigns.Load(s.aid); ok && t.(time.Time).After(s.openedAt.Add(-5*time.Second)) {
			continue // the server shows its sign
		}
		// nobody saw the sign: only a verdict when another bot stands in view of the shop
		for _, v := range snaps {
			if v != s && v.mapN == s.mapN && dist(v.x, v.y, s.x, s.y) <= 12 {
				markShopGhost(s.b, "no sign")
				break
			}
		}
	}
}

// fixShopTitle: a saved title naming goods the cart doesn't hold (and none it does) is replaced (b.mu held).
func (b *Bot) fixShopTitle() {
	v := b.vend
	if v == nil || v.Title == "" {
		return
	}
	t := strings.ToLower(v.Title)
	for _, it := range b.cart {
		if x := items[it.ItemID]; x != nil && it.Count > 0 && b.vendable(it, x) && strings.Contains(t, strings.ToLower(x.Name)) {
			return // names something it sells
		}
	}
	for id, x := range items {
		if x != nil && len(x.Name) >= 4 && (x.Type == "Card" || valuableEtc[id] || x.Type == "Weapon" || x.Type == "Armor") &&
			strings.Contains(t, strings.ToLower(x.Name)) {
			old := v.Title
			v.Title = b.defaultShopTitle()
			econCount("shop title replaced (stale)")
			dropShopTitle(b.charName()) // vend.go: don't reload the stale title on the next open
			b.event("info", "", "shop title \""+old+"\" didn't match my cart, now \""+v.Title+"\"")
			return
		}
	}
}
