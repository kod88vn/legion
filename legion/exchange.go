package main

// exchange.go (redesign package X, 2026-11-14). Owner: "items can be freely given to party member or guild member or
// at a heavily discounted price, therefore, a chat agent must be there to help bots communicate".
//
// Price by relationship, on top of the existing want/offer matcher (economy.go econRound):
//   - party member                  -> free
//   - guild mate or friend (>= 35)  -> free when the item is a spare for the giver (it owns 2+ of it, or the item is
//                                      not on its build table for its level band or any later band), else
//                                      kin_discount_pct (default 25) percent of the market price (priceOf)
//   - anyone else                   -> the market price (the matcher's existing meet-in-the-middle price)
// The giver side is the existing loot rule: what reaches the matcher is econSpare (never what the bot itself wears,
// compounds or refines with) filtered by lootflow.go (party -> guild -> guild stock before a sale).
// Free and discounted hand-overs are "favours": at most exchange_gifts_per_hour per giver; past that the giver sells
// at the market price like anyone else.
//
// Ask order: a want is asked in party chat first, then guild chat, then #trade (exchange_ask_stage_sec per stage;
// stages the buyer has no party / guild for are skipped). While the want is at the party stage only party members may
// fill it; at the guild stage party, guild mates and friends; at the #trade stage anyone.
//
// Everything here is behind tuning exchange_tiers_on (default off): with it off every hook returns at once and the
// matcher behaves exactly as before. Counters and a recent list: GET /api/exchange.
//
// Locking: exch.mu is a leaf lock (nothing else is locked while it is held). exchangePrepare runs in econRound with no
// lock held and takes one bot lock at a time; the matcher hooks run under econ.mu and take only exch.mu;
// exchangeDealDone runs under the bot's b.mu and takes only exch.mu. No file I/O under any lock: the build table is
// read (it stats a file) before any bot is locked.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	relStranger = iota
	relFriend
	relGuild
	relParty
)

var relNames = [...]string{relStranger: "stranger", relFriend: "friend", relGuild: "guild", relParty: "party"}

const (
	tierParty   = "party"    // party member: free
	tierKinFree = "kin_free" // guild mate / friend, a spare: free
	tierKin     = "kin"      // guild mate / friend, not a spare: kin_discount_pct of market
	tierMarket  = "market"   // anyone else
)

const exRecentMax = 60

// ---------------------------------------------------------------- tuning accessors

func exchangeOn() bool { return tune().ExchangeTiersOn }

func kinDiscountPct() int {
	if v := tune().KinDiscountPct; v >= 0 && v <= 100 {
		return v
	}
	return 25
}

func exGiftsPerHour() int {
	if v := tune().ExchangeGiftsPerHour; v > 0 {
		return v
	}
	return 3
}

func exAskStage() time.Duration {
	if v := tune().ExchangeAskStageSec; v > 0 {
		return time.Duration(v) * time.Second
	}
	return 3 * time.Minute
}

// ---------------------------------------------------------------- pure rules (unit tested)

// exTier: the price per unit and the tier name for a hand-over between two characters with this relation.
// market is the market price per unit; pct the kin discount (percent of market the taker pays).
func exTier(rel int, spare bool, market, pct int) (int, string) {
	switch rel {
	case relParty:
		return 0, tierParty
	case relGuild, relFriend:
		if spare {
			return 0, tierKinFree
		}
		if pct <= 0 {
			return 0, tierKin
		}
		return max(1, roundPrice(market*pct/100)), tierKin
	}
	return market, tierMarket
}

// exSpare: an item is a spare for its holder when it owns two or more (bag + worn), or the item is not on its build
// table at all (for its level band or later).
func exSpare(owned int, onTable bool) bool {
	return owned >= 2 || !onTable
}

// exOnTable: the item is on the build's gear table (slots or cards) for the level's band or any later band - gear the
// build will want later is not "spare" even if the bot can't wear it yet.
func exOnTable(table map[string][]gearBand, build string, lv, id int) bool {
	for _, gb := range table[build] {
		if gb.Max < lv {
			continue
		}
		for _, l := range gb.Slots {
			for _, e := range l {
				if e.ID == id {
					return true
				}
			}
		}
		for _, e := range gb.Cards {
			if e.ID == id {
				return true
			}
		}
	}
	return false
}

