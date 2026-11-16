package main

// Vending: merchant alts (role "vendor") walk to their own pitch in the Prontera market, cast Vending
// (MC_VENDING 41) and, when the server answers with ZC_OPENSTORE 0x012d, open the shop with their cart contents
// (CZ_REQ_OPENSTORE2 0x01b2: len, title[80], flag, then per item cart index u16, amount u16, price u32).
// Shop titles and price levels can be set by the mastermind (POST /api/bots/{name}/shop).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type VendState struct {
	Spot     [2]int
	Opened   bool
	TriedAt  time.Time
	Title    string
	Markup   float64
	OpenedAt time.Time
	Items    []vendListing // what the open shop lists (vendbuy.go: bots buy from it)
	Sold     int           // items sold since it opened
	SoldZeny int

	restock    bool      // batch 18: load the bag's new goods into a cart that isn't empty
	restockAt  time.Time // next restock check (closing the shop to restock at most every 30 min)
	identUntil time.Time // batch 18: wait for the Magnifiers before opening, until then

	shopAskAt  time.Time // vendpitch.go: when the shop list (0x01b2) last went out
	loadTries  int       // vendpitch.go: empty-cart load rounds without a result
	pauseUntil time.Time // vendpitch.go: vending given up until then (the cart won't load)
}

// market pitches: a grid of walkable cells at Prontera's south gate (the market place, owner's choice), 3 cells
// apart, handed out first come first served
var market = struct {
	sync.Mutex
	free  [][2]int
	taken map[*Bot][2]int
}{taken: map[*Bot][2]int{}}

func initMarket() {
	gm := maps["prontera"]
	if gm == nil {
		return
	}
	for y := 45; y <= 78; y += 3 {
		for x := 133; x <= 172; x += 3 {
			if (x >= 152 && x <= 160) || !gm.Walkable(x, y) { // keep the main north-south road clear
				continue
			}
			market.free = append(market.free, [2]int{x, y})
		}
	}
}

func takePitch(b *Bot) ([2]int, bool) {
	market.Lock()
	defer market.Unlock()
	if p, ok := market.taken[b]; ok {
		return p, true
	}
	reclaimed := 0
	if vendPitchFixOn() {
		reclaimed = reclaimStalePitchesLocked(time.Now()) // vendpitch.go: vendors that left the market
	}
	if len(market.free) == 0 {
		return [2]int{}, false
	}
	i := b.rng.Intn(len(market.free))
	p := market.free[i]
	market.free = append(market.free[:i], market.free[i+1:]...)
	market.taken[b] = p
	pitchTakenLocked(b, time.Now())
	for ; reclaimed > 0; reclaimed-- {
		econCount("vend pitch reclaimed (vendor left the market)")
	}
	return p, true
}

func releasePitch(b *Bot) {
	market.Lock()
	defer market.Unlock()
	if p, ok := market.taken[b]; ok {
		delete(market.taken, b)
		market.free = append(market.free, p)
	}
	forgetPitchLocked(b)
}

// vendPrice: what a player would ask: the economy's price standard (economy.go priceOf: NPC price when an NPC sells
// it, rarity otherwise, nudged by recent trades) with the vendor's own margin (greedier vendors ask more; the market
// mind can set a markup). Shops priced at random never sold anything to the players that check prices.
func (b *Bot) vendPrice(id int) int {
	if dynamicPriceOn() { // pricedyn.go (EC48, tuning dynamic_price_on): supply, urgency and budget
		return b.vendPriceDyn(id)
	}
	return b.vendPriceFixed(id)
}

