package main

// Cards and item variants: real demand for what the bots actually find (2026-11-14 economy audit: 95% of the wants
// were "cheaper than NPC" asks for NPC gear nobody held, while the offers were cards, slotted starter gear and
// potions nobody wanted).
//
//   - Card compounding: a bot that holds a card fitting a free slot of the gear it wears inserts it
//     (CZ_REQ_ITEMCOMPOSITION 0x017c card index, equip index -> ZC_ACK_ITEMCOMPOSITION 0x017d). rAthena refuses to
//     compound into worn gear, so the bot takes the item off first (0x00ab); autoEquip puts it back on.
//   - Card value per build: the card's script bonuses (item_db_etc.yml, "bonus bStr,1;" ...) weighted by the
//     build's stat goals, so a STR knight wants Hornet/Andre, a caster wants Rod/INT cards, everyone likes HP/DEF.
//   - Card wants: a bot with a free slot asks #trade for a card on the board that suits it (price standard).
//   - No weapon / no armor: a bot without any usable weapon (or body armor) asks for the cheapest one on the board
//     instead of only walking to an NPC (starter Knives, Clubs, Cotton Shirts are what the field has spare).
//   - Variants: rAthena has the same item with more slots under another id (Knife 1201 [3] / 1202 [4], Sword
//     1101/1102, Jacket 2303/2304 ...). A want for one accepts any variant at least as good, and a variant no NPC
//     sells is priced from its NPC namesake (+25% per extra slot), not from the generic rarity formula (which made a
//     Knife [4] cost 3,200z next to the NPC's 50z Knife).

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------- card bonuses

type cardBonus struct {
	stat                   [6]int
	all, hp, sp, hpPct     int
	atk, matk, def, mdef   int
	hit, flee, flee2, crit int
	aspd, situational      int
	ele, race              map[string]int // bonus2 bAddEle / bAddRace: element and race damage bonuses (huntfit.go)
}

var (
	cardBonOnce sync.Once
	cardBon     map[int]*cardBonus
)

var cardStatKey = map[string]int{"bStr": stSTR, "bAgi": stAGI, "bVit": stVIT, "bInt": stINT, "bDex": stDEX, "bLuk": stLUK}

func loadCardBonuses() {
	cardBon = map[int]*cardBonus{}
	// run 7: equipment scripts too (accessories / headgear give stats - "bonus bDex,1;" on Glasses, a Ring's STR...):
	// builds.go itemScore counts them, so a Clip / Ring / Sunglasses is a real step and not "Def 0, worth nothing"
	for _, fn := range []string{"item_db_etc.yml", "item_db_equip.yml"} {
		loadBonusFile(filepath.Join(*rathena, "db", "re", fn))
	}
	loadAmmoElements(filepath.Join(*rathena, "db", "re", "item_db_etc.yml")) // huntfit.go: arrow elements (bAtkEle)
}

func loadBonusFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var cur *cardBonus
	for sc.Scan() {
		t := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(t, "- Id:") {
			id, _ := strconv.Atoi(strings.TrimSpace(t[5:]))
			cur = nil
			if it := items[id]; it != nil && (it.Type == "Card" || it.Type == "Armor") {
				cur = &cardBonus{}
				cardBon[id] = cur
			}
			continue
		}
		if cur == nil {
			continue
		}
		if strings.HasPrefix(t, "bonus2 bAddEle,") || strings.HasPrefix(t, "bonus2 bAddRace,") {
			// element / race damage bonus: valued against the hunting map (huntfit.go)
			kv := strings.SplitN(strings.TrimSuffix(strings.TrimSpace(t[len("bonus2 "):]), ";"), ",", 3)
			if len(kv) == 3 {
				if n, err := strconv.Atoi(strings.TrimSpace(kv[2])); err == nil && n > 0 {
					if kv[0] == "bAddEle" {
						if cur.ele == nil {
							cur.ele = map[string]int{}
						}
						cur.ele[strings.ToLower(strings.TrimPrefix(strings.TrimSpace(kv[1]), "Ele_"))] += n
					} else {
						if cur.race == nil {
							cur.race = map[string]int{}
						}
						cur.race[strings.ToLower(strings.TrimPrefix(strings.TrimSpace(kv[1]), "RC_"))] += n
					}
				}
			}
			cur.situational++
			continue
		}
		if strings.HasPrefix(t, "bonus2 ") || strings.HasPrefix(t, "bonus3 ") || strings.HasPrefix(t, "autobonus") {
			cur.situational++
			continue
		}
		if !strings.HasPrefix(t, "bonus ") {
			continue
		}
		body := strings.TrimSuffix(strings.TrimSpace(t[6:]), ";")
		kv := strings.SplitN(body, ",", 2)
		if len(kv) != 2 {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(kv[1]))
		if err != nil {
			continue
		}
		k := strings.TrimSpace(kv[0])
		if s, ok := cardStatKey[k]; ok {
			cur.stat[s] += n
			continue
		}
		switch k {
		case "bAllStats":
			cur.all += n
		case "bMaxHP":
			cur.hp += n
		case "bMaxSP":
			cur.sp += n
		case "bMaxHPrate":
			cur.hpPct += n
		case "bBaseAtk", "bAtk", "bAtk2":
			cur.atk += n
		case "bMatk", "bMatkRate":
			cur.matk += n
		case "bDef":
			cur.def += n
		case "bMdef":
			cur.mdef += n
		case "bHit":
			cur.hit += n
		case "bFlee":
			cur.flee += n
		case "bFlee2":
			cur.flee2 += n
		case "bCritical":
			cur.crit += n
		case "bAspdRate", "bAspd":
			cur.aspd += n
		default:
			cur.situational++
		}
	}
}