// exRelation: the closest relation between two characters from their party / guild / friendship.
func exRelation(partyA, partyB, guildA, guildB string, friends bool) int {
	switch {
	case partyA != "" && partyA == partyB:
		return relParty
	case guildA != "" && guildA == guildB:
		return relGuild
	case friends:
		return relFriend
	}
	return relStranger
}

// exAskChannel: where a want is asked at this age: party chat, then guild chat, then #trade; a stage the buyer has
// no party / guild for is skipped. Returns the channel and the stage index (0 party, 1 guild, 2 #trade).
func exAskChannel(age, stage time.Duration, inParty, inGuild bool) (string, int) {
	var chans []string
	var idx []int
	if inParty {
		chans, idx = append(chans, "party"), append(idx, 0)
	}
	if inGuild {
		chans, idx = append(chans, "guild"), append(idx, 1)
	}
	chans, idx = append(chans, "#trade"), append(idx, 2)
	i := 0
	if stage > 0 && age > 0 {
		i = int(age / stage)
	}
	if i >= len(chans) {
		i = len(chans) - 1
	}
	return chans[i], idx[i]
}

// exSellerAllowed: at a given ask stage, may a seller with this relation fill the want?
func exSellerAllowed(stageIdx, rel int) bool {
	switch stageIdx {
	case 0:
		return rel == relParty
	case 1:
		return rel >= relFriend
	}
	return true
}

// exFavourOK: the giver did fewer than limit favours in the last hour (times newest last; the list is pruned).
func exFavourOK(times []time.Time, limit int, now time.Time) ([]time.Time, bool) {
	out := times[:0]
	for _, t := range times {
		if now.Sub(t) < time.Hour {
			out = append(out, t)
		}
	}
	return out, len(out) < limit
}

// Ayla keeps at least 10,000 zeny (RULES.md 5). Nothing in this package moves a human's zeny; a price is only quoted
// to Ayla when paying it leaves her at or above the floor (her purse from status.json), and only gifts when her purse
// is unknown.
const aylaName = "Ayla"
const aylaZenyFloor = 10000

func isAyla(name string) bool { return strings.EqualFold(strings.TrimSpace(name), aylaName) }

// aylaQuoteOK: may this price be quoted to the human `name`? Gifts always; Ayla only if she keeps the floor.
func aylaQuoteOK(name string, zenyKnown bool, zeny, price int) bool {
	if price <= 0 || !isAyla(name) {
		return true
	}
	return zenyKnown && zeny-price >= aylaZenyFloor
}

// ---------------------------------------------------------------- state

type exRel struct {
	party, partyHuman, guild string
	members                  []string // party mates (names), for the hearers of a party line
}

type exChoice struct {
	tier       string
	rel        int
	price      int // total
	market     int // per unit
	count      int
	at         time.Time
	seller     string
	buyer      string
	itemID     int
	talkClaims bool
	sides      int // marketDone calls seen (seller, buyer)
}

type exRecord struct {
	T      time.Time `json:"t"`
	Giver  string    `json:"giver"`
	Taker  string    `json:"taker"`
	ItemID int       `json:"item_id"`
	Item   string    `json:"item"`
	Count  int       `json:"count"`
	Tier   string    `json:"tier"`
	Rel    string    `json:"relation"`
	Price  int       `json:"price"`       // total zeny paid
	Market int       `json:"market_each"` // market price per unit at the time
	Status string    `json:"status"`      // matched / done
	Human  bool      `json:"human,omitempty"`
}

type exAskRec struct {
	item  int
	stage int
}

