package main

// Party gear pool: "the party is one mind for gear" (RULES.md 3). Every member eventually gets the ideal gear for its
// build, one step at a time; nobody is left a weakling.
//
// Every 20 s, for each party:
//   - each member's next gear step is worked out (the best real upgrade on its build's route that the party's pooled
//     spare zeny could pay for, sold at any reachable shop - the Prontera south-gate market sells everything);
//   - one member holds the party's gear TURN: a member with no usable weapon first, then whoever got gear longest
//     ago, then the weakest (gear score per level). A turn ends only when the item is bought (an unbuyable item is
//     swapped for a buyable one, same member; an offline holder yields after gear_turn_max_min, an unfundable turn
//     after 3x that); zeny raised for an unbought item is never given on (fair turns, not whoever asks first);
//   - if the turn member can't pay for its step alone but the party can, a party mate on the same map hands over the
//     difference through the real trade window (a zeny-only market deal, economy.go). Money only moves between
//     party mates, and every member keeps its own potion reserve;
//   - bot-led parties: when the turn member can pay and the item isn't sold in the party's nearest town, the party's
//     NEXT real-need town run (bag full, out of potions) goes to the shop's town instead - ONE planned trip (everyone
//     shops there, then the party goes back to its hunting ground), at most once per gear_trip_cooldown_min. Never a
//     town run called for gear alone (owner: town runs only for a real need; 90% of the time is farming). A
//     human-led party (Ayla's) is never moved: the member only asks in party chat and the human decides.
// Each member's step is published as b.gearNext (/api/bots/<name> "gear_next", party dashboard).
//
// Not built (yet): account pooling between the characters of one account (a farmer character funding its alts).
// Only one character of an account is online at a time and RO storage holds items, not zeny, so it needs Kafra
// storage support (buy the item on the farmer, store it, take it out on the alt). The party leader strategy above is
// the other strategy the owner allowed ("Both strategies are fine").

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var gearGifts int64 // completed zeny gifts between party mates (atomic)

// gearStep: a character's next gear step, as shown on the API and the party dashboard.
type gearStep struct {
	ItemID   int      `json:"item_id"`
	Item     string   `json:"item"`
	Cost     int      `json:"cost"`
	Slot     string   `json:"slot"`
	Shop     string   `json:"shop,omitempty"` // map of the closest shop selling it
	Far      bool     `json:"far,omitempty"`  // beyond max_gear_hops: bought on a planned trip
	Have     int      `json:"have"`           // zeny the character can spend on gear (zeny minus its potion reserve)
	Status   string   `json:"status"`         // affordable / saving / funded / party pays / ...
	FundedBy []string `json:"funded_by,omitempty"`
	Turn     bool     `json:"turn,omitempty"` // this member holds the party's gear turn
}

var slotNames = map[uint32]string{0x2: "weapon", 0x10: "armor", 0x20: "shield", 0x4: "garment", 0x40: "shoes",
	0x88: "accessory", 0x100: "headgear", 0x200: "headgear", 0x1: "headgear", 0x300: "headgear", 0x301: "headgear"}

// gearStepFor: the best real upgrade on the build's routes costing at most cap, at any reachable shop (b.mu held).
// cheapest=true instead returns the cheapest real upgrade whatever it costs (what the bot is saving up for).
func (b *Bot) gearStepFor(cap int, cheapest bool) (*Item, *Shop) {
	p := b.plan()
	if p == nil || !b.invReady() || b.noArrows() {
		return nil, nil // a bow without arrows: arrows first (ammo.go buys them), no armour offered meanwhile
	}
	noClassW := b.classWeaponMissing()
	var best *Item
	var bestShop *Shop
	bestV := 0
	for _, route := range [][]int{p.Weapons, p.Armors, p.Extras} {
		for _, id := range route {
			it := items[id]
			if it == nil || !b.canEquip(it) || it.Buy <= 0 || (!cheapest && it.Buy > cap) || b.gearFailed[id] || b.ownsUnequipped(id) {
				continue
			}
			slot := gearSlot(it.Loc)
			if slot == 0x20 && b.equipped[0x2] != nil && items[b.equipped[0x2].ItemID] != nil && items[b.equipped[0x2].ItemID].Loc == 0x22 {
				continue
			}
			if noClassW && (slot != 0x2 || !isClassWeapon(family(b.Class), it)) {
				continue // on the starter Knife the class weapon is the only step (no armour first)
			}
			cur := b.curIn(slot)
			if e := b.equipped[slot]; e != nil && e.ItemID == id {
				continue
			}
			if !b.realUpgrade(it, cur) {
				continue
			}
			gain := b.itemScore(it) - b.itemScore(cur)
			// no class weapon (a Knife in an Archer's / Mage's hand) ranks right after an empty hand, ahead of any
			// armour: Onilord21's party funded a Hood for a DEX archer meleeing with a Knife while its Bow waited
			classStep := slot == 0x2 && cur != nil && isClassWeapon(family(b.Class), it) && !isClassWeapon(family(b.Class), cur)
			if slot == 0x2 && cur == nil {
				gain += 1000
			} else if classStep {
				gain += 500
			}
			v := gain
			if cheapest {
				v = 1 << 30 / (it.Buy + 1)
				if slot == 0x2 && cur == nil {
					v += 1 << 29
				} else if classStep {
					v += 1 << 28
				}
			}
			if v <= bestV {
				continue
			}
			s := b.gearShopNear(id)
			if s == nil {
				s = b.gearShopAny(id)
			}
			if s == nil {
				continue
			}
			best, bestShop, bestV = it, s, v
		}
	}
	return best, bestShop
}