// cardValue: how much this character's build gains from a card (0 = useless to it). b.mu held.
func (b *Bot) cardValue(id int) int {
	cardBonOnce.Do(loadCardBonuses)
	cb := cardBon[id]
	if cb == nil {
		if id == itemCreamy {
			return 60 // teleport.go: the Teleport card (see below)
		}
		return 0
	}
	v := b.bonusValue(cb)
	if id == itemCreamy {
		// owner 2026-11-14 (RULES.md 3b): "every bot should aim to have [the Teleport] card so it can travel fast and
		// avoid danger" - Teleport lv1 (teleport.go) outranks a stat card for every build
		v = max(v, 60)
	}
	return v
}

// bonusValue (b.mu held): how much this build gains from a set of item-script bonuses (a card's or a piece of gear's).
func (b *Bot) bonusValue(cb *cardBonus) int {
	var w [6]int // stat weights from the build's goals (a STR 90 build weighs STR 9)
	if p := b.plan(); p != nil {
		for _, g := range p.Stats {
			if g.Stat >= 0 && g.Stat < 6 {
				w[g.Stat] = max(w[g.Stat], g.Value/10)
			}
		}
	} else {
		w = [6]int{3, 3, 3, 3, 3, 3}
	}
	v, sum := 0, 0
	for s := 0; s < 6; s++ {
		v += cb.stat[s] * w[s] * 2
		sum += w[s]
	}
	v += cb.all * sum * 2
	v += cb.hp/25 + cb.hpPct*3 + cb.def*3 + cb.mdef*2 + cb.hit/2 + cb.flee/2 + cb.flee2*2 + cb.aspd*2
	if b.usesSP() {
		v += cb.sp / 15
	}
	// crit does nothing for the spell styles: a priest or mage with a crit card on its staff gets no value from it (QA 2026-11-15)
	switch b.gearStyle() {
	case "magic":
		v += cb.matk * 2
	default:
		v += cb.atk/2 + cb.crit
	}
	v += b.huntFitValue(cb) // element / race bonuses that hit the hunting map (huntfit.go)
	if cb.situational > 0 && v == 0 {
		v = 1 // race/element cards: some use, not worth buying
	}
	return v
}

// gearBonus (b.mu held): the build's value of an equipment piece's own script bonuses (0 = none / unknown).
func (b *Bot) gearBonus(id int) int {
	cardBonOnce.Do(loadCardBonuses)
	if cb := cardBon[id]; cb != nil {
		return b.bonusValue(cb)
	}
	return 0
}

// freeCardSlot: whether this inventory item has an empty card slot (forged/named/egg items have none).
func freeCardSlot(it *InvItem) bool {
	x := items[it.ItemID]
	if x == nil || x.Slots == 0 || (x.Type != "Weapon" && x.Type != "Armor") {
		return false
	}
	if c := it.Cards[0]; c == 0x00ff || c == 0x00fe || c == 0xff00 {
		return false
	}
	for i := 0; i < x.Slots && i < 4; i++ {
		if it.Cards[i] == 0 {
			return true
		}
	}
	return false
}

