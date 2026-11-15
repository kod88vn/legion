package main

// Economy: bots buy and sell gear, cards and materials from each other by real player trades (owner: "the economy
// must exist. Bots should ask for what they need and be able to trade with each other on the economy standard";
// "they can ask in global chat for what they want and initiate a trade with a fair price").
//
//   - priceOf(id) is the price standard everyone uses: the NPC price when an NPC sells the item, otherwise a value
//     from rarity (cards by the level of the monster that drops them, gear by level/attack/defense/slots,
//     valuable materials at 2-3x the NPC sell price), nudged by the prices of recent trades. Each persona asks or
//     bids around it according to its Greed trait.
//   - Want board: a bot whose next item on its build plan is drop-only or sold far away (or a few cheaper-than-NPC
//     buys), or that is out of Red Potions in the field, registers a Want and posts "B> <item> <price>" on #trade.
//     Bots with spare cards, gear they won't wear and valuable materials register Offers and post "S>" lines.
//   - econManager (a goroutine like partyManager) matches Wants with holders: the seller whispers the buyer
//     in character, then they meet - on the same map the seller walks to the buyer; otherwise both go to Prontera's
//     south gate (the market place) with the usual travel/Kafra code. Party members of a human-led party only
//     trade with bots on their own map (they never leave the party for a trade).
//   - The trade itself (0x00e4 request, 0x00e6 accept, 0x00e8 add item / zeny, 0x00eb lock, 0x00ef commit): each side
//     verifies what the other put in before locking, and every stage has a timeout so nobody stays frozen.
//   - GET /api/economy: open wants/offers, deals in progress, recent trades, per-item average prices.
//
// Locking: the manager locks one bot at a time (b.mu) and never holds econ.mu while locking a bot. Bot code may take
// econ.mu (bot -> econ order) and Deal.mu (bot -> deal order).

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	econTick        = 15 * time.Second
	econMaxDeals    = 60               // trades in progress at once
	econPostEvery   = 10 * time.Minute // per bot, for B>/S> lines
	econLineGap     = 6 * time.Second  // whole server: at most ~10 economy lines a minute on #trade
	econWantLife    = 20 * time.Minute
	econTradeWindow = 30 * time.Second // an open trade window that isn't done by then is cancelled
)

var econTrades int64 // completed bot-to-bot trades (atomic), shown in /api/overview

// ---------------------------------------------------------------- the price standard

var (
	cardLvOnce sync.Once
	cardLv     map[string]*Mob // "Poring Card" -> Poring
)

func cardMob(name string) *Mob {
	cardLvOnce.Do(func() {
		cardLv = map[string]*Mob{}
		for _, m := range mobs {
			if m.Name == "" {
				continue
			}
			k := strings.ToLower(m.Name + " Card")
			// several mobs share a name (event/instance copies at lv 90+): the ordinary one is the low-level non-boss
			if o := cardLv[k]; o == nil || (o.Boss && !m.Boss) || (o.Boss == m.Boss && m.Level > 0 && (m.Level < o.Level || o.Level == 0)) {
				cardLv[k] = m
			}
		}
	})
	return cardLv[strings.ToLower(name)]
}

// standardPrice: the economy standard for one item, before anyone's personal spread.
func standardPrice(id int) int {
	it := items[id]
	if it == nil {
		return 0
	}
	if it.Buy > 0 && len(gearShops(id)) > 0 {
		return it.Buy // an NPC sells it: nobody pays more, nobody sells for much less
	}
	if p, ok := dynPrice(id); ok { // pricing.go (batch 21): wealth x rarity, when tuning dynamic_prices is on
		return p
	}
	return fixedStandardPrice(id)
}

// fixedStandardPrice: the formula before pricing.go (still the floor of the dynamic table)
func fixedStandardPrice(id int) int {
	it := items[id]
	if it == nil {
		return 0
	}
	if it.Buy > 0 && len(gearShops(id)) > 0 {
		return it.Buy
	}
	p := 0
	switch it.Type {
	case "Card":
		// scaled to the money the players have (2026-11-14: bots hold ~1-3k each; at 20k+ no card ever sold)
		p = 3000
		if m := cardMob(it.Name); m != nil {
			p = 500 + m.Level*250 // Poring 750, Lunatic 1,250, Hornet 2,500, Andre ~8,800
			if p > 50000 {
				p = 50000
			}
			if m.Boss {
				p = 250000
			}
		}
	case "Weapon", "Armor", "Shadowgear":
		if n := namesakePrice(id); n > 0 { // cards.go: Knife [4] next to the NPC's 50z Knife [3]
			return n
		}
		p = max(it.Buy, 800+it.MinLv*150+it.Atk*30+it.Def*500)
		p = p * (10 + 3*it.Slots) / 10 // slotted gear is worth more than the plain one
	case "Etc":
		if valuableEtc[id] {
			p = max(sellPrice(id)*5/2, 100)
		} else {
			p = max(sellPrice(id)*6/5, 2)
		}
	default:
		p = max(it.Buy, 10)
	}
	return p
}

// priceOf: the standard blended with what players actually paid recently (30%) once an item has a few trades.
func priceOf(id int) int {
	p := standardPrice(id)
	econ.mu.Lock()
	st := econ.stats[id]
	econ.mu.Unlock()
	if st != nil && st.N >= 3 && p > 0 {
		p = (7*p + 3*int(st.Sum/int64(st.N))) / 10
	}
	if f := marketFactor(id); f != 1 && p > 0 { // market.go (batch 21): supply and demand
		p = max(int(float64(p)*f), sellPrice(id))
	}
	return roundPrice(p)
}

func roundPrice(p int) int {
	switch {
	case p < 100:
		return max(p, 1)
	case p < 1000:
		return (p + 5) / 10 * 10
	case p < 10000:
		return (p + 25) / 50 * 50
	default:
		return (p + 250) / 500 * 500
	}
}

// personal: a stable +-5% per persona and item, so not everyone quotes the same number
func personal(name string, id int) float64 {
	h := fnv.New32a()
	fmt.Fprintf(h, "%s|%d", name, id)
	return 0.95 + float64(h.Sum32()%101)/1000
}

// askPrice: the least a seller takes (greedy sellers ask more).
func askPrice(p *Persona, id int) int {
	ref := priceOf(id)
	ask := npcMatCap(id, npcGearCap(id, roundPrice(int(float64(ref)*(0.85+0.3*p.Greed)*personal(p.Name, id)))))
	return npcWindow(id, roundPrice(markdown(p.Name, id, ask, ref))) // market.go: unsold for long -> 5% steps down
}

// npcWindow (economy run 11, owner rule "Abundant and cheap; the Prontera market beats the NPCs"): a player price for
// anything an NPC shop also sells sits between what a Merchant gets from the NPC with Overcharge 10 (sell price +24%,
// ~62% of the buy price) and what a Merchant pays with Discount 10 (buy price -24%, 76%): the buyer pays less than at
// any NPC, the seller gets more than any NPC pays. Items no NPC sells keep their market price.
func npcWindow(id, price int) int {
	x := items[id]
	if x == nil || x.Buy <= 0 || len(gearShops(id)) == 0 {
		return price
	}
	lo := max(x.Buy*62/100, sellPrice(id)*124/100, 1)
	hi := max(x.Buy*76/100, lo)
	return min(max(price, lo), hi)
}

// npcGearCap (batch 18): used gear an NPC also sells is priced under the NPC, as real players do. 07:25: 33 wants for
// Adventurer's Suit (NPC 1,000z) bid 910, the 5 sellers asked 990+ ("price too high" 1,036 times): nobody pays the
// NPC price for a used one. Cap 93% (the buyers' "cheaper than NPC" bid takes an ask up to 97%).
func npcGearCap(id, price int) int {
	if x := items[id]; x != nil && (x.Type == "Weapon" || x.Type == "Armor") && x.Buy > 0 && len(gearShops(id)) > 0 {
		return min(price, max(x.Buy*93/100, 1))
	}
	return price
}

// bidPrice: what a buyer offers (greedy buyers offer less). Consumables bought in the field carry a convenience
// premium over the NPC price.
func bidPrice(p *Persona, id int) int {
	f := 1.15 - 0.25*p.Greed
	if it := items[id]; it != nil && (it.Type == "Healing" || it.Type == "Usable") {
		f += 0.2
	}
	bid := roundPrice(int(float64(priceOf(id)) * f * personal(p.Name, id)))
	if np := npcMatPrice[id]; np > 0 {
		bid = min(bid, np) // batch 19: nobody bids over Dietrich's price for Phracon / Emveretarcon
	}
	return bid
}

func fmtZ(n int) string {
	switch {
	case n >= 1000000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + "m"
	case n >= 1000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e3), ".0") + "k"
	}
	return fmt.Sprintf("%dz", n)
}

// ---------------------------------------------------------------- board state

type Want struct {
	Buyer    *Bot      `json:"-"`
	Name     string    `json:"buyer"`
	Map      string    `json:"map"`
	ItemID   int       `json:"item_id"`
	Item     string    `json:"item"`
	Count    int       `json:"count"`
	Each     int       `json:"price_each"`
	Why      string    `json:"why"`
	Created  time.Time `json:"created"`
	Posted   time.Time `json:"posted,omitempty"`
	InDeal   bool      `json:"in_deal"`
	Standard int       `json:"standard"`
	Partial  bool      `json:"partial,omitempty"` // any amount up to Count will do (herbs for an alchemist, potions)
}

type offerRow struct {
	ItemID   int    `json:"item_id"`
	Item     string `json:"item"`
	Sellers  int    `json:"sellers"`
	Qty      int    `json:"qty"`
	LowAsk   int    `json:"lowest_ask"`
	Standard int    `json:"standard"`
}

type Trade struct {
	T      time.Time `json:"t"`
	Seller string    `json:"seller"`
	Buyer  string    `json:"buyer"`
	ItemID int       `json:"item_id"`
	Item   string    `json:"item"`
	Count  int       `json:"count"`
	Price  int       `json:"price"` // total zeny
	Map    string    `json:"map"`
}

type itemStat struct {
	N              int
	Sum            int64 // per-unit prices
	Min, Max, Last int
}

// Deal: one agreed trade between a seller and a buyer. Fields other than the state block are fixed at creation.
type Deal struct {
	ID                    int64
	Seller, Buyer         *Bot
	SellerName, BuyerName string
	ItemID, Count, Price  int    // Price = total zeny
	MeetMap               string // "" = same map (the seller walks to the buyer); else meet there
	MeetX, MeetY          int
	Created, Deadline     time.Time
	Why                   string // "guild fund": a zeny gift toward the guild alchemist's Potion Creation Guide
	Deliver               bool   // the seller travels to the buyer\'s map (the buyer is in a party and stays with it)
	Fetch                 bool   // the buyer (solo) travels to the seller's map (the seller is in a party and stays with it)
	Kind                  string // same / meet / deliver / fetch / gift / town: for the deal counters (econCount)
	Courier               bool   // vendbuy.go: the seller is a vendor delivering an item from its shop's cart

	mu       sync.Mutex
	opened   time.Time
	closed   bool
	reason   string
	pauses   int       // windows cancelled because a monster hit one side (the deal goes on afterwards)
	pausedAt time.Time // when the last one was cancelled
	busyAt   time.Time // run 8: the buyer refused our request because it was being hit / under 70% HP
	busyN    int
	moves    int // run 10: a delivery that followed the buyer to its party's new map
}