// spareZeny: what the character can put into gear (its potion reserve stays).
func (b *Bot) spareZeny() int { return max(0, b.Zeny-b.gearReserve()) }

// gearWeakness: gear score per level (lower = weaker); no usable weapon counts as the weakest.
func (b *Bot) gearWeakness() float64 {
	sum := 0
	for _, e := range b.equipped {
		if e != nil {
			sum += max(0, b.itemScore(items[e.ItemID]))
		}
	}
	return float64(sum) / float64(b.BaseLvOr()+10)
}

// gearStepView: the step for the API (b.mu held). The pool's view (turn, funding) when the bot is in a party.
func (b *Bot) gearStepView() *gearStep {
	if b.gearNext != nil {
		g := *b.gearNext
		return &g
	}
	return b.gearStepSolo(b.spareZeny())
}

func (b *Bot) gearStepSolo(cap int) *gearStep {
	have := b.spareZeny()
	it, sh := b.gearStepFor(max(cap, have), false)
	if it == nil {
		it, sh = b.gearStepFor(0, true)
	}
	it, sh = b.stickyStep(it, sh)
	if it == nil {
		return nil
	}
	g := &gearStep{ItemID: it.ID, Item: it.Name, Cost: it.Buy, Slot: slotNames[gearSlot(it.Loc)], Shop: sh.Map,
		Far: b.gearShopNear(it.ID) == nil, Have: have, Status: "saving"}
	if g.Slot == "" {
		g.Slot = fmt.Sprintf("loc %#x", it.Loc)
	}
	if have >= it.Buy {
		g.Status = "affordable"
		if b.BaseLvOr() < minGearLevel() {
			g.Status = "affordable (shops from level " + fmt.Sprint(minGearLevel()) + ")"
		}
	}
	return g
}

// ---------------------------------------------------------------- the pool

type gearPool struct {
	Turn       string
	TurnSince  time.Time
	TurnItem   int
	LastGot    map[string]time.Time
	TripAt     time.Time // last town run that went to Planned
	AskAt      time.Time
	Planned    string // town the next real-need party town run should go to (gear sold only there)
	PlannedFor string // "Zen.fox's Wand"
	// Raised: only filled in the saved copy (partypersist.go) - the live record is gearPools.raised. Lesson
	// (2026-11-14): it was memory-only, so after a deploy Kimchi.senpai gave away 420z + 730z it had received for
	// its own unbought turn item.
	Raised map[string]int `json:"raised,omitempty"`
}

// gearRunTown: the town the party's gear pool wants its next real-need town run to go to ("" = none). Taking it
// starts the cooldown, so the party doesn't go back and forth (b.mu of the caller may be held: gearPools only).
func gearRunTown(party string, now time.Time) (string, string) {
	gearPools.Lock()
	defer gearPools.Unlock()
	pl := gearPools.m[party]
	if pl == nil || pl.Planned == "" || now.Sub(pl.TripAt) < time.Duration(tune().GearTripCooldownMin)*time.Minute {
		return "", ""
	}
	t, f := pl.Planned, pl.PlannedFor
	pl.TripAt, pl.Planned, pl.PlannedFor = now, "", ""
	return t, f
}

var gearPools = struct {
	sync.Mutex
	m      map[string]*gearPool // party name -> pool
	funded map[string][]string  // recipient char -> party mates who gave zeny for its current step
	raised map[string]int       // recipient char -> zeny received for its current (unbought) turn item: never given away
	recent map[string]time.Time // "donor|recipient" -> last gift (no gift spam)
	log    []string             // last pool events (API)
}{m: map[string]*gearPool{}, funded: map[string][]string{}, raised: map[string]int{}, recent: map[string]time.Time{}}