// vendPriceFixed: the listing price before dynamic_price_on (greed x personal, undercut, markdown steps)
func (b *Bot) vendPriceFixed(id int) int {
	if items[id] == nil {
		return 1000
	}
	m := b.vend.Markup
	if m == 0 {
		m = 0.95 + 0.35*b.P.Greed
	}
	ref := priceOf(id)
	p := npcMatCap(id, npcGearCap(id, roundPrice(int(float64(ref)*m*personal(b.P.Name, id)))))
	// market.go (batch 21): undercut the lowest other shop by 2%, then the markdown for a long unsold listing
	low := 0
	econ.mu.Lock()
	for _, sh := range econ.shops {
		if sh.vendor == b {
			continue
		}
		for _, l := range sh.items {
			if l.ItemID == id && l.Count > 0 && l.Price > 0 && (low == 0 || l.Price < low) {
				low = l.Price
			}
		}
	}
	econ.mu.Unlock()
	if low > 0 && p > low {
		p = max(low*98/100, marketFloor(id, ref))
	}
	// run 8: never under the NPC sell value (a glut stops at the floor; Red Potions were listed at 5-11z, NPC pays 25z)
	return npcWindow(id, max(roundPrice(markdown(b.P.Name, id, p, ref)), marketFloor(id, ref), 2)) // run 11: under the NPC price
}