// busy (run 8): the buyer refused the seller's request only because it wasn't safe to trade (a monster on it, HP
// under 70%). "trade request not answered" was the top failure: the seller burned its 4 tries in 24 s while the
// buyer fought. The seller now waits for the buyer to be safe (up to 30 refusals, the deal's Deadline still holds).
func (d *Deal) busy(now time.Time) {
	d.mu.Lock()
	d.busyAt = now
	d.busyN++
	d.mu.Unlock()
}

func (d *Deal) partnerBusy(now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.closed && d.busyN <= 30 && now.Sub(d.busyAt) < 10*time.Second
}

// transientFail (run 8): a deal that ended for a passing reason (a fight, an escape) - the pair may try again soon.
func transientFail(reason string) bool {
	switch reason {
	case "trade request not answered", "fled from danger", "hit by a monster", "HP too low to trade", "partner busy / not safe at start":
		return true
	}
	return false
}

// pause: a monster hit one side while the window was open. Instead of killing the deal (10 of 17 failed deals
// 2026-11-14 03:00 were "hit by a monster"), the window is cancelled and re-requested once it is safe - up to 3 times.
func (d *Deal) pause(now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.pauses >= 3 {
		return false
	}
	d.pauses++
	d.pausedAt, d.opened = now, time.Time{}
	return true
}

func (d *Deal) recentlyPaused(now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.closed && now.Sub(d.pausedAt) < 6*time.Second
}

// resetWindow: back to "not traded yet" on this side after a paused window (b.mu held)
func (s *marketSide) resetWindow(now time.Time) {
	s.open, s.added, s.partnerOK, s.lockSent, s.selfLocked, s.partnerLocked, s.sent = false, false, false, false, false, false, false
	s.tries, s.reqAt = 0, now.Add(4*time.Second)
}

func (d *Deal) close(reason string) {
	d.mu.Lock()
	if !d.closed {
		d.closed, d.reason = true, reason
	}
	d.mu.Unlock()
}

func (d *Deal) state() (closed bool, reason string, opened time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed, d.reason, d.opened
}

// marketSide: one bot's part in a deal (guarded by that bot's b.mu).
type marketSide struct {
	d                    *Deal
	seller               bool
	me, partner          string // character names
	savedGoal            Goal
	goalSet              bool
	reqAt                time.Time
	tries                int
	open                 bool // trade window open
	added, partnerOK     bool
	lockSent, selfLocked bool
	partnerLocked, sent  bool // sent = commit sent
	done                 bool
}

var econ = struct {
	mu       sync.Mutex
	wants    map[string]*Want // buyer char name -> want
	offers   []offerRow
	deals    []*Deal
	trades   []Trade // newest last, capped
	stats    map[int]*itemStat
	posted   map[string]time.Time // char -> last B>/S> line
	nextWant map[string]time.Time // char -> next time its needs are re-checked
	noPair   map[string]time.Time // "seller|buyer" -> don't pair again before
	lastLine time.Time
	nextID   int64
	failed   int64
	shops    []shopRef // vendbuy.go: open vending shops (refreshed every round), browsed on town runs
}{wants: map[string]*Want{}, stats: map[int]*itemStat{}, posted: map[string]time.Time{},
	nextWant: map[string]time.Time{}, noPair: map[string]time.Time{}}

// ---------------------------------------------------------------- what a bot needs and what it can spare

// econWant: the one thing this bot asks the market for right now (b.mu held). nil = nothing.
func (b *Bot) econWant() *Want {
	lv := b.BaseLvOr()
	// field restock: out of potions far from a shop
	if lv >= 5 && b.potionCount() < 5 && townOf(b.MapName) == "" && b.Zeny > 1500 {
		each := bidPrice(b.P, itemRedPotion)
		n := 10
		if each*n <= b.Zeny/3 {
			return &Want{ItemID: itemRedPotion, Count: n, Each: each, Why: "potions"}
		}
	}
	// huntfit.go: an elemental arrow that fits the hunting map, from the market (no NPC shop sells one on this server)
	if w := b.fitArrowWant(); w != nil {
		return w
	}
	p := b.plan()
	if p == nil || lv < minGearLevel() || !b.invReady() {
		return nil
	}
	budget := b.Zeny - b.gearReserve()
	// buildgear.go nextBuildPiece (economy run 7, owner: "each bot should prioritize farming or trading to perfect its
	// build"): the build's next piece comes first when only players have it (drop / trade). Asked for when someone
	// offers it, and now and then anyway (the want is demand the market and farm.go see).
	if pc := b.nextBuildPiece(); pc != nil && pc.NPC == 0 && (pc.Source == "trade" || pc.Source == "drop") {
		if bid := bidPrice(b.P, pc.ItemID); bid > 0 && bid <= budget && (onBoard(pc.ItemID) || cfloat() < 0.2) {
			return &Want{ItemID: pc.ItemID, Count: 1, Each: bid, Why: "for my build"}
		}
	}
	var best *Want
	bestGain := 0
	for _, route := range [][]int{p.Weapons, p.Armors, p.Extras} {
		for _, id := range route {
			it := items[id]
			if it == nil || !b.canEquip(it) || b.gearFailed[id] || b.ownsUnequipped(id) {
				continue
			}
			slot := gearSlot(it.Loc)
			if slot == 0x20 && b.equipped[0x2] != nil && items[b.equipped[0x2].ItemID] != nil && items[b.equipped[0x2].ItemID].Loc == 0x22 {
				continue // shield with a two-handed weapon
			}
			if cur := b.equipped[slot]; cur != nil && cur.ItemID == id {
				continue
			}
			gain := b.itemScore(it) - b.itemScore(b.curIn(slot))
			if gain <= bestGain {
				continue
			}
			bid := bidPrice(b.P, id)
			if bid <= 0 || bid > budget {
				continue
			}
			why := "drop-only"
			if len(gearShops(id)) > 0 {
				if b.gearShopNear(id) != nil {
					// an NPC nearby sells it: only worth asking players for it below the NPC price, now and then
					if cfloat() > 0.25 || it.Buy <= 0 || !onBoard(id) {
						continue // 2026-11-14: 95% of wants were these, for gear nobody held: ask only when someone offers it
					}
					bid, why = roundPrice(it.Buy*9/10), "cheaper than NPC"
					la := boardLowAsk(id)
					if la > bid && la <= it.Buy*97/100 {
						// batch 14: still cheaper than the NPC - bid the lowest ask (346 "Adventurer's Suit: price too
						// high" in 10 min: every want bid 900 at 90%, the sellers asked 950)
						bid = la
					}
					if la <= 0 || la > bid || bid > budget {
						continue // batch 9: 49 Sandals wants sat on the board, every offer above 90% of the NPC price
					}
				} else {
					why = "shop is far"
				}
			}
			best, bestGain = &Want{ItemID: id, Count: 1, Each: bid, Why: why}, gain
		}
	}
	// buildgear.go (batch 21 step 3): the build table's other suitable items (the guide's picks, drop-only gear) when
	// a player or a shop offers one - wants from what the build should wear, not only the NPC routes
	if gb := gearBandFor(p.ID, lv); gb != nil {
		for _, list := range gb.Slots {
			for _, e := range list {
				it := items[e.ID]
				if it == nil || e.Source == "plan" || !b.canEquip(it) || b.gearFailed[e.ID] || b.ownsUnequipped(e.ID) ||
					len(gearShops(e.ID)) > 0 || !onBoard(e.ID) {
					continue
				}
				slot := gearSlot(it.Loc)
				if cur := b.equipped[slot]; cur != nil && cur.ItemID == e.ID {
					continue
				}
				gain := b.itemScore(it) - b.itemScore(b.curIn(slot))
				if gain <= bestGain {
					continue
				}
				bid := bidPrice(b.P, e.ID)
				if bid <= 0 || bid > budget {
					continue
				}
				best, bestGain = &Want{ItemID: e.ID, Count: 1, Each: bid, Why: "for my build"}, gain
			}
		}
	}
	if best != nil && best.Why != "cheaper than NPC" {
		return best
	}
	// cards.go: what the field actually has: a weapon/armor for a bot without one, a card for a free slot
	if w := b.bareWant(b.Zeny - b.gearReserve()/2); w != nil {
		return w
	}
	if w := b.cardWant((b.Zeny - b.gearReserve()) * 2 / 3); w != nil {
		return w
	}
	if w := b.slotWant((b.Zeny - b.gearReserve()) / 2); w != nil {
		return w
	}
	return best
}

// keepsGear: gear this bot will wear (better than what it has) or soon needs on its plan.
func (b *Bot) keepsGear(x *Item) bool {
	if x.ID == b.equipWanted {
		return true
	}
	if b.canEquip(x) && b.itemScore(x) > b.itemScore(b.curIn(gearSlot(x.Loc))) {
		return true
	}
	if p := b.plan(); p != nil && x.MinLv > b.BaseLvOr() && x.MinLv <= b.BaseLvOr()+15 {
		for _, route := range [][]int{p.Weapons, p.Armors, p.Extras} {
			for _, id := range route {
				if id == x.ID {
					return true
				}
			}
		}
	}
	return false
}

type holding struct {
	ItemID, Count, Ask int
}

// econSpare: what this bot would sell to another player (b.mu held). Cards, valuable materials, gear it won't
// wear, and potions far beyond what it carries normally. Never equipped items.
func (b *Bot) econSpare() []holding {
	var out []holding
	for _, it := range b.inv {
		if it.Equipped || it.Count <= 0 {
			continue
		}
		x := items[it.ItemID]
		if x == nil {
			continue
		}
		n := 0
		switch {
		case x.Type == "Card":
			n = it.Count
			if b.keepsCard(x.ID) { // cards.go: goes into its own gear
				n--
			}
		case valuableEtc[x.ID]:
			n = it.Count
		case x.Type == "Weapon" || x.Type == "Armor":
			// junkgear.go: plain gear under 100z goes to the NPC, not to #trade (run 7: 674 Knives, 130 Swords, 37
			// Clubs were offered at 9-90z, flooding the board and the markdown log with junk nobody wants)
			if !b.keepsGear(x) && !it.Unidentified && !b.junkGear(it, x) { // identify.go: nobody can wear an unidentified drop
				n = 1
			}
		case x.ID == itemRedPotion && it.Count > 40:
			n = it.Count - 30
		}
		n -= b.refineKeep(x.ID) // refine.go: the material its own weapon still needs is not for sale
		if n > 0 && boughtRecently(b, x.ID) {
			n = 0 // bought within the hour: not resold (Phracon went back and forth between two bots, 06:10)
		}
		if n > 0 {
			out = append(out, holding{ItemID: x.ID, Count: n, Ask: askPrice(b.P, x.ID)})
		}
	}
	return append(out, b.alchemySpare()...) // alchemy.go: herbs for the alchemists, an alchemist's brewed potions
}

