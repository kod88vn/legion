package main

// farmers.go (WP3, EC45; design docs/design/market-engine.md sections 2.1, 3.2-3.5).
//
// A farmer is a guild-less bot on the roster (data/farmers.json, accounts list). Its own code decides what it farms:
// the market engine (marketengine.go, an algorithm, not an agent) offers it a ranked list of up to 3 items, and the
// farmer takes the first one that passes its own checks (fitFarms, soloMapOK, the EC39 gear gate). Constitution 2.7:
// the offer is advice, never an order, and the farmer may decline it with a recorded reason.
//
// A farmer with no live order keeps the normal opportunity routine (MoneyFarmGoal's own picks): the market offer adds
// to its life, it does not replace it.
//
// Flags (tuning.go, both default off; with both off nothing here changes anything for any bot):
//   farmer_roster_on   the accounts in data/farmers.json are farmers
//   farmer_orders_on   farmers take the engine's offers (MoneyFarmGoal, via farmerGoal) and play the 70/30
//                      hunter/merchant split with a forced merchant session after a parcel (pickChar, via farmerCharPick)
//
// Counters: the CANONICAL farmer counts are in market_metrics.json (marketmetrics.go): offers, accepts, declines by
// reason, deaths, farmer hours, supplied by band. This file calls the noteMM* functions at each event and keeps no second
// copy of those counts. Supply counts only the item of an order in force or finished in the last farmerSupplyKeep, keyed
// by the band the engine listed the item for. Hours are the online time of roster accounts' sessions. Old
// farmer_counters.json (never deployed) is migrated once into the metrics.
//
// Orders: data/farmer_orders.json, {t, since, orders:[...], stats:{accepted, declined, declines, supplied, looted,
// deaths}} in the shape the dashboard Market tab reads. Its stats are as of the last farmer state change (the live
// numbers are in market_metrics.json); looted and per-account deaths are kept here. Live orders reload after a restart.
//
// Hooks (all gated on the farmer roster, so other bots pay nothing): noteFarmerLoot (bot.go pickup ack),
// noteFarmerSale (vendbuy.go vendor sale), noteFarmerDeath (bot.go death), noteFarmerSessionStart/End (bot.go session),
// farmerNoteParcel (mail.go endMail, the server confirmed a send to the farmer's own vendor), farmerCharPick (persona.go
// pickChar), farmerGoal (opportunity.go MoneyFarmGoal).
//
// EC47 (buy own gear from the player market first): a gear-gated farmer's missing piece is posted on the want board by
// the shared econWant (economy.go, "for my build") only when the piece is trade or drop only. An NPC-sold piece is not
// posted: it goes through the shared trip path, where browseShops and gearFromShop take a player shop first when one
// sells it for no more than the NPC (vendbuy.go, town.go). No farmer-specific buying code.

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	farmerDecideGap  = 60 * time.Minute // a farmer decides again no sooner than this after a decision (accept or decline)
	farmerBusyRetry  = 10 * time.Minute // a farmer already on its own goal retries the offer soon; not a decision
	farmerItemCool   = 6 * time.Hour    // the same item to the same farmer again only after this (design 3.2)
	farmerSupplyKeep = 6 * time.Hour    // supply counts for an order's item this long after the order ends
	farmerStartMax   = 15 * time.Minute // a taken order starts after 0..this (design 3.2)
	farmerLifeMin    = 90 * time.Minute // an order lasts this long, plus up to farmerLifeJit
	farmerLifeJit    = 60 * time.Minute
	farmerRosterTTL  = 5 * time.Minute
	farmerSaveEvery  = time.Minute
	farmerOfferAge   = 30 * time.Minute // an engine allocation older than this is not offered
	farmerMerchantP  = 0.3              // share of a farmer's sessions played by its merchant (70/30 split, design 3.4)
	farmerOrdersName = "farmer_orders.json"
	farmerOldCounts  = "farmer_counters.json" // pre-metrics counters, migrated once
)

// farmerPick is the item a farmer chose from its offer, with its safe maps.
type farmerPick struct {
	Item     int
	ItemName string
	Mob      string
	Maps     []string
	Start    time.Duration
	Life     time.Duration
}

// farmerOrder is an accepted order: live from Start to Until.
type farmerOrder struct {
	Farmer string // account (lower case)
	Char   string // the hunter character
	Item   int
	Band   string // the level band the engine listed the item for (the need side of the metric)
	Mob    string
	Maps   []string
	Rank   []MERank
	Start  time.Time
	Until  time.Time
}