func gearPoolManager(stop <-chan struct{}) {
	loadHumanGear() // gearpersist.go: Ayla's party's turn and raised zeny survive a legion-core restart
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		gearPoolRound(time.Now())
		select {
		case <-stop:
			return // shutting down: don't save a half-logged-out state
		default:
			saveHumanGear()
		}
	}
}

func gearLog(s string) {
	gearPools.Lock()
	gearPools.log = append(gearPools.log, time.Now().Format("15:04:05")+" "+s)
	if len(gearPools.log) > 50 {
		gearPools.log = gearPools.log[len(gearPools.log)-50:]
	}
	gearPools.Unlock()
}

type poolMember struct {
	b          *Bot
	name, mapN string
	x, y, lv   int
	zeny       int
	spare      int
	maxReserve int
	keep       int // gearKeep: Kafra fare home + missing potion minimum (town.go), which the buy also keeps
	free       bool
	noWeapon   bool
	noClassW   bool // holds a weapon, but not its class weapon (builds.go classWeapons)
	weak       float64
	it         *Item
	shop       *Shop
	owns       bool // owns the pool's turn item
	keepOK     bool // the turn item is still buyable for this member (wearable, not failed, sold somewhere reachable)
	turnIt     *Item
	turnShop   *Shop
	raised     int // zeny raised for its own unbought turn item: kept for it, never gifted on
	gift       int // giftableZeny: what it may hand to a mate (own savings, earmarked gifts, fare kept)
}

// poolPending: zeny mailed to a member for its gear-turn piece in the last 20 min (mail may still be uncollected), so
// the pool stops mailing once the piece is covered.
var poolPending = struct {
	sync.Mutex
	m map[string]pendingGift
}{m: map[string]pendingGift{}}

type pendingGift struct {
	item, amt int
	at        time.Time
}

func poolPendingAdd(rcpt string, item, amt int, now time.Time) {
	poolPending.Lock()
	defer poolPending.Unlock()
	p := poolPending.m[rcpt]
	if p.item != item || now.Sub(p.at) > 20*time.Minute {
		p = pendingGift{item: item}
	}
	p.amt += amt
	p.at = now
	poolPending.m[rcpt] = p
}

func poolPendingFor(rcpt string, item int, now time.Time) int {
	poolPending.Lock()
	defer poolPending.Unlock()
	p := poolPending.m[rcpt]
	if p.item != item || now.Sub(p.at) > 20*time.Minute {
		return 0
	}
	return p.amt
}

// giftShortfall: the zeny a member still lacks to buy an item of this price while keeping its potion reserve, by the
// same rule the buy itself uses (gearReserve: min(reserve_base + lv*per_lv, zeny / div)); <= 0 means it can pay.
// Lesson (2026-11-14): need was price + the FULL level reserve - zeny, ignoring the zeny/div cap the buy uses, so a
// member who could already afford a 50z Rod was handed 400z ("giving 400z to xXKimchicatXx for Rod").
func giftShortfall(price, zeny, maxReserve int) int {
	div := max(1, tune().GearReserveZenyDiv)
	total := price + maxReserve
	if div > 1 {
		total = min(total, (div*price+div-2)/(div-1)) // smallest purse whose spare (zeny - reserve) covers price
	}
	return total - zeny
}

// poolStep (b.mu held): a member's next gear-turn step with the party's pooled spare. Without a class weapon it is
// the cheapest class weapon the pool can pay for; otherwise the best real upgrade (gearStepFor).
func (b *Bot) poolStep(total int, noClassW bool) (*Item, *Shop) {
	if noClassW {
		if it, sh := b.classWeaponPick(total); it != nil {
			return it, sh
		}
	}
	// lfg.go turnPiece: a human party member's locked piece is its step until owned (the human topped it up for that
	// piece: Rinfang's step switched Claire Suits -> Crossbow between two top-ups, 2026-11-14 20:15-20:30)
	if id := b.turnPiece(); id != 0 {
		if it := items[id]; it != nil {
			if sh := b.gearShopNear(id); sh != nil {
				return it, sh
			}
			if l := gearShops(id); len(l) > 0 {
				return it, l[0]
			}
		}
	}
	return b.gearStepFor(total, false)
}

func gearPoolRound(now time.Time) {
	parties.Lock()
	ps := make([]*Party, 0, len(parties.byName))
	for _, p := range parties.byName {
		ps = append(ps, p)
	}
	parties.Unlock()
	live := map[string]bool{}
	for _, p := range ps {
		live[p.Name] = true
		poolParty(p, now)
	}
	gearPools.Lock()
	for n := range gearPools.m {
		if !live[n] {
			delete(gearPools.m, n)
		}
	}
	gearPools.Unlock()
}