var exch = struct {
	sync.Mutex
	rel     map[string]exRel        // char -> party / guild this round
	friend  map[string]bool         // "a|b" (sorted) -> friend tier or better, among online characters
	spare   map[string]map[int]bool // holder -> item -> spare (exSpare)
	market  map[int]int             // item -> priceOf this round
	favours map[string][]time.Time  // giver -> favour times
	asks    map[string]exAskRec     // buyer -> last asked stage for its want
	askedAt map[string]time.Time    // buyer -> last party/guild ask line
	chosen  map[string]*exChoice    // "seller|buyer|item" -> tier deal in progress
	counts  map[string]int64
	recent  []exRecord
}{rel: map[string]exRel{}, friend: map[string]bool{}, spare: map[string]map[int]bool{}, market: map[int]int{},
	favours: map[string][]time.Time{}, asks: map[string]exAskRec{}, askedAt: map[string]time.Time{},
	chosen: map[string]*exChoice{}, counts: map[string]int64{}}

func exCountLocked(k string) { exch.counts[k]++ }

func exCount(k string) {
	exch.Lock()
	exch.counts[k]++
	exch.Unlock()
}

func pairKey(a, b string) string {
	if b < a {
		a, b = b, a
	}
	return a + "|" + b
}

// exRelLocked (exch.mu held): relation between two characters known this round.
func exRelLocked(a, b string) int {
	ra, rb := exch.rel[a], exch.rel[b]
	return exRelation(ra.party, rb.party, ra.guild, rb.guild, exch.friend[pairKey(a, b)])
}

// ---------------------------------------------------------------- econRound hooks

// exchangePrepare (econRound, no lock held): relations, friendships, spares and market prices for this round.
func exchangePrepare(snaps []*econSnap, now time.Time) {
	if !exchangeOn() {
		return
	}
	table := buildGearTable() // stats a file: before any bot lock
	rel := map[string]exRel{}
	names := map[string]bool{}
	byParty := map[*Party][]string{}
	partyOfSnap := map[*econSnap]*Party{}
	for _, s := range snaps {
		p := partyOf(s.b) // parties lock, no bot lock held
		names[s.name] = true
		if p != nil {
			partyOfSnap[s] = p
			byParty[p] = append(byParty[p], s.name)
		}
	}
	for _, s := range snaps {
		r := exRel{guild: s.guild}
		if p := partyOfSnap[s]; p != nil {
			r.party, r.partyHuman = fmt.Sprintf("%p", p), p.Human
			for _, n := range byParty[p] {
				if n != s.name {
					r.members = append(r.members, n)
				}
			}
			if p.Human != "" {
				r.members = append(r.members, p.Human)
			}
		}
		rel[s.name] = r
	}
	friendSet := exFriendPairs(names, friendTierFriend)
	spare := map[string]map[int]bool{}
	ids := map[int]bool{}
	for _, s := range snaps {
		if len(s.spare) == 0 {
			continue
		}
		b := s.b
		m := map[int]bool{}
		b.mu.Lock()
		build, lv := "", b.BaseLvOr()
		if p := b.plan(); p != nil {
			build = p.ID
		}
		for _, h := range s.spare {
			owned := 0
			for _, it := range b.inv {
				if it != nil && it.ItemID == h.ItemID {
					owned += max(it.Count, 1)
				}
			}
			m[h.ItemID] = exSpare(owned, build != "" && exOnTable(table, build, lv, h.ItemID))
			ids[h.ItemID] = true
		}
		b.mu.Unlock()
		spare[s.name] = m
	}
	market := map[int]int{}
	for id := range ids {
		market[id] = priceOf(id) // takes econ.mu: not held here
	}
	exch.Lock()
	exch.rel, exch.friend, exch.spare, exch.market = rel, friendSet, spare, market
	for k, c := range exch.chosen {
		if now.Sub(c.at) > 20*time.Minute {
			delete(exch.chosen, k)
		}
	}
	exch.Unlock()
}

// exFriendPairs: pairs among `names` with an effective friendship >= min (one pass under friends.Mutex).
func exFriendPairs(names map[string]bool, min float64) map[string]bool {
	out := map[string]bool{}
	friends.Lock()
	defer friends.Unlock()
	for _, src := range []map[string]*friendPair{friends.pairs, friends.peer} {
		for k, p := range src {
			if out[k] || !names[p.A] || !names[p.B] {
				continue
			}
			if friendEffLocked(p.A, p.B) >= min {
				out[pairKey(p.A, p.B)] = true
			}
		}
	}
	return out
}