// farmerSupplyRec: an order's item for the supply count.
type farmerSupplyRec struct {
	Item  int
	Band  string
	Until time.Time
}

// farmerOrderRow is one order in data/farmer_orders.json.
type farmerOrderRow struct {
	Farmer string    `json:"farmer"`
	Char   string    `json:"char"`
	ItemID int       `json:"item_id"`
	Item   string    `json:"item"`
	Band   string    `json:"band,omitempty"`
	Mob    string    `json:"mob,omitempty"`
	Maps   []string  `json:"maps"`
	Start  time.Time `json:"start"`
	Until  time.Time `json:"until"`
	Rank   []MERank  `json:"rank,omitempty"`
}

// farmerFileStats is the "stats" object of data/farmer_orders.json.
type farmerFileStats struct {
	Accepted int64            `json:"accepted"`
	Declined int64            `json:"declined"`
	Declines map[string]int64 `json:"declines"`
	Supplied map[string]int64 `json:"supplied"` // item name -> units the farmers' merchants sold (metrics)
	Looted   map[string]int64 `json:"looted"`   // item name -> units the farmers' hunters picked up
	Deaths   map[string]int64 `json:"deaths"`   // account -> farmer deaths
}

// farmerFile is data/farmer_orders.json.
type farmerFile struct {
	T      time.Time        `json:"t"`
	Since  time.Time        `json:"since"`
	Orders []farmerOrderRow `json:"orders"`
	Stats  farmerFileStats  `json:"stats"`
}

var (
	farmerMu       sync.Mutex
	farmerLive     = map[string]*farmerOrder{}     // account -> accepted order (live or waiting for its start)
	farmerNextAt   = map[string]time.Time{}        // account -> the next time it may look at an offer; not persisted
	farmerItemAt   = map[string]map[int]time.Time{} // account -> item -> last accepted order; not persisted
	farmerSupply   = map[string][]farmerSupplyRec{} // account -> order items for the supply count
	farmerParcel   = map[string]bool{}              // account -> the hunter parcelled goods: next session is the merchant
	farmerLooted   = map[string]int64{}            // item name -> units looted by farmers' hunters (not in the metrics)
	farmerDeaths   = map[string]int64{}            // account -> deaths (per account; the total is in the metrics)
	farmerSince    time.Time
	farmerLoaded   bool
	farmerSavedAt  time.Time
	farmerDirty    bool
	farmerRosterAt time.Time
	farmerRosterSt map[string]bool
	farmerSessions sync.Map // *Bot -> time its session started (roster accounts only)
)

// farmerAcceptP: the chance a farmer takes an offer (design 3.2: 0.5 + 0.5 x greed).
func farmerAcceptP(greed float64) float64 {
	return 0.5 + 0.5*min(1, max(0, greed))
}

// farmerTiming: the start delay (0..farmerStartMax) and the order life (farmerLifeMin..+farmerLifeJit) from a uniform draw.
func farmerTiming(r func() float64) (time.Duration, time.Duration) {
	start := time.Duration(float64(farmerStartMax) * r())
	life := farmerLifeMin + time.Duration(float64(farmerLifeJit)*r())
	return start, life
}

// farmerFirstFit: the farmer's own choice from its ranked offer. The first item that is not on cooldown, has a safe
// drop map (fit) and whose first map passes soloMapOK (safe) is taken. reason is "" when one was taken, else the
// decline reason: "unsafe map" (something was tried and failed), "cooldown" (everything left was on cooldown).
func farmerFirstFit(rank []MERank, cool func(item int) bool, fit func(item int) ([]string, string), safe func(m string) bool) (farmerPick, string) {
	unsafe, cooled := false, false
	for _, r := range rank {
		if cool(r.ItemID) {
			cooled = true
			continue
		}
		ms, mob := fit(r.ItemID)
		if len(ms) == 0 || !safe(ms[0]) {
			unsafe = true
			continue
		}
		return farmerPick{Item: r.ItemID, ItemName: r.Item, Mob: mob, Maps: ms}, ""
	}
	switch {
	case unsafe:
		return farmerPick{}, "unsafe map"
	case cooled:
		return farmerPick{}, "cooldown"
	}
	return farmerPick{}, "no offer"
}