func poolParty(p *Party, now time.Time) {
	gearPools.Lock()
	pool := gearPools.m[p.Name]
	if pool == nil && p.Human != "" {
		pool = takeHumanGear(p.Name) // restored after a restart (gearpersist.go)
		if pool != nil {
			gearPools.m[p.Name] = pool
		}
	}
	if pool == nil {
		pool = &gearPool{LastGot: map[string]time.Time{}}
		gearPools.m[p.Name] = pool
	}
	turn, turnItem := pool.Turn, pool.TurnItem
	gearPools.Unlock()

	// 1. snapshot the members (one lock at a time)
	var ms []*poolMember
	busy := false
	total := 0
	for _, b := range p.Members {
		b.mu.Lock()
		if !b.Online || b.MapName == "" || b.plan() == nil || !b.invReady() {
			b.gearNext = nil
			b.mu.Unlock()
			continue
		}
		m := &poolMember{b: b, name: b.charName(), mapN: b.MapName, x: b.X, y: b.Y, lv: b.BaseLvOr(), zeny: b.Zeny,
			spare: b.spareZeny(), maxReserve: tune().GearReserveBase + b.BaseLvOr()*tune().GearReservePerLv, keep: b.gearKeep(nearestTown(b.MapName)),
			noWeapon: b.equipped[0x2] == nil, noClassW: b.classWeaponMissing(), weak: b.gearWeakness(), gift: b.giftableZeny()}
		inDeal := false
		if b.mkt != nil {
			if c, _, _ := b.mkt.d.state(); !c {
				inDeal = true
			}
		}
		busy = busy || inDeal || b.tradeFrom != ""
		m.free = !inDeal && !b.Dead && b.trip == nil && b.kafra == nil && b.vend == nil && b.tradeFrom == "" && b.Goal.Set != "api"
		if turnItem != 0 && m.name == turn {
			m.owns = b.ownsUnequipped(turnItem) || (b.equipped[gearSlot(itemLoc(turnItem))] != nil && b.equipped[gearSlot(itemLoc(turnItem))].ItemID == turnItem)
			// a member without its class weapon or arrows keeps no other turn item (QA 09:30: Starboy, Bard with a Bow
			// in the bag and 0 arrows, had gear_next = Shoes; builds.go gearHold: the necessary item comes first)
			held := b.gearHold() && !(b.classWeaponMissing() && x0IsClassWeapon(b, turnItem))
			if x := items[turnItem]; !held && !m.owns && x != nil && x.Buy > 0 && b.canEquip(x) && !b.gearFailed[turnItem] {
				if sh := b.gearShopAny(turnItem); sh != nil {
					m.keepOK, m.turnIt, m.turnShop = true, x, sh
				}
			}
		}
		b.mu.Unlock()
		total += m.spare
		ms = append(ms, m)
	}
	if len(ms) == 0 {
		return
	}
	// 2. each member's next step: the best real upgrade the party's pooled spare could pay for
	// a member with no class weapon (Archer on the starter Knife): the CHEAPEST class weapon the pool can pay for now
	// (Bow 1000z), never saving for the next tier first (QA 07:42: 28 Archer-line bots with 1,100-5,277z sat on Knives,
	// "party saving (2031 of 2500)" for a Composite Bow). Upgrades come later through the normal turn.
	for _, m := range ms {
		m.b.mu.Lock()
		m.it, m.shop = m.b.poolStep(total, m.noClassW)
		m.b.mu.Unlock()
	}
	byName := map[string]*poolMember{}
	for _, m := range ms {
		byName[m.name] = m
	}

	// 3. the turn: keep it until the item is bought (or unbuyable: swap the item, same member)
	gearPools.Lock()
	if pool.Turn != "" {
		m := byName[pool.Turn]
		// Lesson (2026-11-14): the turn used to rotate after gear_turn_max_min whether or not the item was bought;
		// Zen.fox was funded 2,320z for a Wand that Ayla's Payon runs never sold, then the turn moved on and Zen.fox
		// handed 2,625z to Echobunny for Shoes - zeny went round and round, nobody bought anything. A turn now ends
		// only on the purchase, or when its item is truly unbuyable (then the SAME member gets a buyable item).
		age := now.Sub(pool.TurnSince)
		maxAge := time.Duration(tune().GearTurnMaxMin) * time.Minute
		switch {
		case m != nil && m.owns:
			pool.LastGot[pool.Turn] = now
			delete(gearPools.funded, pool.Turn)
			delete(gearPools.raised, pool.Turn)
			gearPools.Unlock()
			gearLog(fmt.Sprintf("%s: %s got %s (gear turn done)", p.Name, m.name, itemName(pool.TurnItem)))
			gearPools.Lock()
			pool.Turn, pool.TurnItem = "", 0
		case m == nil && age > maxAge:
			pool.LastGot[pool.Turn] = now // offline too long: the next member (its raised zeny stays protected)
			delete(gearPools.funded, pool.Turn)
			pool.Turn, pool.TurnItem = "", 0
		case m == nil:
			// offline for now: the turn waits for it until it times out
		case m.noClassW && m.it != nil && pool.TurnItem != m.it.ID && (m.turnIt == nil || m.it.Buy < m.turnIt.Buy):
			pool.TurnItem = m.it.ID // still on the Knife: switch a pricier turn item to the cheapest class weapon
		case m.keepOK:
			// the turn item is still buyable: keep it (no switching to a pricier step when the pool grows)
			if m.turnIt != nil {
				m.it, m.shop = m.turnIt, m.turnShop
			}
		case m.it == nil:
			pool.Turn, pool.TurnItem = "", 0 // nothing buyable at all any more
			delete(gearPools.funded, m.name)
			delete(gearPools.raised, m.name)
		default:
			pool.TurnItem = m.it.ID // the old item became unbuyable (failed trip, no shop): a buyable one, same member
		}
		// a turn that never got funded (the party can't raise it) yields after 3x the max age; a funded one never does
		if pool.Turn != "" && m != nil && !m.owns && age > 3*maxAge && gearPools.raised[pool.Turn] == 0 && m.it != nil && m.spare < m.it.Buy {
			pool.LastGot[pool.Turn] = now
			pool.Turn, pool.TurnItem = "", 0
		}
		// a member with no weapon / no class weapon comes before a holder that isn't one and has raised nothing yet
		// (QA 05:43: Ravenheart72, Archer on a Knife, sat "waiting for its gear turn (Silverstorm first)")
		if h := byName[pool.Turn]; pool.Turn != "" && h != nil && !h.noWeapon && !h.noClassW && gearPools.raised[pool.Turn] == 0 {
			for _, c := range ms {
				if (c.noWeapon || c.noClassW) && c.it != nil {
					delete(gearPools.funded, pool.Turn)
					pool.Turn, pool.TurnSince, pool.TurnItem = c.name, now, c.it.ID
					break
				}
			}
		}
	}
	if pool.Turn == "" {
		var cands []*poolMember
		for _, m := range ms {
			if m.it != nil {
				cands = append(cands, m)
			}
		}
		sort.SliceStable(cands, func(i, j int) bool {
			a, c := cands[i], cands[j]
			if a.noWeapon != c.noWeapon {
				return a.noWeapon
			}
			if a.noClassW != c.noClassW { // then a member still on the starter Knife (its step is the class weapon)
				return a.noClassW
			}
			la, lc := pool.LastGot[a.name], pool.LastGot[c.name]
			if !la.Equal(lc) {
				return la.Before(lc)
			}
			return a.weak < c.weak
		})
		if len(cands) > 0 {
			pool.Turn, pool.TurnSince, pool.TurnItem = cands[0].name, now, cands[0].it.ID
		}
	}
	turn = pool.Turn
	tripAt, askAt := pool.TripAt, pool.AskAt
	for _, m := range ms {
		if m.name != turn {
			m.raised = gearPools.raised[m.name]
		}
	}
	funded := map[string][]string{}
	for _, m := range ms {
		if f := gearPools.funded[m.name]; len(f) > 0 {
			funded[m.name] = append([]string(nil), f...)
		}
	}
	gearPools.Unlock()

	// 4. funding: party mates on the same map make up what the turn member is missing, if together they can
	r := byName[turn]
	status := ""
	if r != nil && r.it != nil {
		need := max(giftShortfall(r.it.Buy, r.zeny, r.maxReserve), r.it.Buy+r.keep-r.zeny)
		// zeny already mailed for this piece and not collected yet counts (Ayla's mind 20:30-21:12: friends kept mailing
		// Silent rogue 200z "for ur Wooden Mail" - 9 gifts, the piece still unbought)
		if pend := poolPendingFor(r.name, r.it.ID, now); pend > 0 && need > 0 {
			need = max(0, need-pend)
			if need == 0 {
				status = "funded (mail on its way)"
			}
		}
		others := 0
		for _, m := range ms {
			if m != r && m.mapN == r.mapN {
				others += m.gift
			}
		}
		switch {
		case need <= 0:
			status = "can pay (buys it on the next town run)"
		case others < need:
			status = fmt.Sprintf("party saving (%dz of %dz)", r.spare+others, r.it.Buy)
		case busy || !r.free:
			status = "party pays (waiting)"
		default:
			status = "party pays"
			donors := []*poolMember{}
			for _, m := range ms {
				m.spare = m.gift // run 9: giftableZeny (raised turn zeny, earmarked gifts, its own next piece, fare kept)
				// the gift goes by mail (startGift): any free mate online, wherever it stands - nobody walks or waits
				if m != r && m.free && m.spare >= tune().GearPoolMinGift {
					donors = append(donors, m)
				}
			}
			sort.Slice(donors, func(i, j int) bool { return donors[i].spare > donors[j].spare })
			for _, d := range donors {
				gearPools.Lock()
				recent := now.Sub(gearPools.recent[d.name+"|"+r.name]) < 3*time.Minute
				gearPools.Unlock()
				if recent {
					continue
				}
				amt := min(d.spare, (need+9)/10*10) // the shortfall only (rounded to 10z), never a round 400z for a 50z Rod
				if startGift(d.b, r.b, d.name, r.name, amt, r.it, now) {
					poolPendingAdd(r.name, r.it.ID, amt, now)
					gearPools.Lock()
					gearPools.recent[d.name+"|"+r.name] = now
					gearPools.Unlock()
					gearLog(fmt.Sprintf("%s: %s gives %dz to %s for %s", p.Name, d.name, amt, r.name, r.it.Name))
					status = "party pays (" + d.name + " is handing over zeny)"
				}
				break // one gift at a time
			}
		}
		// 5. a planned gear run when the item isn't sold in the party's nearest town (bot-led parties only)
		if need <= 0 && r.shop != nil {
			town := townOf(r.shop.Map)
			if town == "" {
				town = nearestTown(r.shop.Map)
			}
			// economy run 9 (QA 14:05: lillunashot's lv44-51 party ran to YUNO for a Hood - sold in Prontera / Payon -
			// and ping-ponged the Kafra Prontera <-> Morocc, 12 teleports each): a gear run goes only to Prontera (the
			// south gate) when the party's own town doesn't sell it - never to a far town picked by shop distance
			if town != "prontera" {
				town = ""
				if soldIn(r.it.ID, "prontera") {
					town = "prontera"
				}
			}
			leadMap := r.mapN
			if p.Leader != nil {
				if s, ok := peek(p.Leader.P.Name); ok && s.Map != "" {
					leadMap = s.Map
				}
			}
			here := nearestTown(leadMap)
			cool := time.Duration(tune().GearTripCooldownMin) * time.Minute
			if town != "" && town != here && !soldIn(r.it.ID, here) {
				// owner: town runs only for a real need, and 90% of the time is farming - the gear run is never called
				// on its own: the party's NEXT real-need town run goes to this town instead (partyRunTown)
				if p.Human == "" && now.Sub(tripAt) > cool {
					gearPools.Lock()
					pool.Planned, pool.PlannedFor = town, r.name+"'s "+r.it.Name
					gearPools.Unlock()
					status = "next town run goes to " + town
				} else if p.Human != "" && now.Sub(askAt) > cool {
					gearPools.Lock()
					pool.AskAt = now
					gearPools.Unlock()
					rb, item, cost := r.b, r.it.Name, r.it.Buy
					go func() {
						rb.mu.Lock()
						typing := rb.P.Typing
						rb.mu.Unlock()
						rb.partySay(styled(typing, pick(fmt.Sprintf("next town run %s maybe? i can get a %s there (%dz)", town, item, cost),
							fmt.Sprintf("need a %s, can our next town run be in %s?", item, town),
							fmt.Sprintf("when we need town, can we do %s? %s is sold there, i have the %dz", town, item, cost))))
					}()
					status = "asked the leader to make the next town run " + town
				}
			}
		}
	}

	// 6. publish each member's step
	for _, m := range ms {
		m.b.mu.Lock()
		g := m.b.gearStepSolo(total)
		if g != nil {
			g.FundedBy = funded[m.name]
			if m.name == turn {
				g.Turn = true
				if status != "" {
					g.Status = "gear turn: " + status
				}
			} else if g.Have < g.Cost && total >= g.Cost {
				g.Status = "waiting for its gear turn (" + turn + " first)"
			}
		}
		m.b.gearNext = g
		m.b.mu.Unlock()
	}
}