// exchangeEligible (matcher, econ.mu held): may seller s fill w at the want's current ask stage?
func exchangeEligible(w *Want, buyer, s *econSnap, now time.Time) bool {
	if !exchangeOn() || buyer == nil || s == nil || buyer.stocker || s.courier {
		return true
	}
	exch.Lock()
	defer exch.Unlock()
	br := exch.rel[buyer.name]
	_, idx := exAskChannel(now.Sub(w.Created), exAskStage(), br.party != "", br.guild != "")
	return exSellerAllowed(idx, exRelLocked(s.name, buyer.name))
}

// exTierLocked (exch.mu held): the tier price per unit for seller -> buyer, or "" for the market path.
func exTierLocked(s, buyer *econSnap, itemID, ask int, now time.Time) (int, string, int) {
	if itemID == 0 || s.courier || buyer.stocker || s.stocker {
		return -1, "", relStranger
	}
	rel := exRelLocked(s.name, buyer.name)
	if rel == relStranger {
		return -1, "", rel
	}
	var ok bool
	exch.favours[s.name], ok = exFavourOK(exch.favours[s.name], exGiftsPerHour(), now)
	if !ok {
		return -1, "", rel
	}
	market := exch.market[itemID]
	if market <= 0 {
		market = ask
	}
	spare := exch.spare[s.name][itemID] && !s.keeper // a guild vendor's stock is sold to guild mates at the kin price
	each, tier := exTier(rel, spare, market, kinDiscountPct())
	return each, tier, rel
}

// exchangeTier (matcher, econ.mu held): per-unit tier price for this holding, "" when the market rules apply.
func exchangeTier(s, buyer *econSnap, h holding) (int, string) {
	if !exchangeOn() || s == nil || buyer == nil {
		return -1, ""
	}
	exch.Lock()
	defer exch.Unlock()
	each, tier, _ := exTierLocked(s, buyer, h.ItemID, h.Ask, time.Now())
	return each, tier
}

// exchangePrefer (matcher, econ.mu held): +1 when candidate seller s beats the current best seller bs for this buyer
// by relation (party, then kin with a spare, then kin, then strangers), -1 when it is worse, 0 when equal.
func exchangePrefer(buyer, s *econSnap, sItem int, bs *econSnap, bsItem int) int {
	if !exchangeOn() || buyer == nil || s == nil || bs == nil {
		return 0
	}
	exch.Lock()
	defer exch.Unlock()
	rank := func(x *econSnap, id int) int {
		switch exRelLocked(x.name, buyer.name) {
		case relParty:
			return 3
		case relGuild, relFriend:
			if exch.spare[x.name][id] {
				return 2
			}
			return 1
		}
		return 0
	}
	a, b := rank(s, sItem), rank(bs, bsItem)
	switch {
	case a > b:
		return 1
	case a < b:
		return -1
	}
	return 0
}

// exchangeChosen (matcher, econ.mu held): a match was made; a tier deal is recorded (favour, counters, recent list,
// and the record exchangeDealTalk / exchangeDealDone read).
func exchangeChosen(s, buyer *econSnap, itemID, count, price int, now time.Time) {
	if !exchangeOn() || s == nil || buyer == nil {
		return
	}
	exch.Lock()
	defer exch.Unlock()
	rel := exRelLocked(s.name, buyer.name)
	market := exch.market[itemID]
	tier := tierMarket
	if rel != relStranger && !s.courier && !buyer.stocker {
		_, tier = exTier(rel, exch.spare[s.name][itemID] && !s.keeper, market, kinDiscountPct())
		if market > 0 && price > roundPrice(market*kinDiscountPct()/100)*max(count, 1) && price > 0 {
			tier = tierMarket // the favour limit or the market price won: an ordinary sale
		}
	}
	if tier != tierMarket {
		exch.favours[s.name] = append(exch.favours[s.name], now)
		exch.chosen[s.name+"|"+buyer.name+"|"+fmt.Sprint(itemID)] = &exChoice{tier: tier, rel: rel, price: price, market: market,
			count: count, at: now, seller: s.name, buyer: buyer.name, itemID: itemID}
	}
	exCountLocked("matched: " + tier)
	exRecordLocked(exRecord{T: now, Giver: s.name, Taker: buyer.name, ItemID: itemID, Item: itemName(itemID), Count: count,
		Tier: tier, Rel: relNames[rel], Price: price, Market: market, Status: "matched"})
}