// cardFitsItem: rAthena's rule (clif_use_card): the card's location overlaps the item's; shield cards never go on
// a weapon.
func cardFitsItem(card, gear *Item) bool {
	if card == nil || gear == nil || card.Loc&gear.Loc == 0 {
		return false
	}
	if gear.Type == "Weapon" && card.Loc == 0x20 {
		return false
	}
	return true
}

// cardSlotFor: the worn item a card would go into (nil = none with a free slot). b.mu held.
func (b *Bot) cardSlotFor(cardID int) *InvItem {
	c := items[cardID]
	if c == nil || c.Type != "Card" {
		return nil
	}
	for _, e := range b.equipped {
		if e != nil && e.Equipped && freeCardSlot(e) && cardFitsItem(c, items[e.ItemID]) {
			return e
		}
	}
	return nil
}

// spareSlotFor: an unworn slotted variant of worn gear (Cotton Shirt [1] next to the worn Cotton Shirt) with a free
// slot the card fits: the card goes in, then the bot wears it. b.mu held.
func (b *Bot) spareSlotFor(cardID int) *InvItem {
	c := items[cardID]
	if c == nil || c.Type != "Card" {
		return nil
	}
	if cardID == itemCreamyCard { // teleslot.go: any slotted accessory in the bag takes the Teleport card, then is worn
		for _, it := range b.inv {
			if x := items[it.ItemID]; x != nil && !it.Equipped && it.Count > 0 && !it.Unidentified && !it.NoEquip &&
				x.Loc&accLocMask != 0 && freeCardSlot(it) && b.canEquip(x) {
				return it
			}
		}
	}
	for _, e := range b.equipped {
		if e == nil || !e.Equipped {
			continue
		}
		vs := variantsOf(e.ItemID)
		for _, it := range b.inv {
			if it.Equipped || it.Count <= 0 || it.Unidentified || it.NoEquip || !freeCardSlot(it) || !cardFitsItem(c, items[it.ItemID]) {
				continue
			}
			for _, v := range vs {
				if v == it.ItemID {
					return it
				}
			}
		}
	}
	return nil
}

// keepsCard: a card this bot will put into its own gear (not offered for sale). b.mu held.
func (b *Bot) keepsCard(id int) bool {
	return b.cardValue(id) >= 2 && (b.cardSlotFor(id) != nil || b.spareSlotFor(id) != nil)
}

var cardTried = struct {
	sync.Mutex
	next map[*Bot]time.Time
}{next: map[*Bot]time.Time{}}

// cardThink: compound one useful card into worn gear (b.mu held; called from the economy round every 15 s for a
// free bot). Out of combat only; one attempt per bot per 2 minutes.
func (b *Bot) cardThink(now time.Time) {
	// run 7: "target == 0" at the 15-s economy sample almost never held for a grinder (5 bots kept a fitting card in the
	// bag); between hits is fine too as long as nothing is hitting us
	if (b.target != 0 && (b.underAttack(now) || b.hpFrac() < 0.7)) || b.Dead || b.Sitting || !b.invReady() {
		return
	}
	cardTried.Lock()
	if now.Before(cardTried.next[b]) {
		cardTried.Unlock()
		return
	}
	cardTried.Unlock()
	var card, gear *InvItem
	bestV := 0
	for _, it := range b.inv {
		x := items[it.ItemID]
		if x == nil || x.Type != "Card" || it.Count <= 0 {
			continue
		}
		// run 7 (class QA 15:30: Peco Peco Egg Card next to a slotted Wand, Skeleton Card next to a Main Gauche, Beetle
		// King Card next to a Dirk - none compounded): a race / element card (value 1) still beats an empty slot, like
		// a player would slot it - unless it is worth real money on the market (sell it instead)
		v := b.cardValue(it.ItemID)
		if v <= 1 && (v <= 0 || priceOf(it.ItemID) >= 10000) {
			continue
		}
		if v > bestV {
			if g := b.cardSlotFor(it.ItemID); g != nil {
				card, gear, bestV = it, g, v
			} else if g := b.spareSlotFor(it.ItemID); g != nil {
				card, gear, bestV = it, g, v
			}
		}
	}
	if card == nil {
		return
	}
	cardTried.Lock()
	cardTried.next[b] = now.Add(2 * time.Minute)
	cardTried.Unlock()
	ci, gi := card.Index, gear.Index
	b.setState("compounding " + itemName(card.ItemID))
	b.remember("putting %s into my %s", itemName(card.ItemID), itemName(gear.ItemID))
	worn, loc := gear.Equipped, items[gear.ItemID].Loc
	if loc == 0x88 {
		loc = 0x8
	}
	go func() {
		if worn {
			b.send(newPkt(0x00ab).u16(uint16(gi)).b) // take it off: rAthena only compounds into unworn gear
			time.Sleep(400 * time.Millisecond)
		}
		b.send(newPkt(0x017c).u16(uint16(ci)).u16(uint16(gi)).b)
		if !worn { // the slotted variant replaces the plain one (same score: autoEquip wouldn't swap them)
			time.Sleep(600 * time.Millisecond)
			b.send(newPkt(0x0998).u16(uint16(gi)).u32(loc).b)
		}
	}()
}