// gearTurnIn: the party gear-turn item this bot holds, if a shop in town sells it and budget covers it (b.mu held;
// takes parties and gearPools only).
func (b *Bot) gearTurnIn(town string, budget int) (*Item, *Shop) {
	p := partyOf(b)
	if p == nil || town == "" {
		return nil, nil
	}
	gearPools.Lock()
	id := 0
	if pl := gearPools.m[p.Name]; pl != nil && pl.Turn == b.charName() {
		id = pl.TurnItem
	}
	gearPools.Unlock()
	x := items[id]
	if x == nil || x.Buy <= 0 || x.Buy > budget || b.gearFailed[id] || !b.canEquip(x) || b.ownsUnequipped(id) {
		return nil, nil
	}
	if b.gearHold() && !(b.classWeaponMissing() && gearSlot(x.Loc) == 0x2 && isClassWeapon(family(b.Class), x)) {
		return nil, nil // the necessary item first (builds.go gearHold)
	}
	for _, sh := range gearShops(id) {
		if sh.Map == town || townOf(sh.Map) == town {
			return x, sh
		}
	}
	return nil, nil
}

func itemLoc(id int) uint32 {
	if it := items[id]; it != nil {
		return it.Loc
	}
	return 0
}

// gearPoolView: the party's turn and recent pool events (party dashboard).
func gearPoolView(party string) (string, []string) {
	gearPools.Lock()
	defer gearPools.Unlock()
	turn := ""
	if pl := gearPools.m[party]; pl != nil {
		turn = pl.Turn
	}
	var ev []string
	pre := party + ": "
	for _, l := range gearPools.log {
		if len(l) > 9 && len(l[9:]) > len(pre) && l[9:9+len(pre)] == pre {
			ev = append(ev, l)
		}
	}
	if len(ev) > 5 {
		ev = ev[len(ev)-5:]
	}
	return turn, ev
}