func exRecordLocked(r exRecord) {
	exch.recent = append(exch.recent, r)
	if len(exch.recent) > exRecentMax {
		exch.recent = exch.recent[len(exch.recent)-exRecentMax:]
	}
}

// exchangeAsk (want posting, econ.mu held): at the party / guild stage the want is asked there (once per stage, at
// most one such line per bot every 5 minutes) instead of #trade. true = handled (the #trade code skips it).
func exchangeAsk(w *Want, s *econSnap, now time.Time) bool {
	if !exchangeOn() || s == nil || w == nil || w.InDeal || s.stocker {
		return false
	}
	exch.Lock()
	r := exch.rel[s.name]
	ch, idx := exAskChannel(now.Sub(w.Created), exAskStage(), r.party != "", r.guild != "")
	if ch == "#trade" {
		exch.Unlock()
		return false
	}
	last := exch.asks[s.name]
	if last.item == w.ItemID && last.stage >= idx || now.Sub(exch.askedAt[s.name]) < 5*time.Minute {
		exch.Unlock()
		return true
	}
	exch.asks[s.name] = exAskRec{item: w.ItemID, stage: idx}
	exch.askedAt[s.name] = now
	exCountLocked("asked: " + ch)
	hearers := append([]string(nil), r.members...)
	exch.Unlock()
	line := styled(s.typing, exAskLine(w, ch))
	facts := map[string]any{"item": w.Item, "item_id": w.ItemID, "count": w.Count, "bid_each": w.Each, "why": w.Why,
		"stage": ch, "ask_order": "party, guild, #trade"}
	go EnqueueChatJob(ChatJobSpec{Kind: ChatKindAsk, Channel: ch, Speaker: s.b, Hearers: hearers, Facts: facts,
		Template: []ChatLine{{Speaker: s.name, Channel: ch, Text: line}}})
	return true
}

func exAskLine(w *Want, ch string) string {
	item := w.Item
	if w.Count > 1 {
		item = fmt.Sprintf("%d %s", w.Count, w.Item)
	}
	if ch == "guild" {
		return pick("anyone in the guild got "+item+"? can pay a bit", "guild, does anyone have a spare "+item+"?",
			"looking for "+item+", any guildie has one?")
	}
	return pick("anyone got a spare "+item+"?", "need "+item+", anyone have one?", "does anyone have "+item+"? would help a lot")
}

// exchangeTradePost (econRound, no lock held): a #trade line from the market code; with chat jobs on it becomes a job.
func exchangeTradePost(b *Bot, text string) {
	if !chatJobsOn() {
		b.channelSay("#trade", text)
		return
	}
	kind := ChatKindAsk
	if u := strings.ToUpper(strings.TrimSpace(text)); strings.HasPrefix(u, "S>") || strings.HasPrefix(u, "SELLING") {
		kind = ChatKindOffer
	}
	EnqueueChatJob(ChatJobSpec{Kind: kind, Channel: "#trade", Speaker: b, Facts: map[string]any{"line": text},
		Template: []ChatLine{{Speaker: b.charName(), Channel: "#trade", Text: text}}})
}