// farmerRoster: the roster accounts, read from data/farmers.json at most every farmerRosterTTL. Nil when the flag is
// off, and nil when the file is missing or unreadable (no farmers).
func farmerRoster(now time.Time) map[string]bool {
	if !tune().FarmerRosterOn {
		return nil
	}
	farmerMu.Lock()
	defer farmerMu.Unlock()
	if now.Sub(farmerRosterAt) >= farmerRosterTTL {
		farmerRosterAt = now
		farmerRosterSt = meFarmerRoster()
	}
	return farmerRosterSt
}

// farmerMember: the bot's account is on the roster (roster flag on).
func farmerMember(b *Bot, now time.Time) bool {
	return b != nil && b.P != nil && farmerRoster(now)[strings.ToLower(b.P.Account)]
}

// farmerOn: this bot is a farmer and the farmer orders flag is on.
func farmerOn(b *Bot, now time.Time) bool {
	return tune().FarmerOrdersOn && farmerMember(b, now)
}

// farmerEngineOffer: the engine's latest order for one of this account's characters (never a held one).
func farmerEngineOffer(b *Bot, now time.Time) (MEOrder, bool) {
	names := map[string]bool{}
	for _, c := range b.P.Chars {
		names[strings.ToLower(c.Name)] = true
	}
	meState.Lock()
	defer meState.Unlock()
	if meState.result == nil || now.Sub(meState.last) > farmerOfferAge {
		return MEOrder{}, false
	}
	for _, o := range meState.result.Allocation.Orders {
		if !o.Held && names[strings.ToLower(o.Farmer)] {
			return o, true
		}
	}
	return MEOrder{}, false
}

// farmerBandFor: the level band the engine listed the item for (its gap); the bot's own band when the engine gave none.
func farmerBandFor(item, lv int) string {
	meState.Lock()
	defer meState.Unlock()
	if meState.result != nil {
		for _, g := range meState.result.Needs {
			if g.ItemID == item && g.Band != "" {
				return g.Band
			}
		}
	}
	return meBand(lv)
}

// farmerCooling: the item was taken by this account within farmerItemCool.
func farmerCooling(acct string, item int, now time.Time) bool {
	farmerMu.Lock()
	defer farmerMu.Unlock()
	t := farmerItemAt[acct][item]
	return !t.IsZero() && now.Sub(t) < farmerItemCool
}

// farmerRand: the bot's own random source (tests and fresh bots may have none).
func farmerRand(b *Bot) float64 {
	if b.rng != nil {
		return b.rng.Float64()
	}
	return rand.Float64()
}

// farmerDecide (b.mu held): the farmer's own checks on an offer, in the order the decline reasons are listed in
// the design: level, gear gate (EC39), the greed roll, then the ranked items. (Busy is checked by farmerGoal first.)
func (b *Bot) farmerDecide(acct string, rank []MERank, now time.Time) (farmerPick, string) {
	lv := b.BaseLvOr()
	if lv < meMinLv {
		return farmerPick{}, "level"
	}
	if gate, _ := b.gearGate(lv); gate > 0 && gate < lv {
		return farmerPick{}, "gear gate" // behind on its own gear milestone: it farms for its gear first (EC39)
	}
	if farmerRand(b) >= farmerAcceptP(b.P.Greed) {
		return farmerPick{}, "not taken up"
	}
	pk, reason := b.farmerFirstFit(acct, rank, now)
	if reason != "" {
		return pk, reason
	}
	pk.Start, pk.Life = farmerTiming(func() float64 { return farmerRand(b) })
	return pk, ""
}

// farmerFirstFit (b.mu held): farmerFirstFit with the bot's real checks (fitFarms, soloMapOK, the cooldown).
func (b *Bot) farmerFirstFit(acct string, rank []MERank, now time.Time) (farmerPick, string) {
	lv := b.BaseLvOr()
	return farmerFirstFit(rank,
		func(id int) bool { return farmerCooling(acct, id, now) },
		func(id int) ([]string, string) { return b.fitFarms(pieceFarms(id, lv, 4), id) },
		b.soloMapOK)
}

// farmerSetGoal (b.mu held): the farmer's order as its money goal (the same record MoneyFarmGoal writes).
func farmerSetGoal(b *Bot, item int, mob string, maps []string, until, now time.Time) *farmGoal {
	opps.Lock()
	defer opps.Unlock()
	g := opps.goals[b]
	if g == nil || g.ItemID != item || !g.Until.Equal(until) {
		g = &moneyGoal{ItemID: item, Item: itemName(item), Mob: mob, Maps: maps, Until: until}
		opps.goals[b] = g
		if opps.chase[item] == nil {
			opps.chase[item] = map[*Bot]time.Time{}
		}
		opps.chase[item][b] = until
		opps.at = time.Time{}
	}
	return &farmGoal{Kind: "zeny", ItemID: g.ItemID, Item: g.Item, Mob: g.Mob, Map: g.Maps[0], Maps: g.Maps, Set: now, Until: until}
}