// ---------------------------------------------------------------- manager

type econSnap struct {
	b              *Bot
	name, mapN     string
	x, y, lv, zeny int
	free           bool
	sameMapOnly    bool // in a party: never leaves it for a trade
	crossOK        bool
	typing         string
	spare          []holding
	want           *Want
	wantChecked    bool
	shop           []vendListing // vendbuy.go: an open vending shop's listing
	guild          string        // alchemy.go guildOfAccount
	leaderParty    bool          // in a guild leader's party: served first by its guild alchemist
	brewer         bool          // an Alchemist who brews (its potions go to the guild first)
	needGuide      bool          // alchemy.go: the guild alchemist, close to Alchemist, saving for the 100k guide
	fundTo         int           // alchemy.go guildFund: zeny the guild tops it up to (the guide, or a brewer's supply money)
	idle           bool          // free for a zeny gift (vendor role allowed while not vending)
	stocker        bool          // vendbuy.go: a vendor (not vending, cart nearly empty) buying stock for its shop
	courier        bool          // vendbuy.go: a vending vendor that would close its shop to deliver a listed item
	keeper         bool          // guildstock.go: a guild vendor - its shop is the guild's stock
	potShort       bool          // guildstock.go: under its survival minimum of HP potions
	nextPiece      int           // guildstock.go: its next build piece (drop / trade), filled when the stock round is due
	soloScore      int           // guildstock.go: solo survivability (DS Hunters first)
	aid            uint32        // shopcheck.go: the vendor's account id and when its shop opened
	openedAt       time.Time
	pecoShort      int  // peco.go: zeny this rider still lacks for its Peco Peco (0 = none)
	pecoNeed       bool // peco.go: a rider without its Peco (its money is not given away)
	katarShort     int  // coreweapon.go / peco.go katarFundRound: zeny an Assassin still lacks for its katar
	coreNeed       bool // coreweapon.go: saving for its core weapon (its money is not given away)
	gearShort      int  // guildstock.go traderFundRound: zeny its party gear turn still lacks (0 = none)
	gift           int  // gearpool.go giftableZeny: what it may give to any pool (own savings, gifts, fare kept)
}

func econManager(bots []*Bot, stop <-chan struct{}) {
	loadEconCounters() // econpersist.go (run 10): counters survive deploys
	t := time.NewTicker(econTick)
	defer t.Stop()
	nextSave := time.Now().Add(time.Minute)
	for {
		select {
		case <-stop:
			saveEconCounters(time.Now())
			return
		case <-t.C:
		}
		econRound(bots, time.Now())
		if now := time.Now(); now.After(nextSave) {
			nextSave = now.Add(time.Minute)
			saveEconCounters(now)
		}
	}
}