// ---------------------------------------------------------------- the zeny gift (a market deal with no item)

// startGift: donor mails amount zeny (RODEX, 2% fee) to a party mate for its gear step. Mail, not the trade window:
// a face-to-face gift made the recipient stand "waiting for <donor> to trade" for minutes while the donor fought 34
// cells away, pulling the party apart (Echobunny / Zendancer84, Ayla's party, 2026-11-14). The receiver collects it
// at once (0x09e7 -> checkMail), wherever it is; nobody walks or waits.
func startGift(donor, rcpt *Bot, dName, rName string, amount int, it *Item, now time.Time) bool {
	donor.mu.Lock()
	ok := donor.Online && donor.charName() == dName && !donor.Dead && donor.tradeFrom == "" &&
		(donor.mkt == nil || func() bool { c, _, _ := donor.mkt.d.state(); return c }()) &&
		donor.Zeny >= mailCost(amount) &&
		donor.mailZeny(rName, amount, "for your "+it.Name, "party gear turn, get that "+it.Name, func(ok bool, note string) {
			if !ok {
				return
			}
			giftDone(&Deal{SellerName: rName, BuyerName: dName, Price: amount})
			atomic.AddInt64(&gearGifts, 1)
			econCount("gear gift: mailed")
		})
	donor.mu.Unlock()
	if !ok {
		return false
	}
	donor.event("info", rName, fmt.Sprintf("mailing %dz to %s for %s (party gear turn)", amount, rName, it.Name))
	go func() {
		donor.mu.Lock()
		dt := donor.P.Typing
		donor.mu.Unlock()
		donor.partySay(styled(dt, pick(fmt.Sprintf("%s, sent u %dz by mail for ur %s", rName, amount, it.Name),
			fmt.Sprintf("%s check ur mail, %dz for that %s", rName, amount, it.Name))))
		time.Sleep(time.Duration(2+crand(3)) * time.Second)
		rcpt.mu.Lock()
		rt := rcpt.P.Typing
		rcpt.mu.Unlock()
		rcpt.partySay(styled(rt, pick("got it, ty!", "thank u!!", "tyty")))
	}()
	return true
}