// farmerLiveGet: the account's order (live or waiting), nil if none. Reads the saved state on first use.
func farmerLiveGet(acct string, now time.Time) *farmerOrder {
	farmerMu.Lock()
	defer farmerMu.Unlock()
	loadFarmerLocked(now)
	return farmerLive[acct]
}

// farmerGoal (b.mu held, MoneyFarmGoal): a farmer's market order as its farm goal, or nil. nil means the normal
// opportunity routine goes on. A farmer already on its own goal does not decide at all: it looks again in
// farmerBusyRetry (no stats). A new decision: accepted orders start after their delay and last 90-150 min; each call
// re-checks the live order's map.
func (b *Bot) farmerGoal(now time.Time) *farmGoal {
	if !farmerOn(b, now) {
		return nil
	}
	acct := strings.ToLower(b.P.Account)
	if o := farmerLiveGet(acct, now); o != nil {
		if !now.Before(o.Until) {
			farmerMutate(now, true, func() { delete(farmerLive, acct) })
			return nil
		}
		if now.Before(o.Start) {
			return nil
		}
		ms, mob := b.fitFarms(pieceFarms(o.Item, b.BaseLvOr(), 4), o.Item)
		if len(ms) == 0 || !b.soloMapOK(ms[0]) {
			farmerMutate(now, true, func() { delete(farmerLive, acct) })
			noteMMFarmerDecline("unsafe map")
			opps.Lock()
			if g := opps.goals[b]; g != nil && g.ItemID == o.Item {
				delete(opps.goals, b)
			}
			opps.Unlock()
			b.event("market", "", fmt.Sprintf("dropped the market order for %s: no safe map left", itemName(o.Item)))
			return nil
		}
		return farmerSetGoal(b, o.Item, mob, ms, o.Until, now)
	}
	off, ok := farmerEngineOffer(b, now)
	if !ok {
		return nil
	}
	farmerMu.Lock()
	next := farmerNextAt[acct]
	farmerMu.Unlock()
	if now.Before(next) {
		return nil
	}
	if b.moneyGoalOf(now) != nil { // busy on its own goal: not a decision, no offer, no decline; it looks again soon
		farmerMutate(now, false, func() { farmerNextAt[acct] = now.Add(farmerBusyRetry) })
		return nil
	}
	noteMMFarmerOffer() // one offer per decision
	rank := off.Rank
	if len(rank) == 0 {
		rank = []MERank{{ItemID: off.ItemID, Item: off.Item, Map: off.Map, Mob: off.Mob, YPerH: off.YPerH, Value: off.Value}}
	}
	pk, reason := b.farmerDecide(acct, rank, now)
	if reason != "" {
		noteMMFarmerDecline(reason)
		farmerMutate(now, false, func() { farmerNextAt[acct] = now.Add(farmerDecideGap) })
		b.event("market", "", fmt.Sprintf("the market offered %s; not taking it (%s)", off.Item, reason))
		return nil
	}
	o := &farmerOrder{Farmer: acct, Char: b.charName(), Item: pk.Item, Band: farmerBandFor(pk.Item, b.BaseLvOr()),
		Mob: pk.Mob, Maps: pk.Maps, Rank: rank, Start: now.Add(pk.Start), Until: now.Add(pk.Start + pk.Life)}
	noteMMFarmerAccept() // counted before the save below, so the orders file carries it
	farmerMutate(now, true, func() {
		farmerNextAt[acct] = now.Add(farmerDecideGap)
		farmerLive[acct] = o
		if farmerItemAt[acct] == nil {
			farmerItemAt[acct] = map[int]time.Time{}
		}
		farmerItemAt[acct][pk.Item] = now
		farmerSupplyAddLocked(acct, o, now)
	})
	b.event("market", "", fmt.Sprintf("took a market order: %s on %s (%s), starts in %d min",
		itemName(pk.Item), pk.Maps[0], pk.Mob, int(pk.Start.Minutes())))
	if pk.Start == 0 {
		return b.farmerGoal(now)
	}
	return nil
}