// onCardAck: ZC_ACK_ITEMCOMPOSITION (equip index, card index, result 0 = ok). b.mu held (packet handler).
func (b *Bot) onCardAck(pk []byte) {
	if len(pk) < 7 {
		return
	}
	gi, ci, res := int(le.Uint16(pk[2:])), int(le.Uint16(pk[4:])), pk[6]
	g := b.inv[gi]
	card := b.inv[ci]
	name := "a card"
	cid := 0
	if card != nil {
		cid = card.ItemID
		name = itemName(cid)
	}
	if res != 0 || g == nil {
		b.event("info", "", "couldn't put "+name+" into my gear")
		if g != nil { // don't take it off every 2 minutes for a slot the server says is not free
			for i := range g.Cards {
				if g.Cards[i] == 0 {
					g.Cards[i] = -1
				}
			}
		}
		return
	}
	for i := 0; i < 4; i++ {
		if g.Cards[i] == 0 {
			g.Cards[i] = cid
			break
		}
	}
	b.event("market", "", "put "+name+" into my "+itemName(g.ItemID))
	atomic.AddInt64(&cardsInserted, 1)
}

var cardsInserted int64 // cards compounded (atomic), shown in /api/economy

// ---------------------------------------------------------------- wants from the board

// boardOffers: a copy of the open offers (b.mu may be held: bot -> econ order).
func boardOffers() []offerRow {
	econ.mu.Lock()
	defer econ.mu.Unlock()
	return append([]offerRow(nil), econ.offers...)
}

// onBoard: the item or a variant at least as good is offered by someone (b.mu may be held).
func onBoard(id int) bool {
	ids := append([]int{id}, variantsOf(id)...)
	for _, o := range boardOffers() {
		for _, x := range ids {
			if o.ItemID == x {
				return true
			}
		}
	}
	return false
}

// boardLowAsk: the lowest ask on the board for the item or a variant at least as good (0 = not offered).
func boardLowAsk(id int) int {
	ids := append([]int{id}, variantsOf(id)...)
	low := 0
	for _, o := range boardOffers() {
		for _, x := range ids {
			if o.ItemID == x && (low == 0 || o.LowAsk < low) {
				low = o.LowAsk
			}
		}
	}
	return low
}

// wantsFor: open wants for an item by others (b.mu may be held: bot -> econ order).
func wantsFor(id int, except string) int {
	econ.mu.Lock()
	defer econ.mu.Unlock()
	n := 0
	for name, w := range econ.wants {
		if w.ItemID == id && name != except {
			n++
		}
	}
	return n
}

// cardWant: the best card on the board for a free slot of this bot's gear, within budget. b.mu held.
func (b *Bot) cardWant(budget int) *Want {
	if budget <= 0 {
		return nil
	}
	var best *Want
	bestScore := 0.0
	for _, o := range boardOffers() {
		x := items[o.ItemID]
		if x == nil || x.Type != "Card" || b.cardSlotFor(o.ItemID) == nil || b.ownsUnequipped(o.ItemID) {
			continue
		}
		v := b.cardValue(o.ItemID)
		if v < 2 || wantsFor(o.ItemID, b.charName()) >= 2*o.Sellers {
			continue // 2026-11-14: 25 bots asked for the same 2 Hornet Cards; ask for one that is still to be had
		}
		bid := bidPrice(b.P, o.ItemID)
		if bid > budget || o.LowAsk > bid {
			continue
		}
		if s := float64(v) / float64(max(bid, 1)); best == nil || s > bestScore {
			best, bestScore = &Want{ItemID: o.ItemID, Count: 1, Each: bid, Why: "card for my gear"}, s
		}
	}
	return best
}