func econRound(bots []*Bot, now time.Time) {
	econ.mu.Lock()
	due := map[string]bool{}
	for k, v := range econ.nextWant {
		if now.After(v) {
			due[k] = true
		}
	}
	econ.mu.Unlock()

	// 1. snapshot every online bot (one lock at a time)
	var snaps []*econSnap
	stockDue := guildStockDue(now) // guildstock.go: fill nextPiece this round
	for _, b := range bots {
		p := partyOf(b) // parties lock first, never while holding b.mu
		leaderParty := p != nil && p.Leader != nil && p.Leader.leaderVision() != nil
		guild := guildOfAccount(b)
		b.mu.Lock()
		if !b.Online || b.MapName == "" {
			b.mu.Unlock()
			continue
		}
		b.mailThink(now)          // mail.go: a mail the server never answered
		b.parcelThink(guild, now) // parcel.go: surplus gear mailed to a vendor for the south gate shops
		busy := b.vendBuying() || b.mailBusy()
		if b.mkt != nil {
			if c, _, _ := b.mkt.d.state(); !c {
				busy = true
			}
		}
		name := b.charName()
		s := &econSnap{b: b, name: name, mapN: b.MapName, x: b.X, y: b.Y, lv: b.BaseLvOr(), zeny: b.Zeny,
			typing: b.P.Typing, guild: guild, leaderParty: leaderParty, brewer: b.canBrew(), needGuide: b.needsGuide() || b.brewerBroke(), fundTo: b.fundTarget()}
		travelling := b.Goal.Map != "" && b.Goal.Map != b.MapName
		s.free = !busy && !b.Dead && b.trip == nil && b.kafra == nil && b.vend == nil && b.tradeFrom == "" &&
			b.charRole() != "vendor" && b.Goal.Set != "api" && !travelling && b.basicLv() >= 1 && b.saleRoom == ""
		// alchemy.go guildFund: a vendor-role alchemist that isn't vending can still receive the guild's zeny
		s.idle = s.free || !busy && !b.Dead && b.trip == nil && b.kafra == nil && b.tradeFrom == "" && b.Goal.Set != "api" &&
			!travelling && b.saleRoom == "" && (b.vend == nil || !b.vend.Opened)
		s.sameMapOnly = p != nil || b.pin != nil
		s.keeper = b.charRole() == "vendor" || b.isHumanGuildTrader(now) // humanguildtrader.go: Starveil's trader
		s.potShort = !b.isHealer() && b.BaseLvOr() >= 20 && b.realPotCount() < b.potMin()
		s.soloScore = b.soloScore()
		s.pecoShort, s.pecoNeed = b.pecoShortfall(), b.pecoNeed()
		s.coreNeed = b.coreSaving()
		if s.coreNeed {
			s.katarShort = b.katarShortfall()
		}
		s.gift = b.giftableZeny()
		if g := b.gearNext; g != nil && g.Turn {
			// gearpool.go: "gear turn: party saving (Xz of Yz)" - the party raises X of the turn item's Y
			var x, y int
			if n, _ := fmt.Sscanf(g.Status, "gear turn: party saving (%dz of %dz)", &x, &y); n == 2 && y > x {
				s.gearShort = y - x + 300 // + a little for its potion reserve (gearpool keep)
			}
		}
		if stockDue {
			if pc := b.nextBuildPiece(); pc != nil && pc.Source != "shop" {
				s.nextPiece = pc.ItemID
			}
		}
		s.crossOK = !s.sameMapOnly && s.lv >= 15 && b.Zeny >= 1000
		if b.charRole() != "vendor" {
			s.spare = b.lootFilter(b.econSpare(), p == nil && guild == "") // lootflow.go: party -> guild -> stock first
		}
		s.shop = b.shopListing()
		if len(s.shop) > 0 {
			s.aid, s.openedAt = b.aid, b.vend.OpenedAt
		}
		if !busy && b.courierReady(now) { // vendbuy.go: a vendor delivers from its shop to a party member (batch 11)
			s.courier, s.spare = true, b.courierSpare()
		}
		if s.free {
			b.cardThink(now) // cards.go
		}
		// vendbuy.go stocker: a vendor whose cart is (nearly) empty buys spare cards/materials/gear from field bots
		// to stock its shop (2026-11-14: shops listed 1 card each, vendors farmed for stock themselves)
		s.stocker = !s.free && s.idle && b.charRole() == "vendor" && b.stockerReady()
		if (s.free || s.stocker) && (due[name] || !econKnown(name)) {
			if s.stocker {
				s.want, s.wantChecked = b.stockWant(), true
			} else {
				s.want, s.wantChecked = b.econWantAll(leaderParty), true // alchemy.go: herbs / SP potions, then econWant
			}
			if s.want != nil {
				s.want.Standard = priceOf(s.want.ItemID)
			}
		}
		b.mu.Unlock()
		snaps = append(snaps, s)
	}

	lootFlowRound(snaps, now) // lootflow.go (run 10): interesting loot goes to party / guild / guild stock before a sale
	exchangePrepare(snaps, now) // exchange.go: party / guild / friend relations and spares (exchange_tiers_on)

	// 2. update the boards
	online := map[string]*econSnap{}
	for _, s := range snaps {
		online[s.name] = s
	}
	agg := map[int]*offerRow{}
	for _, s := range snaps {
		for _, h := range s.spare {
			r := agg[h.ItemID]
			if r == nil {
				r = &offerRow{ItemID: h.ItemID, Item: itemName(h.ItemID), LowAsk: h.Ask, Standard: priceOf(h.ItemID)}
				agg[h.ItemID] = r
			}
			r.Sellers++
			r.Qty += h.Count
			r.LowAsk = min(r.LowAsk, h.Ask)
		}
	}
	// open vending shops are on the board too (players browse the south gate): wants for a card or a weapon can be
	// served by a shop (step 4), which bots never asked for while only in-person spares were listed
	for _, s := range snaps {
		for _, l := range s.shop {
			r := agg[l.ItemID]
			if r == nil {
				r = &offerRow{ItemID: l.ItemID, Item: itemName(l.ItemID), LowAsk: l.Price, Standard: priceOf(l.ItemID)}
				agg[l.ItemID] = r
			}
			r.Sellers++
			r.Qty += l.Count
			r.LowAsk = min(r.LowAsk, l.Price)
		}
	}
	offers := make([]offerRow, 0, len(agg))
	for _, r := range agg {
		offers = append(offers, *r)
	}
	sort.Slice(offers, func(i, j int) bool { return offers[i].Standard*offers[i].Qty > offers[j].Standard*offers[j].Qty })

	type post struct {
		b    *Bot
		text string
	}
	var posts []post
	checkShops(snaps, now) // shopcheck.go: open shops vs the server's shop signs
	var shopRefs []shopRef
	for _, s := range snaps {
		if len(s.shop) > 0 {
			shopRefs = append(shopRefs, shopRef{vendor: s.b, name: s.name, mapN: s.mapN, x: s.x, y: s.y, items: s.shop})
		}
	}
	econ.mu.Lock()
	econ.offers = offers
	econ.shops = shopRefs
	for _, s := range snaps {
		if !s.wantChecked {
			continue
		}
		econ.nextWant[s.name] = now.Add(time.Duration(120+crand(120)) * time.Second)
		old := econ.wants[s.name]
		if old != nil && old.InDeal {
			continue
		}
		if s.want == nil {
			delete(econ.wants, s.name)
			continue
		}
		if old != nil && old.ItemID == s.want.ItemID {
			old.Each, old.Count, old.Map = s.want.Each, s.want.Count, s.mapN
			continue
		}
		w := s.want
		w.Buyer, w.Name, w.Map, w.Item, w.Created = s.b, s.name, s.mapN, itemName(w.ItemID), now // Standard set outside econ.mu (priceOf locks it)
		econ.wants[s.name] = w
	}
	for n, w := range econ.wants {
		if online[n] == nil || (!w.InDeal && now.Sub(w.Created) > econWantLife) {
			delete(econ.wants, n)
		}
	}
	for k, v := range econ.noPair {
		if now.After(v) {
			delete(econ.noPair, k)
		}
	}
	// #trade lines: wants first (that's how bots ask), then a few offers
	for _, w := range econ.wants {
		s := online[w.Name]
		if exchangeAsk(w, s, now) { // exchange.go: asked in party / guild chat first
			continue
		}
		if s == nil || w.InDeal || now.Sub(econ.posted[w.Name]) < econPostEvery || now.Sub(econ.lastLine) < econLineGap {
			continue
		}
		econ.posted[w.Name], econ.lastLine, w.Posted = now, now, now
		posts = append(posts, post{s.b, styled(s.typing, wantLine(w))})
	}
	if now.Sub(econ.lastLine) >= econLineGap && len(snaps) > 0 {
		for try := 0; try < 8; try++ {
			s := snaps[crand(len(snaps))]
			if len(s.spare) == 0 || now.Sub(econ.posted[s.name]) < econPostEvery {
				continue
			}
			h := s.spare[crand(len(s.spare))]
			if h.Ask*h.Count < 300 {
				continue // "S> Red Herb 18z" isn't worth a line on #trade
			}
			econ.posted[s.name], econ.lastLine = now, now
			posts = append(posts, post{s.b, styled(s.typing, offerLine(h))})
			break
		}
	}
	// drop finished deals
	active := econ.deals[:0]
	for _, d := range econ.deals {
		c, _, opened := d.state()
		if !c && now.After(d.Deadline.Add(time.Minute)) && (opened.IsZero() || now.Sub(opened) > 2*econTradeWindow) {
			d.close("timed out")
			c = true
		}
		if !c {
			if online[d.SellerName] == nil || online[d.BuyerName] == nil {
				d.close("partner went offline")
				c = true
			}
		}
		if c {
			if w := econ.wants[d.BuyerName]; w != nil && w.InDeal {
				w.InDeal = false
				w.Created = now.Add(-econWantLife / 2) // ask again a bit later, maybe someone else has one
			}
			continue
		}
		active = append(active, d)
	}
	econ.deals = active
	// 3. matching: wants x holders
	type cand struct {
		w       *Want
		buyer   *econSnap
		seller  *econSnap
		h       holding
		price   int
		count   int
		meet    bool
		gift    bool
		deliver bool
		fetch   bool
		mail    bool // mailtrade.go: settled by RODEX (far apart, worth 1,000z+)
	}
	var matches []cand
	slots := econMaxDeals - len(econ.deals)
	holders := map[int][]*econSnap{}
	for _, s := range snaps {
		if !s.free {
			continue
		}
		for _, h := range s.spare {
			holders[h.ItemID] = append(holders[h.ItemID], s)
		}
	}
	used := map[string]bool{}
	// a guild leader's party is served first (owner: the guild alchemist's potions go to the leader's party first)
	wantList := make([]*Want, 0, len(econ.wants))
	for _, w := range econ.wants {
		wantList = append(wantList, w)
	}
	sort.SliceStable(wantList, func(i, j int) bool {
		a, b := online[wantList[i].Name], online[wantList[j].Name]
		return a != nil && a.leaderParty && !(b != nil && b.leaderParty)
	})
	for _, w := range wantList {
		if slots <= 0 {
			break
		}
		buyer := online[w.Name]
		if w.InDeal || buyer == nil || !(buyer.free || buyer.stocker) || used[w.Name] {
			continue
		}
		var best *cand
		stage := 0 // how far the best candidate got: 0 no seller, 1 price, 2 distance/party, 3 zeny
		farWhy := ""
		var sellers []*econSnap
		seen := map[*econSnap]bool{}
		for _, id := range append([]int{w.ItemID}, variantsOf(w.ItemID)...) { // cards.go: Knife [4] does for a Knife
			for _, s := range holders[id] {
				if !seen[s] {
					seen[s] = true
					sellers = append(sellers, s)
				}
			}
		}
		for _, s := range sellers {
			if s == buyer || used[s.name] || now.Before(econ.noPair[s.name+"|"+w.Name]) || !exchangeEligible(w, buyer, s, now) {
				continue
			}
			var h holding
			for _, x := range s.spare {
				if wantAccepts(w, x.ItemID) && (x.Count >= w.Count || w.Partial && x.Count > 0) && (h.ItemID == 0 || x.Ask < h.Ask) {
					h = x
				}
			}
			count := min(w.Count, h.Count)
			guildMate := s.guild != "" && s.guild == buyer.guild
			// the guild alchemist gives a few potions to its guild leader's party; guild mates trade at a guild price
			// guildstock.go (run 8): a guild vendor's stock is the guild's - healing items and potions go to a guild mate
			// who asks, as a gift (owner: supplies ready when members need them)
			gift := guildMate && (s.brewer && buyer.leaderParty || s.keeper) && (isHealPotionOrSP(w.ItemID) || healItem(w.ItemID)) &&
				now.After(giftNext[w.Name])
			guildEach := roundPrice(h.Ask * 7 / 10)
			tierEach, tier := exchangeTier(s, buyer, h) // exchange.go: party / kin price ("" = market)
			if h.ItemID == 0 || (!gift && h.Ask > w.Each && !(guildMate && guildEach <= w.Each) && !(tier != "" && tierEach <= w.Each)) {
				stage = max(stage, 1)
				continue
			}
			stage = max(stage, 2)
			same := s.mapN == buyer.mapN
			// meeting in Prontera costs travel (owner: "don't waste money on travel"): only for a worthwhile item,
			// or when one of them is already there (guild mates also meet there for smaller things: herbs, potions)
			inPront := s.mapN == "prontera" || buyer.mapN == "prontera"
			value := w.Each * count
			// delivery: a solo seller travels to a party member's map and hands it over there (the party member never
			// leaves its party; partyTradeOK). 2026-11-14: party members could only trade within 10 cells, so the
			// matcher kept picking far holders and startDeal refused them: 0 trades in 30 min.
			deliver := !same && buyer.sameMapOnly && !s.sameMapOnly && value >= 500 && !mapTooStrong(buyer.mapN, s.lv)
			if deliver {
				hops := mapHops(s.mapN, s.x, s.y, buyer.mapN)
				// owner: no money wasted on travel - a cheap item is only walked over from nearby. Batch 9: a seller
				// short of Kafra money (crossOK needs 1,000z; median purse lv20-39 ~600z) still walks <= 3 maps.
				deliver = s.crossOK && (value >= 3000 || hops <= 3) || s.lv >= 15 && hops <= 3
			}
			if s.courier && !deliver {
				continue // a vendor leaves its shop only to deliver to a party member; others shop at the south gate
			}
			// fetch: the mirror of delivery - a solo buyer (a vendor buying stock, a solo player) travels to a party
			// member's map; the party member keeps playing until the buyer is next to it (economy batch 9)
			fetch := !same && !deliver && s.sameMapOnly && !buyer.sameMapOnly && buyer.crossOK && value >= 500 &&
				(value >= 3000 || mapHops(buyer.mapN, buyer.x, buyer.y, s.mapN) <= 3) && !mapTooStrong(s.mapN, buyer.lv)
			mail := false
			if !same && !deliver && !fetch && !(s.crossOK && buyer.crossOK && (value >= 5000 || inPront && (value >= 1000 || guildMate && value >= 300))) &&
				mailTradeOK(s, buyer, h.ItemID, count, roundPrice((h.Ask+w.Each)/2)*count) {
				mail = true // mailtrade.go: far apart, worth the 100z fee
			}
			if !mail && !same && !deliver && !fetch && !(s.crossOK && buyer.crossOK && (value >= 5000 || inPront && (value >= 1000 || guildMate && value >= 300))) {
				switch {
				case s.sameMapOnly && buyer.sameMapOnly:
					farWhy = "too far: both in parties"
				case s.sameMapOnly || buyer.sameMapOnly:
					farWhy = "too far: party member, partner can't travel (lv/zeny/map/value)"
				default:
					farWhy = "too far: not worth the travel"
				}
				continue
			}
			if same && !partyTradeOK(s, buyer, false) {
				farWhy = "too far: party member on the same map, > 10 cells"
				continue // a party member trades only with bots already next to it
			}
			each := min(roundPrice((h.Ask+w.Each)/2), max(npcWindow(h.ItemID, (h.Ask+w.Each)/2), h.Ask)) // meet in the middle; run 11: under the NPC price
			if guildMate {
				each = min(each, guildEach)
			}
			if tier != "" {
				each = min(each, tierEach)
			}
			if gift {
				count, each = min(count, 5), 0
			}
			c := &cand{w: w, buyer: buyer, seller: s, h: h, price: each * count, count: count, meet: !same && !deliver && !fetch && !mail,
				gift: gift, deliver: deliver, fetch: fetch, mail: mail}
			stage = 3
			bestMate := best != nil && best.seller.guild != "" && best.seller.guild == buyer.guild
			if best == nil || best.mail && !c.mail || c.mail && !best.mail && best != nil {
				if best == nil || best.mail { // in person beats a mail sale (no fee); among mail sales the rules below
					best = c
				}
				continue
			}
			if r := exchangePrefer(buyer, s, h.ItemID, best.seller, best.h.ItemID); r != 0 { // exchange.go: party, then kin
				if r > 0 {
					best = c
				}
				continue
			}
			if (best.meet && same) || best.meet == c.meet && guildMate && !bestMate ||
				(best.meet == c.meet && guildMate == bestMate && dist(s.x, s.y, buyer.x, buyer.y) < dist(best.seller.x, best.seller.y, buyer.x, buyer.y)) {
				best = c // batch 21: guild mates first (owner: the guild gears its members), then the nearest
			}
		}
		if best == nil || best.price > buyer.zeny {
			econMiss([]string{"no seller", "price too high", "too far / party", "buyer short of zeny"}[stage])
			econMissItem(itemName(w.ItemID) + ": " + []string{"no seller", "price too high", "too far / party", "buyer short of zeny"}[stage])
			if stage == 2 && farWhy != "" {
				econMiss(farWhy) // which distance rule stopped it (batch 9)
			}
		}
		if best != nil && best.price <= buyer.zeny {
			if best.gift {
				giftNext[w.Name] = now.Add(30 * time.Minute)
			}
			used[best.seller.name], used[w.Name] = true, true
			w.InDeal = true
			matches = append(matches, *best)
			exchangeChosen(best.seller, best.buyer, best.h.ItemID, best.count, best.price, now) // exchange.go
			slots--
		}
	}
	// 4. what nobody offered in person may be on sale in a vending shop at the south gate (vendbuy.go)
	type shopMatch struct {
		w      *Want
		buyer  *econSnap
		vendor *econSnap
		itemID int
		price  int
		count  int
	}
	var shopBuys []shopMatch
	for _, w := range wantList {
		buyer := online[w.Name]
		if w.InDeal || buyer == nil || !buyer.free || used[w.Name] {
			continue
		}
		var best *shopMatch
		for _, v := range snaps {
			if v == buyer || len(v.shop) == 0 {
				continue
			}
			for _, l := range v.shop {
				if !wantAccepts(w, l.ItemID) || l.Price > w.Each || l.Price <= 0 || (l.Count < w.Count && !w.Partial) {
					continue
				}
				n := min(w.Count, l.Count)
				// a shopping trip to Prontera only for something worth it, or when already there
				if buyer.mapN != v.mapN && !(buyer.crossOK && l.Price*n >= 1000) {
					continue
				}
				if buyer.sameMapOnly && dist(buyer.x, buyer.y, v.x, v.y) > partyTradeNear {
					continue // a party member never walks off to a shop for a trade
				}
				if best == nil || l.Price < best.price {
					best = &shopMatch{w: w, buyer: buyer, vendor: v, itemID: l.ItemID, price: l.Price, count: n}
				}
			}
		}
		if best != nil && best.price*best.count <= buyer.zeny {
			used[w.Name] = true
			delete(econ.wants, w.Name)
			econ.nextWant[w.Name] = now.Add(5 * time.Minute)
			shopBuys = append(shopBuys, *best)
		}
	}
	econ.mu.Unlock()

	for _, m := range shopBuys {
		startVendBuy(m.buyer.b, m.buyer.name, m.vendor.b, m.vendor.name, m.itemID, m.count, m.price, now)
	}
	for _, p := range posts {
		go exchangeTradePost(p.b, p.text) // exchange.go: channelSay("#trade"), or a chat job (chat_jobs_on)
	}
	shopAnnounceRound(snaps, now) // shopannounce.go (EC35): vendors tell #trade about their shop's rare items
	pecoFundRound(snaps, now)   // peco.go: guild / party mates pay a rider's Peco Peco first
	katarFundRound(snaps, now)  // peco.go: ... and part of an Assassin's katar
	guildFund(snaps, now)       // alchemy.go
	marketTick(now)             // market.go: supply and demand factors, markdown resets
	guildPoolRound(snaps, now)  // guildpool.go: guilds pool zeny for boss-only items
	guildStockRound(snaps, now) // guildstock.go: guild stock vs member needs, guild farmers
	traderFundRound(snaps, now) // guildstock.go: the guild trader pays a member's stuck gear turn
	exchangeRound(snaps, now)   // chatintent.go: humans' needs / offers registered by the chat agent
	humanWantRound(snaps, now)  // humanwant.go: a human's B> on #trade gets an offer
	for _, m := range matches {
		if m.mail {
			if !startMailTrade(m.seller, m.buyer, m.w, m.h.ItemID, m.count, m.price, now) {
				econMiss("partner busy / not safe at start")
				econ.mu.Lock()
				if w := econ.wants[m.w.Name]; w == m.w {
					w.InDeal = false
				}
				econ.mu.Unlock()
			}
			continue
		}
		if !startDeal(m.seller, m.buyer, m.h.ItemID, m.count, m.price, m.meet, m.deliver, m.fetch, now) {
			econMiss("partner busy / not safe at start")
			econ.mu.Lock()
			if w := econ.wants[m.w.Name]; w == m.w {
				w.InDeal = false
			}
			econ.mu.Unlock()
		}
	}
}