// farmerSupplyAddLocked (farmerMu held): the order's item counts for supply until farmerSupplyKeep after it ends.
func farmerSupplyAddLocked(acct string, o *farmerOrder, now time.Time) {
	keep := farmerSupply[acct][:0]
	for _, r := range farmerSupply[acct] {
		if !now.After(r.Until.Add(farmerSupplyKeep)) {
			keep = append(keep, r)
		}
	}
	farmerSupply[acct] = append(keep, farmerSupplyRec{Item: o.Item, Band: o.Band, Until: o.Until})
}

// farmerCharPick (b.mu held, pickChar): a farmer plays its hunter (main) 70 percent and its merchant (vendor) 30
// percent of its sessions; after a parcel the next session is the merchant (design 3.4). nil = not a farmer (or no
// split possible), so pickChar does its usual choice.
func farmerCharPick(b *Bot) *CharSpec {
	now := time.Now()
	if !farmerOn(b, now) {
		return nil
	}
	var main, vend *CharSpec
	for i := range b.P.Chars {
		switch b.P.Chars[i].Role {
		case "main":
			if main == nil {
				main = &b.P.Chars[i]
			}
		case "vendor":
			if vend == nil {
				vend = &b.P.Chars[i]
			}
		}
	}
	if main == nil || vend == nil {
		return nil
	}
	acct := strings.ToLower(b.P.Account)
	farmerMu.Lock()
	due := farmerParcel[acct]
	delete(farmerParcel, acct)
	farmerMu.Unlock()
	if due || farmerRand(b) < farmerMerchantP {
		return vend
	}
	return main
}

// farmerNoteParcel (mail.go endMail, b.mu held, the server confirmed the mail): a farmer's hunter sent goods to its own
// vendor by RODEX, so its next session is the merchant. Not called for a send the server did not confirm.
func farmerNoteParcel(b *Bot, to string) {
	now := time.Now()
	if !farmerMember(b, now) {
		return
	}
	for _, c := range b.P.Chars {
		if c.Role == "vendor" && strings.EqualFold(c.Name, to) {
			farmerMu.Lock()
			farmerParcel[strings.ToLower(b.P.Account)] = true
			farmerMu.Unlock()
			return
		}
	}
}

// noteFarmerLoot (bot.go pickup ack): units a farmer's hunter picked up.
func noteFarmerLoot(b *Bot, item, n int) {
	now := time.Now()
	if n <= 0 || !farmerMember(b, now) {
		return
	}
	farmerMutate(now, false, func() { farmerLooted[itemName(item)] += int64(n) })
}

// noteFarmerSale (vendbuy.go, a vendor sale): units a farmer's merchant sold. Only the item of an order in force, or of
// one that ended within farmerSupplyKeep, counts; it is filed under the band the engine listed the item for.
func noteFarmerSale(b *Bot, item, n int) {
	now := time.Now()
	if n <= 0 || !farmerMember(b, now) {
		return
	}
	acct := strings.ToLower(b.P.Account)
	band, ok := "", false
	farmerMu.Lock()
	for _, r := range farmerSupply[acct] {
		if r.Item == item && !now.After(r.Until.Add(farmerSupplyKeep)) {
			band, ok = r.Band, true
			break
		}
	}
	farmerMu.Unlock()
	if ok {
		noteMMSupplied(band, item, int64(n))
	}
}

// noteFarmerDeath (bot.go death handler): a farmer's account died.
func noteFarmerDeath(b *Bot, now time.Time) {
	if !farmerMember(b, now) {
		return
	}
	acct := strings.ToLower(b.P.Account)
	farmerMutate(now, false, func() { farmerDeaths[acct]++ })
	noteMMFarmerDeath()
}

// noteFarmerSessionStart (bot.go, once the session's character is picked): a roster account's session starts.
func noteFarmerSessionStart(b *Bot, now time.Time) {
	if farmerMember(b, now) {
		farmerSessions.Store(b, now)
	}
}

// noteFarmerSessionEnd (bot.go, when the session logs off): the session's online time is farmer hours in the metrics
// (the denominator of deaths per farmer-hour). Only a session that started as a roster account's counts.
func noteFarmerSessionEnd(b *Bot, now time.Time) {
	v, ok := farmerSessions.LoadAndDelete(b)
	if !ok {
		return
	}
	noteMMFarmerHours(now.Sub(v.(time.Time)).Hours())
}

// farmerMutate changes the state under farmerMu, saves at once when orders changed, else at most once a minute.
func farmerMutate(now time.Time, orders bool, f func()) {
	farmerMu.Lock()
	defer farmerMu.Unlock()
	loadFarmerLocked(now)
	f()
	farmerDirty = true
	if orders || now.Sub(farmerSavedAt) >= farmerSaveEvery {
		farmerSavedAt = now
		saveFarmerLocked(now)
	}
}