func (b *Bot) vendThink(gm *GameMap, moving bool, now time.Time) {
	if b.vend == nil {
		b.vend = b.newVendState()
	}
	v := b.vend
	if b.MapName != "prontera" {
		b.Goal = Goal{Mode: "vend", Map: "prontera", Set: b.Goal.Set}
		b.travel(gm, moving)
		return
	}
	b.notePitch(now) // vendpitch.go: keeps the pitch; a lost one is retaken or replaced
	// batch 18: the vendor's Magnifier trip (identify.go) runs from here (vendors never reach ai.go's tripThink)
	// run 7: also the vendor-alchemist's potion-bottle trip (alchemyThink sets Why "alchemy"; it waited forever here)
	if b.trip != nil && (b.trip.Why == "magnifier" || b.trip.Why == "alchemy") && b.tripThink(gm, moving, now) {
		return
	}
	if v.Opened && shopIsGhost(b) { // shopcheck.go: the server has no shop for us: close and set up again
		go b.send(newPkt(0x012e).b)
		v.Opened, v.TriedAt = false, now
		b.cartLoadAt = time.Time{}
	}
	if v.Opened && b.alchShopClose(now) { // alchemy.go: the guide errand, a brew session or supplies (run 7)
		go b.send(newPkt(0x012e).b)
		v.Opened, v.TriedAt = false, now
		b.cartLoadAt = time.Time{}
		econCount("vendor alchemist: shop closed for the craft")
		b.event("info", "", "closing my shop for alchemy (guide / brewing / supplies)")
		return
	}
	if v.Opened {
		// batch 18: the cart was loaded only when empty, so gear that came later (parcels from field bots, parcel.go;
		// courier leftovers; loot) waited in the bag while the shop stayed open: close and restock now and then
		// run 10 (market mind: Holy Mart Orange x55 -> 0 -> x10 -> 0 in ~20 min): a shop stays open at least 40 min
		// and restocks at most once an hour - real vendors don't close every 20 min
		if now.Sub(v.OpenedAt) > 40*time.Minute && now.After(v.restockAt) {
			v.restockAt = now.Add(60 * time.Minute)
			if b.vendRestockNeed() {
				go b.send(newPkt(0x012e).b)
				v.Opened, v.TriedAt, v.restock = false, now, true
				b.cartLoadAt = time.Time{}
				econCount("vendor restock: shop closed")
				b.setState("restocking the shop")
				return
			}
		}
		b.setState("vending: " + v.Title)
		return
	}
	// a fresh Merchant has no cart: rent one at the nearest Prontera Kafra (Kafra menu "Rent a Pushcart")
	if !b.hasCart {
		if b.kafra == nil {
			if now.Before(b.kafraNext) {
				b.loiter(gm, moving)
				return
			}
			sp := kafraSpots["prontera"][0]
			for _, s := range kafraSpots["prontera"][1:] {
				if dist(b.X, b.Y, s.X, s.Y) < dist(b.X, b.Y, sp.X, sp.Y) {
					sp = s
				}
			}
			b.kafra = &kafraTrip{Cart: true, Spot: sp, Town: "prontera", Stage: "walk", Started: now, At: now}
		}
		b.kafraThink(gm, moving, now)
		return
	}
	// batch 18: gear to identify first (Magnifiers from the south gate Tools Merchant, identify.go)
	if b.vendMagnifierTrip(now) {
		return
	}
	if k := b.vendUnidentified(); k > 0 && b.invIndex(itemMagnifier) >= 0 {
		if v.identUntil.IsZero() {
			v.identUntil = now.Add(time.Duration(k*20) * time.Second)
		}
		if now.Before(v.identUntil) {
			b.setState(fmt.Sprintf("identifying %d items for the shop", k))
			return
		}
	} else {
		v.identUntil = time.Time{}
	}
	// load what we want to sell into the cart (vending sells from the cart)
	if len(b.cart) == 0 || v.restock {
		v.restock = false
		if now.Sub(b.cartLoadAt) > 6*time.Second {
			b.cartLoadAt = now
			n := 0
			for _, it := range healFirst(b.inv) { // run 8 (owner): healing items are the first thing the shop sells
				if x := items[it.ItemID]; x != nil && it.Count > 0 && !it.Equipped && b.vendable(it, x) && n < 10 {
					go b.send(newPkt(0x0126).u16(uint16(it.Index)).u32(uint32(it.Count)).b)
					n++
				}
			}
			b.setState("loading the cart")
			if n == 0 {
				b.setState("vendor (nothing to sell)")
			} else if len(b.cart) == 0 && b.cartLoadStuck(now) { // vendpitch.go: moves refused: reset / pause
				return
			}
		}
		return
	}
	// NPC junk loaded before (Jellopy at 3z) comes back to the bag for the next NPC trip; the shop sells what players buy
	if !v.Opened && now.Sub(b.cartLoadAt) > 6*time.Second {
		junk, good := 0, 0
		for _, it := range b.cart {
			if x := items[it.ItemID]; x != nil && it.Count > 0 {
				if b.vendable(it, x) {
					good++
				} else {
					junk++
					go b.send(newPkt(0x0127).u16(uint16(it.Index)).u32(uint32(it.Count)).b)
				}
			}
		}
		if junk > 0 {
			b.cartLoadAt = now
			b.setState("unloading junk from the cart")
			return
		}
		if good == 0 {
			b.setState("vendor (nothing to sell)")
			return
		}
	}
	if v.Spot == ([2]int{}) {
		p, ok := takePitch(b)
		if !ok {
			b.loiter(gm, moving)
			return
		}
		v.Spot = p
	}
	if dist(b.X, b.Y, v.Spot[0], v.Spot[1]) > 0 {
		b.setState("walking to market pitch")
		if !moving && gm != nil {
			b.stepAlong(gm, v.Spot[0], v.Spot[1])
		}
		return
	}
	if now.Sub(v.TriedAt) > 12*time.Second {
		v.TriedAt = now
		lv := 10
		if si := b.skills[mcVending]; si != nil && si.Lv > 0 {
			lv = si.Lv
		}
		b.setState("setting up shop")
		go b.useSkill(mcVending, lv, b.aid)
	}
}

// openShop is called when the server sends ZC_OPENSTORE (slots = max items).
// healFirst (run 8): a bag / cart in a stable order with healing items first (owner: "the market should first
// prioritize hp restore items to sell")
func healFirst(m map[int]*InvItem) []*InvItem {
	out := make([]*InvItem, 0, len(m))
	for _, it := range m {
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool {
		hi, hj := healItem(out[i].ItemID), healItem(out[j].ItemID)
		if hi != hj {
			return hi
		}
		return out[i].Index < out[j].Index
	})
	return out
}