// wantAccepts: the wanted item or a variant at least as good (cards.go variantsOf)
func wantAccepts(w *Want, id int) bool {
	if id == w.ItemID {
		return true
	}
	for _, v := range variantsOf(w.ItemID) {
		if v == id {
			return true
		}
	}
	return false
}

// econKnown: whether the bot's needs were checked before (new bots get checked on their first round).
func econKnown(name string) bool {
	econ.mu.Lock()
	defer econ.mu.Unlock()
	_, ok := econ.nextWant[name]
	return ok
}

func wantLine(w *Want) string {
	if w.Count > 1 {
		return pick(fmt.Sprintf("B> %dx %s %s ea", w.Count, w.Item, fmtZ(w.Each)), fmt.Sprintf("buying %s x%d, %s each", w.Item, w.Count, fmtZ(w.Each)))
	}
	return pick(fmt.Sprintf("B> %s %s", w.Item, fmtZ(w.Each)), fmt.Sprintf("buying %s for %s, pm", w.Item, fmtZ(w.Each)),
		fmt.Sprintf("B> %s, paying %s", w.Item, fmtZ(w.Each)), fmt.Sprintf("anyone has %s? %s", w.Item, fmtZ(w.Each)))
}

func offerLine(h holding) string {
	n := itemName(h.ItemID)
	if who := buildsWanting(h.ItemID); who != "" && cfloat() < 0.6 { // buildgear.go (batch 21): advertise to the builds
		n += pick(" (gd for "+who+")", " - "+who+" want this", " ["+who+"]")
	}
	if h.Count > 1 {
		return pick(fmt.Sprintf("S> %s %s ea (%d)", n, fmtZ(h.Ask), h.Count), fmt.Sprintf("selling %s x%d, %s each pm", n, h.Count, fmtZ(h.Ask)))
	}
	return pick(fmt.Sprintf("S> %s %s", n, fmtZ(h.Ask)), fmt.Sprintf("selling %s, %s pm", n, fmtZ(h.Ask)), fmt.Sprintf("S> %s, %s obo", n, fmtZ(h.Ask)))
}

// prontMeet: a free spot at Prontera's south gate, off the road (x 152-160 is kept clear for walking)
func prontMeet() (int, int) {
	gm := maps["prontera"]
	for i := 0; i < 40; i++ {
		x := 140 + crand(11)
		if crand(2) == 0 {
			x = 162 + crand(9)
		}
		y := 55 + crand(18)
		if gm == nil || gm.Walkable(x, y) {
			return x, y
		}
	}
	return 156, 60
}

// partyTradeNear: how close (cells) a party member's trade partner must already stand. Lesson (2026-11-14): a gear
// trade pulled Echobunny out of Ayla's party in the cave to Payon to sell its suit; a trade never makes a party member
// walk away from its party or out of a dungeon.
const partyTradeNear = 10

// partyTradeOK: a deal involving a party member (sameMapOnly) only between two bots already within partyTradeNear.
func partyTradeOK(a, b *econSnap, meet bool) bool {
	if !a.sameMapOnly && !b.sameMapOnly {
		return true
	}
	if !meet && a.mapN == b.mapN && townOf(a.mapN) == a.mapN {
		return true // batch 9: inside a town (the party's town run) nothing can hit them: any distance on the map
	}
	return !meet && a.mapN == b.mapN && dist(a.x, a.y, b.x, b.y) <= partyTradeNear
}

// startDeal hands the deal to both bots (one lock at a time); false if either became busy meanwhile.
func startDeal(seller, buyer *econSnap, itemID, count, price int, meet, deliver, fetch bool, now time.Time) bool {
	if !deliver && !fetch && !partyTradeOK(seller, buyer, meet) {
		return false
	}
	econ.mu.Lock()
	econ.nextID++
	id := econ.nextID
	econ.mu.Unlock()
	d := &Deal{ID: id, Seller: seller.b, Buyer: buyer.b, SellerName: seller.name, BuyerName: buyer.name, ItemID: itemID,
		Count: count, Price: price, Created: now, Deadline: now.Add(4 * time.Minute)}
	if meet {
		d.MeetMap = "prontera"
		d.MeetX, d.MeetY = prontMeet()
		d.Deadline = now.Add(10 * time.Minute)
	}
	if deliver { // the seller comes to the buyer's map; the buyer keeps playing with its party
		d.MeetMap, d.MeetX, d.MeetY, d.Deliver = buyer.mapN, buyer.x, buyer.y, true
		d.Deadline = now.Add(10 * time.Minute)
	}
	if fetch { // the buyer goes to the seller's map (wherever its party is); MeetMap stays "" (marketThink follows the seller)
		d.Fetch = true
		d.Deadline = now.Add(10 * time.Minute)
	}
	switch {
	case price == 0 && itemID != 0:
		d.Kind = "gift"
	case fetch:
		d.Kind = "fetch"
	case deliver:
		d.Kind = "deliver"
	case meet:
		d.Kind = "meet"
	case townOf(seller.mapN) == seller.mapN:
		d.Kind = "town"
	default:
		d.Kind = "same"
	}
	if buyer.stocker {
		d.Kind = "stock-" + d.Kind
	}
	if seller.courier && deliver {
		d.Courier, d.Kind = true, "courier"
	}
	take := func(s *econSnap, isSeller bool) bool {
		b := s.b
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.Online || b.charName() != s.name || b.mkt != nil && !func() bool { c, _, _ := b.mkt.d.state(); return c }() ||
			b.trip != nil || b.kafra != nil || b.tradeFrom != "" || b.Dead || !b.tradeSafe(now) {
			return false // tradeSafe (gearpower.go): never start a deal while hit or under 70% HP
		}
		if isSeller && d.Courier {
			if !b.courierTake(itemID, count, now) { // vendbuy.go: close the shop, the item from the cart to the bag
				return false
			}
		} else if isSeller {
			ok := false
			for _, it := range b.inv {
				if it.ItemID == itemID && !it.Equipped && it.Count >= count {
					ok = true
				}
			}
			if !ok {
				return false
			}
		} else if b.Zeny < price {
			return false
		}
		if b.mkt != nil {
			b.endMarket(now) // a closed deal not yet cleaned up
		}
		partner := buyer.name
		if !isSeller {
			partner = seller.name
		}
		b.mkt = &marketSide{d: d, seller: isSeller, me: s.name, partner: partner}
		b.target, b.path = 0, nil
		return true
	}
	if !take(seller, true) {
		return false
	}
	if !take(buyer, false) {
		seller.b.mu.Lock()
		if seller.b.mkt != nil && seller.b.mkt.d == d {
			seller.b.mkt = nil
		}
		seller.b.mu.Unlock()
		return false
	}
	econ.mu.Lock()
	econ.deals = append(econ.deals, d)
	econ.mu.Unlock()
	econCount("deal started: " + d.Kind)
	// the diary says how they meet (2026-11-14: fetch/delivery deals were all written "same map", and the life
	// mind retold a cross-map deal as a same-map sale)
	sWhere, bWhere := "same map", "same map"
	switch {
	case meet:
		sWhere = fmt.Sprintf("meeting at prontera %d,%d", d.MeetX, d.MeetY)
		bWhere = sWhere
	case deliver:
		sWhere, bWhere = "I bring it to "+buyer.mapN, "they bring it to me on "+buyer.mapN
	case fetch:
		sWhere, bWhere = "they come to me on "+seller.mapN, "I go to "+seller.mapN+" to get it"
	}
	item := itemName(itemID)
	if count > 1 {
		item = fmt.Sprintf("%dx %s", count, item)
	}
	seller.b.event("info", buyer.name, fmt.Sprintf("agreed to sell %s to %s for %dz (%s)", item, buyer.name, price, sWhere))
	buyer.b.event("info", seller.name, fmt.Sprintf("agreed to buy %s from %s for %dz (%s)", item, seller.name, price, bWhere))
	// the conversation, in character
	go func() {
		if exchangeDealTalk(seller, buyer, itemID, count, price, meet, deliver, fetch) { // exchange.go: tier deal lines
			return
		}
		st, bt := seller.typing, buyer.typing
		if price == 0 && itemID != 0 { // the guild alchemist hands potions to its guild leader's party
			seller.b.whisper(buyer.name, styled(st, pick(fmt.Sprintf("got %s for u, on the guild", item),
				fmt.Sprintf("here, %s from the guild stock", item), fmt.Sprintf("brewed some, take %s", item))))
		} else {
			seller.b.whisper(buyer.name, styled(st, pick(fmt.Sprintf("i have %s, %s?", item, fmtZ(price)),
				fmt.Sprintf("got %s for u, %s ok?", item, fmtZ(price)), fmt.Sprintf("saw ur B>, %s for %s", item, fmtZ(price)))))
		}
		time.Sleep(time.Duration(3+crand(4)) * time.Second)
		if meet {
			buyer.b.whisper(seller.name, styled(bt, pick("deal! meet me at pront south gate", "ok, pront south gate?", "sure, im heading to prontera south gate")))
		} else if deliver {
			buyer.b.whisper(seller.name, styled(bt, pick("deal! im on "+buyer.mapN+", bring it pls", "ok, im at "+buyer.mapN, "sure, find me on "+buyer.mapN)))
		} else if fetch {
			buyer.b.whisper(seller.name, styled(bt, pick("deal, omw to "+seller.mapN, "ok coming to u", "sure, ill come to "+seller.mapN)))
		} else {
			buyer.b.whisper(seller.name, styled(bt, pick("deal, im here", "ok come trade", "sure! same map, come")))
		}
	}()
	return true
}