// farmerFlush writes pending state to data/farmer_orders.json now (tests and shutdown paths).
func farmerFlush(now time.Time) {
	farmerMu.Lock()
	defer farmerMu.Unlock()
	loadFarmerLocked(now)
	if farmerDirty {
		farmerSavedAt = now
		saveFarmerLocked(now)
	}
}

// farmerMMTotals: accepted, declined (total and by reason), supplied by item name, read from the metrics.
func farmerMMTotals() farmerFileStats {
	mm.Lock()
	defer mm.Unlock()
	mmEnsureLocked()
	s := farmerFileStats{Accepted: mm.accepts, Declines: map[string]int64{}, Supplied: map[string]int64{},
		Looted: map[string]int64{}, Deaths: map[string]int64{}}
	for r, n := range mm.declines {
		s.Declines[r] = n
		s.Declined += n
	}
	for _, byItem := range mm.supplied {
		for item, n := range byItem {
			s.Supplied[itemName(item)] += n
		}
	}
	return s
}

func farmerFilePath() string {
	return filepath.Join(*dataDir, farmerOrdersName)
}

// loadFarmerLocked (farmerMu held): the saved orders and per-account state, once per process. A missing file is none.
// The old farmer_counters.json (pre-metrics) is migrated into the metrics once.
func loadFarmerLocked(now time.Time) {
	if farmerLoaded {
		return
	}
	farmerLoaded = true
	farmerSince = now
	migrateFarmerCounts()
	raw, err := os.ReadFile(farmerFilePath())
	if err != nil {
		return
	}
	var f farmerFile
	if json.Unmarshal(raw, &f) != nil {
		return
	}
	if !f.Since.IsZero() {
		farmerSince = f.Since
	}
	for k, v := range f.Stats.Looted {
		farmerLooted[k] += v
	}
	for k, v := range f.Stats.Deaths {
		farmerDeaths[k] += v
	}
	for _, r := range f.Orders {
		if !now.Before(r.Until) || farmerLive[r.Farmer] != nil {
			continue
		}
		o := &farmerOrder{Farmer: r.Farmer, Char: r.Char, Item: r.ItemID, Band: r.Band, Mob: r.Mob, Maps: r.Maps,
			Rank: r.Rank, Start: r.Start, Until: r.Until}
		farmerLive[r.Farmer] = o
		farmerSupplyAddLocked(r.Farmer, o, now)
	}
}

// migrateFarmerCounts: the old farmer_counters.json totals go into the metrics once. The file is renamed first: if the
// rename fails nothing is added (so a second start cannot add them twice).
func migrateFarmerCounts() {
	path := filepath.Join(*dataDir, farmerOldCounts)
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if os.Rename(path, path+".migrated") != nil {
		return
	}
	var old struct {
		Accepted int64            `json:"accepted"`
		Declined map[string]int64 `json:"declined"`
		Deaths   map[string]int64 `json:"deaths"`
	}
	if json.Unmarshal(raw, &old) != nil {
		return
	}
	mm.Lock()
	mmEnsureLocked()
	mm.accepts += old.Accepted
	for r, n := range old.Declined {
		mm.declines[r] += n
	}
	for _, n := range old.Deaths {
		mm.deaths += n
	}
	mm.Unlock()
}

// saveFarmerLocked (farmerMu held): atomic write of data/farmer_orders.json. Counts come from the metrics.
func saveFarmerLocked(now time.Time) {
	f := farmerFile{T: now, Since: farmerSince, Stats: farmerMMTotals()}
	f.Stats.Looted = farmerCopy(farmerLooted)
	f.Stats.Deaths = farmerCopy(farmerDeaths)
	for acct, o := range farmerLive {
		if now.Before(o.Until) {
			f.Orders = append(f.Orders, farmerOrderRow{Farmer: acct, Char: o.Char, ItemID: o.Item, Item: itemName(o.Item),
				Band: o.Band, Mob: o.Mob, Maps: o.Maps, Start: o.Start, Until: o.Until, Rank: o.Rank})
		}
	}
	sort.Slice(f.Orders, func(i, j int) bool { return f.Orders[i].Farmer < f.Orders[j].Farmer })
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	tmp := farmerFilePath() + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil && os.Rename(tmp, farmerFilePath()) == nil {
		farmerDirty = false
	}
}

func farmerCopy(m map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