func (b *Bot) openShop(slots int) {
	if b.vend == nil {
		b.vend = b.newVendState()
	}
	v := b.vend
	n := 0
	var listing []vendListing
	for _, it := range healFirst(b.cart) {
		if n >= slots {
			break
		}
		if x := items[it.ItemID]; it.Count <= 0 || x == nil || !b.vendable(it, x) {
			continue
		}
		price := b.vendPrice(it.ItemID)
		cnt := it.Count
		if glutted(it.ItemID) {
			cnt = min(cnt, 10) // market.go (run 8): a glutted item - a few on the shelf, the rest waits for demand
		}
		listing = append(listing, vendListing{Index: it.Index, ItemID: it.ItemID, Count: cnt, Price: price})
		n++
	}
	// run 10 (market mind: Ravenclaw10 Shop "S> Red Potion, Poring Card" holding Apple / Carrot): the title must name
	// what the shop actually lists - checked against the listing, not the cart (unlisted junk / sold-out goods)
	if v.Title != "" && !titleMatchesListing(v.Title, listing) {
		old := v.Title
		v.Title = listingTitle(listing)
		econCount("shop title replaced (not in the listing)")
		dropShopTitle(b.charName())
		b.event("info", "", "shop title \""+old+"\" didn't match my shop, now \""+v.Title+"\"")
	}
	if v.Title == "" {
		v.Title = listingTitle(listing)
	}
	p := newPkt(0x01b2).u16(0)
	title := v.Title
	if len(title) > 79 {
		title = title[:79]
	}
	p.str(title, 80).u8(1)
	for _, l := range listing {
		p.u16(uint16(l.Index)).u16(uint16(l.Count)).u32(uint32(l.Price))
	}
	if n == 0 {
		// nothing to sell (empty cart): still answer, with flag 0 - otherwise the server keeps the character in
		// "preparing a shop" (prevend + WIP_DISABLE_ALL) and it can never walk again (Puddingfire Trade sat frozen at
		// alberta_in 180,94 for 13+ minutes, "stuck ... unsticking" x39, 2026-11-14)
		go b.send(newPkt(0x01b2).u16(0).str(title, 80).u8(0).varlen())
		return
	}
	go b.send(p.varlen())
	v.shopAskAt, v.loadTries = time.Now(), 0 // vendpitch.go
	v.Opened, v.OpenedAt, v.Items, v.Sold, v.SoldZeny = true, time.Now(), listing, 0, 0
	b.event("market", "", fmt.Sprintf("opened shop \"%s\" at prontera %d,%d with %d items", title, b.X, b.Y, n))
}

// onOpenStoreAck: the server's answer to our shop (b.mu held). 2026-11-14: rAthena refuses the whole shop when one
// item is unidentified/untradeable, but Opened was set when we asked, so the vendor sat "vending" with no shop and 10
// of 14 buyers walked up to it for nothing ("shop did not open").
func (b *Bot) onOpenStoreAck(pk []byte) {
	if len(pk) < 3 || b.vend == nil {
		return
	}
	if pk[2] == 0 {
		shopGhost.Delete(b) // really open: back on the market
		return
	}
	b.vend.Opened, b.vend.TriedAt = false, time.Now()
	b.cartLoadAt = time.Time{}
	econCount("shop open refused")
	if t, ok := shopRefusedAt.Load(b); !ok || time.Since(t.(time.Time)) > 10*time.Minute {
		shopRefusedAt.Store(b, time.Now())
		b.event("info", "", "my shop didn't open (an item in the cart can't be sold)")
	}
}

var shopRefusedAt sync.Map // *Bot -> time.Time: diary line at most every 10 min

// listingTitle (run 10): "S> a, b, c" from what the shop lists - cards and healing items first
func listingTitle(listing []vendListing) string {
	var names []string
	seen := map[string]bool{}
	for pass := 0; pass < 2 && len(names) < 3; pass++ {
		for _, l := range listing {
			x := items[l.ItemID]
			if x == nil || seen[x.Name] || len(names) >= 3 {
				continue
			}
			first := x.Type == "Card" || healItem(l.ItemID)
			if pass == 0 && first || pass == 1 && !first {
				names = append(names, x.Name)
				seen[x.Name] = true
			}
		}
	}
	if len(names) == 0 {
		return "Cheap stuff"
	}
	return "S> " + strings.Join(names, ", ")
}