// giftDone: the zeny gift went through (called from marketDone, b.mu of that side held; takes only gearPools).
func giftDone(d *Deal) {
	gearPools.Lock()
	f := gearPools.funded[d.SellerName]
	seen := false
	for _, n := range f {
		seen = seen || n == d.BuyerName
	}
	if !seen {
		gearPools.funded[d.SellerName] = append(f, d.BuyerName)
	}
	gearPools.raised[d.SellerName] += d.Price
	gearPools.Unlock()
	markPartiesDirty() // persist the raised-zeny record (data/parties.json)
}

// x0IsClassWeapon: the item is a weapon of the bot's class family (b.mu held)
func x0IsClassWeapon(b *Bot, id int) bool {
	x := items[id]
	return x != nil && gearSlot(x.Loc) == 0x2 && isClassWeapon(family(b.Class), x)
}

// upgradeGain (b.mu held): what the item adds over what the bot wears in its slot (<= 0: no real upgrade)
func (b *Bot) upgradeGain(it *Item) int {
	if it == nil || !b.canEquip(it) || it.Buy <= 0 || b.gearFailed[it.ID] || b.ownsUnequipped(it.ID) {
		return 0
	}
	slot := gearSlot(it.Loc)
	if e := b.equipped[slot]; e != nil && e.ItemID == it.ID {
		return 0
	}
	cur := b.curIn(slot)
	if !b.realUpgrade(it, cur) {
		return 0
	}
	return b.itemScore(it) - b.itemScore(cur)
}