// ---------------------------------------------------------------- bot side (b.mu held)

// marketThink runs the bot's part of a deal: travel to the meeting point, walk up to the partner, request the trade.
// Returns true while it owns the bot.
func (b *Bot) marketThink(gm *GameMap, moving bool, now time.Time) bool {
	s := b.mkt
	if s == nil {
		return false
	}
	d := s.d
	closed, _, opened := d.state()
	// while the window is open the server has the last word (0x00f0 done / 0x00ee cancelled close it on both
	// sides); ending early here could miss our own "trade done" packet
	if !s.open && (closed || b.charName() != s.me) {
		b.endMarket(now)
		return false
	}
	if s.open {
		if now.Sub(opened) > econTradeWindow {
			d.close("trade window timed out")
			b.endMarket(now)
			return false
		}
		b.setState("trading with " + s.partner)
		return true
	}
	if now.After(d.Deadline) {
		d.close("could not meet in time")
		b.endMarket(now)
		return false
	}
	partner := d.Buyer
	if !s.seller {
		partner = d.Seller
	}
	ps, ok := peek(partner.P.Name)
	if !ok || !ps.Online || ps.Name != s.partner {
		d.close("partner went offline")
		b.endMarket(now)
		return false
	}
	// a face-to-face deal with a party member in the field never holds the party up: the partner must stay within
	// partyTradeNear cells, and the whole hand-off gets 30 s, then it is dropped (the market retries later).
	// Echobunny stood "waiting for Zendancer84 to trade" for minutes while the priest fought 34 cells away.
	if d.MeetMap == "" && !d.Fetch && !d.Deliver && partyOf(b) != nil {
		// run 7: 28 of 40 failed deals (15:17-15:45) were "party mate too far": matched within 10 cells, then one
		// side stepped past 10 while they walked together (the party moves). Keep the deal while the partner is with
		// the party (20 cells, RULES.md 3 "together"); in a town any distance on the map, as partyTradeOK matched it.
		inTown := townOf(b.MapName) == b.MapName
		if pp := partyOf(partner); pp == nil && ps.Map == b.MapName {
			// economy run 10: 16 of 26 failed deals (19:57-20:10) were "party mate too far" - 19 same-map deals failed
			// vs 12 done: the party member walked to a solo partner while its party moved on, or the solo partner
			// waited while the party member was carried off by its party. Now the solo partner walks over and the
			// party member keeps playing with its party until the partner is next to it (2 min for the hand-off).
			if now.Sub(d.Created) > 2*time.Minute {
				d.close("party trade took too long (solo partner)")
				b.endMarket(now)
				return false
			}
			if dist(b.X, b.Y, ps.X, ps.Y) > 3 {
				return false // keep playing; the partner comes to us
			}
		} else {
			if ps.Map != b.MapName || !inTown && !partyTogether(b.MapName, b.X, b.Y, ps.Map, ps.X, ps.Y) { // together.go
				d.close("party mate too far for a trade")
				b.endMarket(now)
				return false
			}
			if now.Sub(d.Created) > 30*time.Second && !(inTown && now.Sub(d.Created) <= 90*time.Second) {
				d.close("party trade took too long")
				b.endMarket(now)
				return false
			}
		}
	}
	// delivery buyer: keep playing with the party until the seller is close on our map
	if d.Deliver && !s.seller && (ps.Map != b.MapName || dist(b.X, b.Y, ps.X, ps.Y) > 8) {
		return false
	}
	// fetch seller (a party member): the same - it never travels for the deal; the buyer comes to it (batch 9)
	if d.Fetch && s.seller && (ps.Map != b.MapName || dist(b.X, b.Y, ps.X, ps.Y) > 8) {
		return false
	}
	// fight back if jumped on the way
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
	// fetch buyer: follow the seller to wherever its party is, walk up to it; the seller requests the trade
	if d.Fetch && !s.seller {
		if b.MapName != ps.Map {
			if !s.goalSet {
				s.savedGoal, s.goalSet = b.Goal, true
			}
			if b.Goal.Map != ps.Map || b.Goal.Set != "market" {
				b.Goal, b.route, b.path = Goal{Mode: "idle", Map: ps.Map, Set: "market"}, nil, nil
			}
			b.travel(gm, moving)
			if b.Goal.Map != ps.Map {
				d.close("no route to " + ps.Map)
				b.endMarket(now)
				return false
			}
			b.setState(fmt.Sprintf("going to %s to buy from %s", ps.Map, s.partner))
			return true
		}
		if gm == nil {
			return true
		}
		if dist(b.X, b.Y, ps.X, ps.Y) > 2 {
			if !b.sameWalkRegion(gm, ps.X, ps.Y) {
				d.close("partner unreachable (other walk region)")
				b.endMarket(now)
				return false
			}
			if !moving {
				b.stepAlong(gm, ps.X, ps.Y)
			}
			b.setState("going to trade with " + s.partner)
			return true
		}
		b.setState("waiting for " + s.partner + " to trade")
		return true
	}
	// economy run 10 (life mind 19:10: Shadow_fire agreed "I bring it to prontera", the buyer's party moved on to geffen
	// / mjolnir and the deal fell through): a delivering seller follows the buyer's party to its new map, up to twice
	// and only where it is safe for the seller; otherwise it says it can't come and the deal is called off.
	if d.Deliver && s.seller && ps.Map != "" && d.MeetMap != "" && ps.Map != d.MeetMap {
		typing, to := b.P.Typing, s.partner
		if d.moves >= 2 || mapTooStrong(ps.Map, b.BaseLvOr()) {
			go b.whisper(to, styled(typing, pick("cant get to "+ps.Map+", sry - lets do it another time",
				"u moved to "+ps.Map+", too dangerous for me. cancel, sry", "no way i can reach "+ps.Map+" sry, deal off")))
			d.close("buyer moved out of reach")
			b.endMarket(now)
			return false
		}
		d.mu.Lock()
		d.MeetMap, d.MeetX, d.MeetY = ps.Map, ps.X, ps.Y
		d.moves++
		d.Deadline = d.Deadline.Add(5 * time.Minute)
		d.mu.Unlock()
		b.Goal, b.route, b.path = Goal{Mode: "idle", Map: ps.Map, Set: "market"}, nil, nil
		go b.whisper(to, styled(typing, pick("ok omw to "+ps.Map, "saw u moved, coming to "+ps.Map, "np, ill bring it to "+ps.Map)))
	}
	// travel to the meeting town
	if d.MeetMap != "" && b.MapName != d.MeetMap {
		if !s.goalSet {
			s.savedGoal, s.goalSet = b.Goal, true
		}
		if b.Goal.Map != d.MeetMap || b.Goal.Set != "market" {
			b.Goal, b.route, b.path = Goal{Mode: "idle", Map: d.MeetMap, Set: "market"}, nil, nil
		}
		b.travel(gm, moving)
		if b.Goal.Map != d.MeetMap {
			d.close("no route to " + d.MeetMap)
			b.endMarket(now)
			return false
		}
		b.setState(fmt.Sprintf("going to %s to trade with %s", d.MeetMap, s.partner))
		return true
	}
	if gm == nil {
		return true
	}
	if ps.Map != b.MapName {
		if d.MeetMap == "" {
			d.close("partner left the map")
			b.endMarket(now)
			return false
		}
		// at the meeting point first: wait there
		if !moving && dist(b.X, b.Y, d.MeetX, d.MeetY) > 3 {
			b.stepAlong(gm, d.MeetX, d.MeetY)
		}
		b.setState("waiting for " + s.partner + " to trade")
		return true
	}
	if !s.seller && d.MeetMap == "" && !d.Deliver && !d.Fetch && partyOf(b) == nil && partyOf(partner) != nil {
		// run 10: a solo buyer walks to a party member selling to it (the seller keeps playing with its party)
		if dist(b.X, b.Y, ps.X, ps.Y) > 2 {
			if !b.sameWalkRegion(gm, ps.X, ps.Y) {
				d.close("partner unreachable (other walk region)")
				b.endMarket(now)
				return false
			}
			if !moving {
				b.stepAlong(gm, ps.X, ps.Y)
			}
			b.setState("going to trade with " + s.partner)
			return true
		}
		b.setState("waiting for " + s.partner + " to trade")
		return true
	}
	if !s.seller {
		// the buyer waits (at the meeting spot when meeting in town); the seller comes over
		if d.MeetMap != "" && !moving && dist(b.X, b.Y, d.MeetX, d.MeetY) > 3 && dist(b.X, b.Y, ps.X, ps.Y) > 6 {
			b.stepAlong(gm, d.MeetX, d.MeetY)
		}
		b.setState("waiting for " + s.partner + " to trade")
		return true
	}
	b.setState("going to trade with " + s.partner)
	if dist(b.X, b.Y, ps.X, ps.Y) <= 2 {
		if !b.tradeSafe(now) {
			b.setState("waiting to be safe to trade with " + s.partner)
			return false // fight / rest first (survive, lowHPGuard, hunt); the window freezes the bot
		}
		if !moving && now.Sub(s.reqAt) > 6*time.Second {
			if d.partnerBusy(now) {
				s.tries = 0 // refused while fighting: not "unanswered" - ask again once it's safe
				s.reqAt = now
				b.setState("waiting for " + s.partner + " to finish its fight")
				return true
			}
			if s.tries >= 4 {
				d.close("trade request not answered")
				b.endMarket(now)
				return false
			}
			s.tries++
			s.reqAt = now
			aid := ps.AID
			go b.send(newPkt(0x00e4).u32(aid).b)
		}
		return true
	}
	if !b.sameWalkRegion(gm, ps.X, ps.Y) { // nav.go: across a river (prt_fild04) the straight walk never arrives
		d.close("partner unreachable (other walk region)")
		b.endMarket(now)
		return false
	}
	if !moving {
		b.stepAlong(gm, ps.X, ps.Y)
	}
	return true
}