// titleMatchesListing (run 10): the title names at least one listed item and no item it doesn't list. Free-text
// titles from the market mind ("cheap pots!") are kept when they name no item at all.
func titleMatchesListing(title string, listing []vendListing) bool {
	t := strings.ToLower(title)
	listed := map[int]bool{}
	hit := false
	rest := t // the title with every listed name taken out: what's left must name nothing else
	for _, l := range listing {
		listed[l.ItemID] = true
		if x := items[l.ItemID]; x != nil {
			n := strings.ToLower(x.Name)
			if strings.Contains(t, n) {
				hit = true
				rest = strings.ReplaceAll(rest, n, " ")
			}
		}
	}
	for id, n := range titleNames() {
		if !listed[id] && strings.Contains(rest, n) {
			return false
		}
	}
	return hit || !strings.Contains(t, "s>") && !strings.Contains(t, ",")
}

var titleNamesOnce struct {
	sync.Once
	m map[int]string
}

// titleNames: lowercase names of the items a shop title may name (cards, gear, potions, valuable materials, food)
func titleNames() map[int]string {
	titleNamesOnce.Do(func() {
		titleNamesOnce.m = map[int]string{}
		for id, x := range items {
			if x != nil && len(x.Name) >= 4 && (x.Type == "Card" || valuableEtc[id] || x.Type == "Weapon" || x.Type == "Armor" ||
				healItem(id) || x.Type == "Healing") {
				titleNamesOnce.m[id] = strings.ToLower(x.Name)
			}
		}
	})
	return titleNamesOnce.m
}

func (b *Bot) defaultShopTitle() string {
	var names []string
	for _, it := range b.cart {
		if x := items[it.ItemID]; x != nil && (x.Type == "Card" || len(names) < 2) {
			names = append(names, x.Name)
		}
		if len(names) >= 3 {
			break
		}
	}
	if len(names) == 0 {
		return "Cheap stuff"
	}
	t := "S> " + names[0]
	for _, n := range names[1:] {
		t += ", " + n
	}
	return t
}

// canVend: merchants open shops only once they've actually learned Vending (real progression).
// canVend: knows Vending and Pushcart, and has stock (a loaded cart, or enough loot in the bag to fill one).
// Until then a vendor character levels and farms like everyone else.
func (b *Bot) canVend() bool {
	if s := b.skills[mcVending]; s == nil || s.Lv == 0 {
		return false
	}
	if s := b.skills[mcPushcart]; s == nil || s.Lv == 0 {
		return false
	}
	if b.vendPaused(time.Now()) { // vendpitch.go: the cart wouldn't load; farm a while
		return false
	}
	if b.vend != nil && b.vend.Opened || len(b.cart) > 0 {
		return true
	}
	if !b.hasCart && b.Zeny < 1200 { // cart rental
		return false
	}
	// worth a shop: a few things players buy (cards, materials, spare gear, brewed potions), not a bag of Jellopy
	n, value := 0, 0
	for _, it := range b.inv {
		if x := items[it.ItemID]; x != nil && it.Count > 0 && !it.Equipped && b.vendable(it, x) {
			n++
			value += priceOf(x.ID) * it.Count
		}
	}
	return n >= 2 && value >= 1500
}

// ---------------------------------------------------------------- shop titles survive reopens and restarts

// The market mind sets titles and markups (POST /api/bots/{name}/shop). b.vend is reset at every session start, so
// the title fell back to the generic "S> ..." on every reopen (~40 times in 2.5 h, market mind report 2026-11-14).
// Now the last title/markup per character is kept in data/shoptitles.json (shoptitles_core.json for legion-core).
type shopTitle struct {
	Title  string  `json:"title"`
	Markup float64 `json:"markup,omitempty"`
}