// slotWant: a bot holding a useful card with no free slot for it asks for a slotted variant of the gear it wears
// (Cotton Shirt [1] for a Poring Card over a plain Cotton Shirt, Knife [4] ...). b.mu held.
func (b *Bot) slotWant(budget int) *Want {
	if budget <= 0 {
		return nil
	}
	for _, it := range b.inv {
		c := items[it.ItemID]
		if c == nil || c.Type != "Card" || it.Count <= 0 || b.cardValue(it.ItemID) < 2 || b.cardSlotFor(it.ItemID) != nil {
			continue
		}
		for _, e := range b.equipped {
			if e == nil || !cardFitsItem(c, items[e.ItemID]) {
				continue
			}
			for _, v := range variantsOf(e.ItemID) {
				vx := items[v]
				if vx == nil || vx.Slots == 0 || b.ownsUnequipped(v) || !onBoard(v) {
					continue
				}
				if bid := bidPrice(b.P, v); bid <= budget {
					return &Want{ItemID: v, Count: 1, Each: bid, Why: "slot for my " + c.Name}
				}
			}
		}
	}
	return nil
}

// bareWant: a bot with no usable weapon (or no body armor) asks for the cheapest usable one on the board. b.mu held.
func (b *Bot) bareWant(budget int) *Want {
	for _, slot := range []uint32{0x2, 0x10} {
		if b.equipped[slot] != nil {
			continue
		}
		owned := false
		for _, it := range b.inv {
			// any usable item already in the bag counts, worn or not yet (an unidentified or refused one goes to a
			// Magnifier, identify.go): 2026-11-14 acolytes bought Club after Club that the server wouldn't equip
			if x := items[it.ItemID]; x != nil && gearSlot(x.Loc) == slot && it.Count > 0 && b.canEquip(x) {
				owned = true
				break
			}
		}
		if owned {
			continue
		}
		var best *Want
		for _, o := range boardOffers() {
			x := items[o.ItemID]
			if x == nil || gearSlot(x.Loc) != slot || !b.canEquip(x) || b.gearFailed[x.ID] || b.itemScore(x) <= 0 {
				continue
			}
			bid := max(bidPrice(b.P, o.ItemID)*6/5, o.LowAsk) // urgent: pays up to the lowest ask
			if bid > max(budget, 0) || bid > o.Standard*3/2 {
				continue
			}
			if best == nil || bid < best.Each {
				why := "no weapon"
				if slot == 0x10 {
					why = "no armor"
				}
				best = &Want{ItemID: o.ItemID, Count: 1, Each: roundPrice(bid), Why: why}
			}
		}
		if best != nil {
			return best
		}
	}
	return nil
}

// ---------------------------------------------------------------- variants and namesake prices

var (
	variantMu    sync.Mutex
	variantCache = map[int][]int{}
	namesakeMemo = map[int]int{}
)

// variantsOf: other ids of the same item (same name, type and location) at least as good: as many slots or more,
// attack and defense not lower.
func variantsOf(id int) []int {
	variantMu.Lock()
	if v, ok := variantCache[id]; ok {
		variantMu.Unlock()
		return v
	}
	variantMu.Unlock()
	var out []int
	if x := items[id]; x != nil && (x.Type == "Weapon" || x.Type == "Armor") && x.Name != "" {
		for oid, o := range items {
			if oid != id && o.Name == x.Name && o.Type == x.Type && o.Loc == x.Loc && o.Slots >= x.Slots && o.Atk >= x.Atk &&
				o.Def >= x.Def && o.MinLv <= x.MinLv {
				out = append(out, oid)
			}
		}
		sort.Ints(out)
	}
	variantMu.Lock()
	variantCache[id] = out
	variantMu.Unlock()
	return out
}

// namesakePrice: for gear no NPC sells, the price from an NPC-sold item of the same name (+25% per extra slot);
// 0 when there is none.
func namesakePrice(id int) int {
	variantMu.Lock()
	if p, ok := namesakeMemo[id]; ok {
		variantMu.Unlock()
		return p
	}
	variantMu.Unlock()
	p := 0
	if x := items[id]; x != nil && (x.Type == "Weapon" || x.Type == "Armor") && x.Name != "" {
		for oid, o := range items {
			if oid == id || o.Name != x.Name || o.Type != x.Type || o.Loc != x.Loc || o.Buy <= 0 || o.Slots > x.Slots ||
				len(gearShops(oid)) == 0 {
				continue
			}
			if q := o.Buy * (4 + x.Slots - o.Slots) / 4; p == 0 || q < p {
				p = q
			}
		}
	}
	variantMu.Lock()
	namesakeMemo[id] = p
	variantMu.Unlock()
	return p
}