// dealOpen (no lock held): the bot is in an agreed deal (face to face) or a mail sale still waiting for its zeny / item
func (b *Bot) dealOpen() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s := b.mkt; s != nil {
		if c, _, _ := s.d.state(); !c {
			return true
		}
	}
	return b.mailBusy()
}

// endMarket cleans up this bot's side of a deal (b.mu held).
func (b *Bot) endMarket(now time.Time) {
	s := b.mkt
	if s == nil {
		return
	}
	b.mkt = nil
	closed, reason, _ := s.d.state()
	if !closed {
		s.d.close("abandoned")
		reason = "abandoned"
	}
	if s.open && !s.done {
		go b.send(newPkt(0x00ed).b) // cancel the open window
	}
	if s.goalSet && b.Goal.Set == "market" {
		b.Goal, b.route, b.path = s.savedGoal, nil, nil
	}
	if strings.EqualFold(b.tradeFrom, s.partner) {
		b.tradeFrom, b.tradeOpen = "", time.Time{}
	}
	if s.seller && s.d.Courier {
		b.courierEnd(s.d.ItemID) // vendbuy.go: what didn't sell goes back into the cart for the shop
	}
	if !s.done {
		if s.seller {
			atomic.AddInt64(&econ.failed, 1) // count each deal once
			econMiss("deal ended: " + reason)
			if s.d.Kind != "" {
				econCount("deal failed: " + s.d.Kind)
			}
		}
		b.event("info", s.partner, fmt.Sprintf("trade with %s fell through (%s)", s.partner, reason))
		econ.mu.Lock()
		block := 30 * time.Minute
		if transientFail(reason) {
			block = 4 * time.Minute // run 8: a fight / an escape passes - retry the pair soon instead of 30 min later
		}
		econ.noPair[s.d.SellerName+"|"+s.d.BuyerName] = now.Add(block)
		econ.mu.Unlock()
	}
}

// marketPartner: a trade request from the partner of our current deal (accepted by the 0x01f4 handler).
func (b *Bot) marketPartner(from string) bool {
	if s := b.mkt; s != nil && !s.seller && strings.EqualFold(s.partner, from) {
		c, _, _ := s.d.state()
		return !c
	}
	return false
}

// onMarketTrade handles trade packets for a market deal (b.mu held). Returns true when the packet was fully
// handled here (the generic party/guild trade code must not act on it).
func (b *Bot) onMarketTrade(op uint16, pk []byte) bool {
	s := b.mkt
	if s == nil {
		return false
	}
	d := s.d
	now := time.Now()
	cancel := func(why string) {
		d.close(why)
		go b.send(newPkt(0x00ed).b)
	}
	switch op {
	case 0x01f5: // trade request answer: result, char id, level (3 = accepted: the window is open on both sides)
		if len(pk) < 3 {
			return false
		}
		if pk[2] != 3 {
			if s.seller {
				s.reqAt = now.Add(-4 * time.Second) // try again shortly
			}
			return false
		}
		partner := d.Buyer
		if !s.seller {
			partner = d.Seller
		}
		if ps, ok := peek(partner.P.Name); len(pk) >= 7 && ok && le.Uint32(pk[3:]) != ps.AID {
			return false // a trade with someone else (a party or guild mate)
		}
		if c, _, _ := d.state(); c || s.open {
			return false
		}
		s.open = true
		d.mu.Lock()
		if d.opened.IsZero() {
			d.opened = now
		}
		d.mu.Unlock()
		if s.seller && d.ItemID == 0 {
			s.added = true // a zeny gift (gearpool.go): the recipient puts nothing in and locks once the zeny shows up
		} else if s.seller {
			idx := -1
			for _, it := range b.inv {
				if it.ItemID == d.ItemID && !it.Equipped && it.Count >= d.Count {
					idx = it.Index
				}
			}
			if idx < 0 {
				cancel("item gone")
				return true
			}
			b.send(newPkt(0x00e8).u16(uint16(idx)).u32(uint32(d.Count)).b)
			if d.Price == 0 {
				s.partnerOK = true // an item gift (alchemy.go: the guild alchemist's potions): nothing comes back
			}
		} else if d.Price == 0 && d.ItemID != 0 {
			s.added = true // receiving an item gift: put nothing in, lock once the item shows up
		} else {
			if b.Zeny < d.Price {
				cancel("not enough zeny")
				return true
			}
			b.send(newPkt(0x00e8).u16(0).u32(uint32(d.Price)).b)
			s.added = true
			if d.ItemID == 0 && !s.lockSent { // a gift: nothing comes back, lock right after the zeny
				s.partnerOK, s.lockSent = true, true
				go func() {
					time.Sleep(time.Duration(800+crand(700)) * time.Millisecond)
					b.send(newPkt(0x00eb).b)
				}()
			}
		}
		return true
	case 0x00ea: // add-item answer: index, result (0 = ok). Index 0 is the lock / zeny ack.
		if !s.open || len(pk) < 5 {
			return false
		}
		idx := int(le.Uint16(pk[2:]))
		if !s.seller || idx == 0 || s.added {
			return true
		}
		if pk[4] != 0 {
			cancel(fmt.Sprintf("could not hand over the item (code %d)", pk[4]))
			return true
		}
		s.added = true
		// the item left the bag for the trade window: the server re-adds it (0x0b41) if the trade is cancelled and
		// sends nothing more if it completes
		if it := b.inv[idx]; it != nil {
			it.Count -= d.Count
			if it.Count <= 0 {
				delete(b.inv, idx)
			}
		}
	case 0x0b42, 0x0a96, 0x0a09: // the other side put something in: item id u32, type, amount i32 (zeny: id 0)
		if !s.open || len(pk) < 11 {
			return false
		}
		id, amt := int(le.Uint32(pk[2:])), int(int32(le.Uint32(pk[7:])))
		switch {
		case s.seller && id == 0 && amt == d.Price:
			s.partnerOK = true
		case !s.seller && id == d.ItemID && amt == d.Count && len(pk) > 11 && pk[11] == 0 && items[id] != nil && items[id].Loc != 0:
			// 2021 layout: item id 2, type 6, amount 7, identified 11. An unidentified weapon/armor can't be worn:
			// TheYukisage Bless bought two looted Clubs and the server refused to equip both (identify.go)
			cancel(fmt.Sprintf("%s is unidentified", itemName(id)))
			return true
		case !s.seller && id == d.ItemID && amt == d.Count:
			s.partnerOK = true
		default:
			cancel(fmt.Sprintf("wrong offer in the trade window (%s x%d)", itemName(id), amt))
			return true
		}
	case 0x00ec: // someone locked: 0 = us, 1 = them
		if !s.open || len(pk) < 3 {
			return false
		}
		if pk[2] == 0 {
			s.selfLocked = true
		} else {
			s.partnerLocked = true
		}
		if s.selfLocked && s.partnerLocked && !s.sent {
			s.sent = true
			go func() {
				time.Sleep(time.Duration(400+crand(600)) * time.Millisecond)
				b.send(newPkt(0x00ef).b)
			}()
		}
		return true
	case 0x00f0: // trade finished (0 = success)
		if !s.open {
			return false
		}
		if len(pk) >= 3 && pk[2] == 0 {
			s.done = true
			d.close("done")
			b.marketDone(s, now)
		} else {
			d.close("trade failed")
		}
		s.open = false
		return false
	case 0x00ee: // cancelled
		if !s.open {
			return false
		}
		if d.recentlyPaused(now) { // the other side was hit (tradeGuard): the deal goes on once it is safe
			s.resetWindow(now)
			return false
		}
		d.close("cancelled")
		s.open = false
		return false
	default:
		return false
	}
	// lock our side once our part is in and theirs checks out
	if s.open && s.added && s.partnerOK && !s.lockSent {
		s.lockSent = true
		go func() {
			time.Sleep(time.Duration(500+crand(700)) * time.Millisecond)
			b.send(newPkt(0x00eb).b)
		}()
	}
	return true
}