var shopTitles = struct {
	sync.Mutex
	m map[string]shopTitle
}{}

func shopTitleFile() string {
	if coreInstance() {
		return filepath.Join(*dataDir, "shoptitles_core.json")
	}
	return filepath.Join(*dataDir, "shoptitles.json")
}

func loadShopTitlesLocked() {
	if shopTitles.m != nil {
		return
	}
	shopTitles.m = map[string]shopTitle{}
	if raw, err := os.ReadFile(shopTitleFile()); err == nil {
		json.Unmarshal(raw, &shopTitles.m)
	}
}

// rememberShopTitle stores a character's shop title and markup (called by the API when the market mind sets them).
func rememberShopTitle(name, title string, markup float64) {
	if name == "" || title == "" {
		return
	}
	shopTitles.Lock()
	defer shopTitles.Unlock()
	loadShopTitlesLocked()
	shopTitles.m[name] = shopTitle{Title: title, Markup: markup}
	if raw, err := json.MarshalIndent(shopTitles.m, "", " "); err == nil {
		os.WriteFile(shopTitleFile(), raw, 0644)
	}
}

// dropShopTitle forgets a saved title (keeps the markup): shopcheck.go fixShopTitle found it names goods the cart
// doesn't hold. Before (batch 14), the stale title was reloaded on every shop open and replaced again each time,
// so the market mind saw "ok" but the shop always showed the default title (Onistorm Deals: "Hornet Card, fair
// price" saved, the Hornet Card long sold).
func dropShopTitle(name string) {
	shopTitles.Lock()
	defer shopTitles.Unlock()
	loadShopTitlesLocked()
	if t, ok := shopTitles.m[name]; ok && t.Title != "" {
		t.Title = ""
		shopTitles.m[name] = t
		if raw, err := json.MarshalIndent(shopTitles.m, "", " "); err == nil {
			os.WriteFile(shopTitleFile(), raw, 0644)
		}
	}
}

// newVendState: a fresh shop state with the character's saved title and markup (b.mu held).
func (b *Bot) newVendState() *VendState {
	shopTitles.Lock()
	defer shopTitles.Unlock()
	loadShopTitlesLocked()
	t := shopTitles.m[b.charName()]
	return &VendState{Title: t.Title, Markup: t.Markup}
}

// vendRestockNeed: goods in the bag worth closing the shop for: 2+ listable items not in the cart yet, or 2+ pieces
// of gear to identify (b.mu held)
func (b *Bot) vendRestockNeed() bool {
	n := 0
	for _, it := range b.inv {
		if x := items[it.ItemID]; x != nil && it.Count > 0 && !it.Equipped && b.vendable(it, x) {
			n++
			if x.Type == "Card" || healItem(x.ID) && it.Count >= 10 {
				n += 2 // a card or a stack of healing items is worth a restock on its own
			}
		}
	}
	return n >= 4 || b.vendUnidentified() >= 2 && (b.invIndex(itemMagnifier) >= 0 || b.Zeny >= 300)
}

// vendPitchRefused: the server refused Vending at this pitch (0x0110 for MC_VENDING: an NPC within
// min_npc_vendchat_distance 3 cells, or a no-vending cell). 07:25: 13 of 35 vendors in Prontera sat on "setting up shop"
// forever, re-casting every 12 s next to the south gate merchants (Tools 164,57, Armor 146,57, Weapons 142,57/60 ...).
// The pitch is dropped for the session (never handed out again) and the vendor takes another (b.mu held).
func (b *Bot) vendPitchRefused() {
	if b.vend == nil || b.vend.Opened {
		return
	}
	market.Lock()
	delete(market.taken, b)
	forgetPitchLocked(b)
	market.Unlock()
	b.vend.Spot, b.vend.TriedAt = [2]int{}, time.Time{}
	econCount("vend pitch refused: moved")
}