// exchangeDealTalk (startDeal's conversation goroutine, no lock held): a tier deal is talked through in party chat /
// guild chat / whisper with tier lines (as chat jobs). false = not a tier deal, the usual sale whispers follow.
func exchangeDealTalk(seller, buyer *econSnap, itemID, count, price int, meet, deliver, fetch bool) bool {
	if !exchangeOn() {
		return false
	}
	exch.Lock()
	c := exch.chosen[seller.name+"|"+buyer.name+"|"+fmt.Sprint(itemID)]
	if c == nil || c.talkClaims {
		exch.Unlock()
		return false
	}
	c.talkClaims = true
	exch.Unlock()
	item := itemName(itemID)
	if count > 1 {
		item = fmt.Sprintf("%dx %s", count, item)
	}
	ch, to := "party", ""
	switch c.rel {
	case relGuild:
		ch = "guild"
	case relFriend:
		ch, to = "whisper", buyer.name
	}
	if c.tier == tierKin { // a price is private: whisper it
		ch, to = "whisper", buyer.name
	}
	st, bt := seller.typing, buyer.typing
	var offer string
	switch c.tier {
	case tierParty:
		offer = pick("here "+buyer.name+", take my "+item, buyer.name+" u can have my "+item+", free", "got "+item+" for u "+buyer.name+", np")
	case tierKinFree:
		offer = pick("i have a spare "+item+", its yours "+buyer.name, buyer.name+" take my extra "+item+", no charge", "got an extra "+item+", free for u")
	default:
		offer = pick(fmt.Sprintf("%s for u, guildie price %s", item, fmtZ(price)), fmt.Sprintf("u can have my %s for %s, friend price", item, fmtZ(price)),
			fmt.Sprintf("%s? %s for u, way under market", item, fmtZ(price)))
	}
	where := "come trade, im close"
	switch {
	case meet:
		where = pick("ty!! meet at pront south gate?", "omg ty, see u at prontera south gate")
	case deliver:
		where = pick("ty!! im on "+buyer.mapN+", bring it pls", "thank u! im at "+buyer.mapN)
	case fetch:
		where = pick("ty!! omw to "+seller.mapN, "thanks, coming to u")
	default:
		where = pick("ty!! im right here", "omg ty, come trade", "thank u <3 im next to u")
	}
	bch, bto := ch, ""
	if ch == "whisper" {
		bto = seller.name
	}
	facts := map[string]any{"item": item, "item_id": itemID, "count": count, "price": price, "market_each": c.market,
		"tier": c.tier, "relation": relNames[c.rel], "giver": seller.name, "taker": buyer.name,
		"meet": map[bool]string{true: "prontera south gate"}[meet], "deliver_to": map[bool]string{true: buyer.mapN}[deliver],
		"fetch_from": map[bool]string{true: seller.mapN}[fetch]}
	EnqueueChatJob(ChatJobSpec{Kind: ChatKindOffer, Channel: ch, To: to, Speaker: seller.b, Hearers: []string{buyer.name}, Facts: facts,
		Template: []ChatLine{{Speaker: seller.name, Channel: ch, To: to, Text: styled(st, offer)},
			{Speaker: buyer.name, Channel: bch, To: bto, Text: styled(bt, where), DelaySec: 3 + crand(4)}}})
	return true
}

// exchangeDealDone (marketDone, the bot's b.mu held): a tier deal completed. Counts once (seller side); the taker of a
// free hand-over thanks in party / guild chat.
func exchangeDealDone(d *Deal, seller bool) {
	if d == nil || d.ItemID == 0 {
		return
	}
	k := d.SellerName + "|" + d.BuyerName + "|" + fmt.Sprint(d.ItemID)
	exch.Lock()
	c := exch.chosen[k]
	if c == nil {
		exch.Unlock()
		return
	}
	c.sides++
	if c.sides == 1 { // whichever side finishes first counts the deal
		exCountLocked("done: " + c.tier)
		for i := len(exch.recent) - 1; i >= 0; i-- {
			if r := &exch.recent[i]; r.Giver == d.SellerName && r.Taker == d.BuyerName && r.ItemID == d.ItemID && r.Status == "matched" {
				r.Status = "done"
				break
			}
		}
	} else {
		delete(exch.chosen, k)
	}
	exch.Unlock()
	if seller {
		return
	}
	if c.tier == tierKin || c.rel == relFriend {
		return // the usual whispered thanks follow
	}
	ch := "party"
	if c.rel == relGuild {
		ch = "guild"
	}
	if d.Buyer == nil {
		return
	}
	typing := d.Buyer.P.Typing
	item := itemName(d.ItemID)
	line := styled(typing, pick("ty "+d.SellerName+" for the "+item+"!", "got the "+item+", thanks "+d.SellerName+" <3", "tyty "+d.SellerName))
	go EnqueueChatJob(ChatJobSpec{Kind: ChatKindThanks, Channel: ch, Speaker: d.Buyer, Hearers: []string{d.SellerName},
		Facts:    map[string]any{"item": item, "giver": d.SellerName, "tier": c.tier},
		Template: []ChatLine{{Speaker: d.BuyerName, Channel: ch, Text: line, DelaySec: 2 + crand(3)}}})
}