// marketDone: the trade went through (b.mu held).
func (b *Bot) marketDone(s *marketSide, now time.Time) {
	d := s.d
	// friends.go: a trade between them (each side counts once)
	go friendNote(b.charName(), s.partner, fkTrade)
	if d.ItemID == 0 && d.Why == "guild fund" { // alchemy.go guildFund
		if s.seller {
			b.event("market", s.partner, fmt.Sprintf("got %dz from %s for the Potion Creation Guide (guild fund)", d.Price, s.partner))
		} else {
			b.event("market", s.partner, fmt.Sprintf("gave %dz to %s for the guild's Potion Creation Guide", d.Price, s.partner))
		}
		atomic.AddInt64(&guildFundGifts, 1)
		return
	}
	if d.ItemID == 0 { // a zeny gift between party mates (gearpool.go)
		typing := b.P.Typing
		if s.seller {
			b.event("market", s.partner, fmt.Sprintf("got %dz from %s for my next gear (party gear turn)", d.Price, s.partner))
			giftDone(d)
			go func() {
				time.Sleep(time.Duration(1+crand(2)) * time.Second)
				b.partySay(styled(typing, pick("ty "+s.partner+"!", "thanks a lot!", "ty, ill get it next town run")))
			}()
		} else {
			b.event("market", s.partner, fmt.Sprintf("gave %dz to %s for its next gear (party gear turn)", d.Price, s.partner))
		}
		atomic.AddInt64(&gearGifts, 1)
		return
	}
	item := itemName(d.ItemID)
	if d.Count > 1 {
		item = fmt.Sprintf("%dx %s", d.Count, item)
	}
	exchangeDealDone(d, s.seller) // exchange.go: tier deal counters, thanks
	memoryOf(b.P).meet(s.partner, "acquaintance", b.MapName, !allChars[s.partner])
	typing := b.P.Typing
	if s.seller {
		b.event("market", s.partner, fmt.Sprintf("sold %s to %s for %dz", item, s.partner, d.Price))
		atomic.AddInt64(&econTrades, 1)
		econCount("deal done: " + d.Kind)
		if d.Price > 0 && d.ItemID != 0 {
			noteMarketFlow("market_sell", d.Price, d.ItemID, d.Count) // marketflow.go (run 11)
		}
		econ.mu.Lock()
		if d.Seller != nil && d.ItemID != 0 {
			marketSold(d.Seller.P.Name, d.ItemID) // market.go: the markdown starts over
		}
		econ.trades = append(econ.trades, Trade{T: now, Seller: d.SellerName, Buyer: d.BuyerName, ItemID: d.ItemID,
			Item: itemName(d.ItemID), Count: d.Count, Price: d.Price, Map: b.MapName})
		if len(econ.trades) > 300 {
			econ.trades = econ.trades[len(econ.trades)-300:]
		}
		if each := d.Price / max(d.Count, 1); d.Price > 0 { // a guild gift (alchemy.go) is not a market price
			st := econ.stats[d.ItemID]
			if st == nil {
				st = &itemStat{Min: each, Max: each}
				econ.stats[d.ItemID] = st
			}
			st.N++
			st.Sum += int64(each)
			st.Min, st.Max, st.Last = min(st.Min, each), max(st.Max, each), each
		}
		econ.mu.Unlock()
		go func() {
			time.Sleep(time.Duration(2+crand(3)) * time.Second)
			b.whisper(s.partner, styled(typing, pick("np, gl!", "ty, have fun", "enjoy :)", "thx")))
		}()
	} else {
		b.event("market", s.partner, fmt.Sprintf("bought %s from %s for %dz", item, s.partner, d.Price))
		noteBought(b, d.ItemID) // refine.go: not offered again for an hour (no flip-flop)
		econ.mu.Lock()
		if w := econ.wants[d.BuyerName]; w != nil && wantAccepts(w, d.ItemID) {
			delete(econ.wants, d.BuyerName)
		}
		econ.nextWant[d.BuyerName] = now.Add(time.Minute) // re-check needs soon (next upgrade)
		econ.mu.Unlock()
		go func() {
			time.Sleep(time.Duration(1+crand(2)) * time.Second)
			b.whisper(s.partner, styled(typing, pick("ty!", "thanks a lot", "tyty", "thx!")))
		}()
	}
}

// ---------------------------------------------------------------- API

func registerEconomy(mux *http.ServeMux) {
	mux.HandleFunc("/api/economy", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, economyReport())
	})
	registerAlchemy(mux)       // alchemy.go: /api/alchemists (guild alchemists, potion market)
	registerBuildGear(mux)
	registerBuildMilestones(mux) // buildmilestones.go: /api/buildmilestones (gear milestones per band)     // buildgear.go: /api/buildgear (per-build item table), /api/buildgear/fit
	registerPricing(mux)       // pricing.go: /api/economy/pricing (money supply, wealth x rarity price table)
	registerGuildStock(mux)    // guildstock.go: /api/guild-stock
	registerOpportunities(mux) // opportunity.go: market opportunities and money goals
	registerFarm(mux)          // farm.go: /api/economy/farm?lv=N (sought-after drops farmable at a level)
}

func economyReport() map[string]interface{} {
	econ.mu.Lock()
	wants := make([]Want, 0, len(econ.wants))
	for _, w := range econ.wants {
		wants = append(wants, *w)
	}
	offers := econ.offers
	if len(offers) > 60 {
		offers = offers[:60]
	}
	offers = append([]offerRow{}, offers...)
	trades := []Trade{}
	for i := len(econ.trades) - 1; i >= 0 && len(trades) < 50; i-- {
		trades = append(trades, econ.trades[i])
	}
	type avgRow struct {
		ItemID   int    `json:"item_id"`
		Item     string `json:"item"`
		Trades   int    `json:"trades"`
		Avg      int    `json:"avg_each"`
		Min      int    `json:"min_each"`
		Max      int    `json:"max_each"`
		Last     int    `json:"last_each"`
		Standard int    `json:"standard"`
	}
	var avgs []avgRow
	for id, st := range econ.stats {
		avgs = append(avgs, avgRow{ItemID: id, Item: itemName(id), Trades: st.N, Avg: int(st.Sum / int64(max(st.N, 1))),
			Min: st.Min, Max: st.Max, Last: st.Last})
	}
	type dealRow struct {
		ID      int64     `json:"id"`
		Seller  string    `json:"seller"`
		Buyer   string    `json:"buyer"`
		Item    string    `json:"item"`
		Count   int       `json:"count"`
		Price   int       `json:"price"`
		Meet    string    `json:"meet"`
		Created time.Time `json:"created"`
		Open    bool      `json:"window_open"`
	}
	var deals []dealRow
	for _, d := range econ.deals {
		c, _, opened := d.state()
		if c {
			continue
		}
		meet := "same map"
		switch {
		case d.Deliver:
			meet = "delivery to " + d.MeetMap
		case d.Fetch:
			meet = "fetch (" + d.Kind + ")"
		case d.MeetMap != "":
			meet = fmt.Sprintf("%s %d,%d", d.MeetMap, d.MeetX, d.MeetY)
		}
		dn := itemName(d.ItemID)
		if d.ItemID == 0 {
			dn = "zeny gift" // gearpool.go party gear turn
		}
		deals = append(deals, dealRow{ID: d.ID, Seller: d.SellerName, Buyer: d.BuyerName, Item: dn,
			Count: d.Count, Price: d.Price, Meet: meet, Created: d.Created, Open: !opened.IsZero()})
	}
	failed := atomic.LoadInt64(&econ.failed)
	econ.mu.Unlock()
	for i := range avgs {
		avgs[i].Standard = priceOf(avgs[i].ItemID)
	}
	sort.Slice(wants, func(i, j int) bool { return wants[i].Created.After(wants[j].Created) })
	sort.Slice(avgs, func(i, j int) bool { return avgs[i].Trades > avgs[j].Trades })
	return map[string]interface{}{
		"trades_total":       atomic.LoadInt64(&econTrades),
		"vend_sales":         atomic.LoadInt64(&vendSales),     // vendbuy.go: items sold by Legion vending shops
		"vend_sales_zeny":    atomic.LoadInt64(&vendSalesZeny), // ... and the zeny they took
		"human_wants":        humanWantsReport(),               // humanwant.go: humans' B> lines and the offers made
		"shops":              openShops(),
		"deals_failed":       failed,
		"deal_fail_reasons":  dealFailReasons(),                 // run 10: why agreed deals fell through (cumulative)
		"loot_flow":          lootFlowReport(),                  // run 10 lootflow.go: kept / party / guild / stock / sold since start
		"market_flow": marketFlowReport(), // run 11 marketflow.go: vendors vs NPC shops (owner: the Prontera market beats the NPCs)
		"counters_since":     countersSince(),                   // run 10: counters are cumulative across deploys (econpersist.go)
		"match_misses_items": econMissesItems(),                 // the same by item ("Blue Potion: no seller")
		"match_misses":       econMisses(),                      // why wants found no trade, and why deals ended (cumulative since start)
		"counts":             econCounts(),                      // batch 9: deals started/done/failed by kind, shop buys by result
		"zeny_by_level":      zenyByLevel(),                     // batch 9: online bots' zeny per 10-level band
		"gear_gifts":         atomic.LoadInt64(&gearGifts),      // gearpool.go zeny gifts completed
		"cards_inserted":     atomic.LoadInt64(&cardsInserted),  // cards.go
		"guild_fund_gifts":   atomic.LoadInt64(&guildFundGifts), // alchemy.go: zeny toward the guild alchemist's guide
		"peco_rented":        pecoRented.Load(),                 // peco.go: rentals seen through
		"katar_fund_mails":   katarFunded.Load(),                // peco.go katarFundRound
		"peco_fund_mails":    pecoFunded.Load(),                 // peco.go: guild / party mates paying a rider's Peco
		"guild_pools":        guildPoolsReport(),                // guildpool.go: guilds pooling zeny for a boss-only item
		"mails_sent":         atomic.LoadInt64(&mailSent),       // mail.go: zeny mails (RODEX) sent / collected / failed
		"mails_collected":    atomic.LoadInt64(&mailZenyIn),
		"mails_failed":       atomic.LoadInt64(&mailFailed),
		"wants":              wants,
		"offers":             offers,
		"deals":              deals,
		"trades":             trades,
		"prices":             avgs,
	}
}

// econSummary: completed trades, open wants and deals in progress (dashboard economy card).
func econSummary() (int64, int, int) {
	econ.mu.Lock()
	defer econ.mu.Unlock()
	return atomic.LoadInt64(&econTrades), len(econ.wants), len(econ.deals)
}

// ---------------------------------------------------------------- why trades don't happen (QA 2026-11-14)

var econMissMu sync.Mutex
var econMissN = map[string]int64{}

func econMiss(reason string) {
	econMissMu.Lock()
	econMissN[reason]++
	econMissMu.Unlock()
}

// econMissItem: the same misses by item (batch 13, /api/economy match_misses_items): which goods fail and why
var econMissItemN = map[string]int64{}

func econMissItem(key string) {
	econMissMu.Lock()
	econMissItemN[key]++
	econMissMu.Unlock()
}

func econMissesItems() map[string]int64 {
	econMissMu.Lock()
	defer econMissMu.Unlock()
	out := make(map[string]int64, len(econMissItemN))
	for k, v := range econMissItemN {
		out[k] = v
	}
	return out
}

func econMisses() map[string]int64 {
	econMissMu.Lock()
	defer econMissMu.Unlock()
	out := make(map[string]int64, len(econMissN))
	for k, v := range econMissN {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------- deal / shop-buy counters (batch 9)

var econCountN = map[string]int64{}

func econCount(key string) {
	econMissMu.Lock()
	econCountN[key]++
	econMissMu.Unlock()
}

func econCounts() map[string]int64 {
	econMissMu.Lock()
	defer econMissMu.Unlock()
	out := make(map[string]int64, len(econCountN))
	for k, v := range econCountN {
		out[k] = v
	}
	return out
}

// zenyByLevel: online bots' zeny per 10-level band (n, total, median): the money flow by level, for the owner.
func zenyByLevel() map[string]interface{} {
	bands := map[int][]int{}
	bots, _ := allBots.Load().([]*Bot)
	for _, b := range bots {
		b.mu.Lock()
		if b.Online && b.MapName != "" {
			k := b.BaseLvOr() / 10 * 10
			bands[k] = append(bands[k], b.Zeny)
		}
		b.mu.Unlock()
	}
	out := map[string]interface{}{}
	for k, v := range bands {
		sort.Ints(v)
		sum := 0
		for _, z := range v {
			sum += z
		}
		out[fmt.Sprintf("lv%02d-%02d", k, k+9)] = map[string]int{"n": len(v), "total": sum, "avg": sum / len(v), "median": v[len(v)/2]}
	}
	return out
}