// stickyStep (economy run 9; QA 13:50: gear_next flipped between checks as zeny moved - Rinfang Great Bow -> Mantle,
// vex Muffler -> Rapier, Silent Muffler -> Wooden Mail - so nobody ever saved for one thing): the target is kept until
// bought or no longer an upgrade, unless a new pick is clearly better (gain x1.5) or a step the bot can buy now while
// the kept one is far off. b.mu held.
func (b *Bot) stickyStep(it *Item, sh *Shop) (*Item, *Shop) {
	if old := items[b.gearStick]; old != nil && (it == nil || old.ID != it.ID) {
		og := b.upgradeGain(old)
		if og > 0 && !b.gearHold() {
			ng := b.upgradeGain(it)
			spare := b.spareZeny()
			better := it != nil && (ng*2 >= og*3 || it.Buy <= spare && old.Buy > 3*max(spare, 1))
			if !better {
				if s := b.gearShopNear(old.ID); s != nil {
					return old, s
				}
				if s := b.gearShopAny(old.ID); s != nil {
					return old, s
				}
			}
		}
	}
	if it != nil {
		b.gearStick = it.ID
	}
	return it, sh
}

// giftableZeny (b.mu held): what a bot may give away - its spare minus the zeny a party raised for its own unbought
// gear-turn item (economy run 9: Zendancer84 was funded 9.5k for a Staff by three party mates, then the Starveil pool
// mailed it on to Echoz; it stood in Prontera with 3.7k, "can pay", buying nothing).
// Also kept (QA 14:00): zeny received by mail lately (earmarked for what it was sent for), the fare home
// (gearKeep), and the savings for its own next piece when that piece is within reach (<= 2x its spare).
func (b *Bot) giftableZeny() int {
	gearPools.Lock()
	r := gearPools.raised[b.charName()]
	gearPools.Unlock()
	if time.Now().After(b.earmarkUntil) {
		b.earmarkZ = 0
	}
	keep := max(b.gearReserve(), b.gearKeep(nearestTown(b.MapName))) + r + b.earmarkZ
	if g := b.gearNext; g != nil && g.Cost > 0 && g.Cost <= 2*max(b.spareZeny(), 1) {
		keep += g.Cost
	}
	return aylaGiftCap(b.charName(), b.Zeny, max(0, min(b.Zeny-keep, b.Zeny))) // G12: Ayla keeps 10,000z
}

// townBuyNow (b.mu held): the gear piece to buy right here, standing in this town: the class weapon, the party
// gear-turn item, or the plan's next piece - sold in this town and paid from the bot's own spare. Economy run 9
// (QA 13:50): Ayla's friends stood at the Prontera Kafra with the money for their pieces and bought nothing because
// only a "town run" ever shopped.
func (b *Bot) townBuyNow(town string) *Item {
	if !b.invReady() || b.BaseLvOr() < minGearLevel() || b.overRegenWeight() {
		return nil
	}
	budget := b.Zeny - max(b.gearReserve(), b.gearKeep(town))
	if budget <= 0 {
		return nil
	}
	if it, _ := b.classWeaponNeed(); it != nil && it.Buy <= budget && soldIn(it.ID, town) {
		return it
	}
	if b.gearHold() {
		return nil
	}
	if it, _ := b.gearTurnIn(town, budget); it != nil {
		return it
	}
	if g := b.gearNext; g != nil && g.ItemID != 0 {
		if it := items[g.ItemID]; it != nil && it.Buy <= budget && soldIn(it.ID, town) && b.upgradeGain(it) > 0 {
			return it
		}
	}
	if it, s := b.nextGear(); it != nil && s != nil && it.Buy <= budget && soldIn(it.ID, town) {
		return it
	}
	return nil
}