// ---------------------------------------------------------------- API

// exchangeReport: GET /api/exchange (local part)
func exchangeReport() map[string]any {
	exch.Lock()
	counts := map[string]int64{}
	for k, v := range exch.counts {
		counts[k] = v
	}
	recent := append([]exRecord(nil), exch.recent...)
	exch.Unlock()
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].T.After(recent[j].T) })
	return map[string]any{
		"on":               exchangeOn(),
		"kin_discount_pct": kinDiscountPct(),
		"gifts_per_hour":   exGiftsPerHour(),
		"ask_stage_sec":    int(exAskStage() / time.Second),
		"rules": []string{"party member: free", "guild mate or friend: free when a spare (2+ owned, or not on the giver's build table), else kin_discount_pct of market",
			"anyone else: market price", "ask order: party chat, guild chat, #trade", "favours (free / discounted) at most gifts_per_hour per giver"},
		"counts":  counts,
		"recent":  recent,
		"intents": intentsReport(),
	}
}

func registerExchange(mux *http.ServeMux, bots []*Bot) {
	mux.HandleFunc("/api/exchange", func(w http.ResponseWriter, r *http.Request) {
		rep := exchangeReport()
		if r.URL.Query().Get("local") != "1" {
			if base := coreAPIURL(); base != "" {
				if resp, err := coreClient.Get(base + "/api/exchange?local=1"); err == nil {
					var core map[string]any
					if json.NewDecoder(resp.Body).Decode(&core) == nil {
						rep["core"] = core
					}
					resp.Body.Close()
				}
			}
		}
		writeJSON(w, rep)
	})
	registerChatJobs(mux)         // chatjobs.go: /api/chat-jobs
	registerChatIntent(mux, bots) // chatintent.go: /api/chat-intent
}

// lootTalkJob (lootflow.go lootGive's conversation goroutine, no lock held): with chat jobs on, the loot-flow offer
// ("anyone need a X?" - "me!" - "np, mailed") is one chat job with the same template lines. false = chat jobs off,
// lootGive talks as before.
func lootTalkJob(b, rb *Bot, kind, item, to, typing string) bool {
	if !chatJobsOn() || b == nil {
		return false
	}
	ch := "party"
	if kind == "guild" {
		ch = "guild"
	}
	giver := b.charName()
	lines := []ChatLine{{Speaker: giver, Channel: ch,
		Text: styled(typing, pick("anyone need a "+item+"?", "got a "+item+", anyone use it?", "who needs "+item+"?"))}}
	if rb != nil {
		rb.mu.Lock()
		rt := rb.P.Typing
		rb.mu.Unlock()
		lines = append(lines, ChatLine{Speaker: to, Channel: ch, DelaySec: 4 + crand(5),
			Text: styled(rt, pick("me! i can use that", "oh i need that", "me pls", "yes pls, mine is worse"))})
	}
	lines = append(lines, ChatLine{Speaker: giver, Channel: ch, DelaySec: 3 + crand(4),
		Text: styled(typing, pick("np take it, mailed", "np, sent it to u "+to, "here u go, check ur mail"))})
	EnqueueChatJob(ChatJobSpec{Kind: ChatKindOffer, Channel: ch, Speaker: b, Hearers: []string{to},
		Facts:    map[string]any{"item": item, "giver": giver, "taker": to, "price": 0, "how": "mailed", "flow": "loot flow (" + kind + ")"},
		Template: lines})
	return true
}
