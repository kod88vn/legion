package main

// opportunity.go (economy run 8). Two owner rules, 2026-11-14 (RULES.md 2):
//   - "the market should first prioritize hp restore items to sell even before blue pots. More bots should be farming
//     for healing items": healing items (healItem) are vendable, listed first, stocked first by vendors, kept from the
//     NPC (parcelled to the guild's open shop instead), and count double in the opportunity score.
//   - "bots should prioritize checking out markets in pron and figure out what it should farm to make money as well.
//     Any high demand items missing is an opportunity to farm and make money": marketOpps scores every item by demand
//     (live wants + sales + build interest) against supply (units in Prontera shops + bots already chasing it) x the
//     market price; a bot back from a town run in Prontera "checks the market" (checkMarket) and takes the best
//     opportunity that drops on a map safe for it as its money goal (moneyGoal), farms it (solo map picker, ai.go;
//     goal parties via MoneyFarmGoal for lfg.go "zeny" goals) and sells it (shops / #trade / parcels).
//   - No herding: each item takes at most oppChaseCap chasers, and every chaser counts as oppChaseSupply units of
//     supply, so the score falls as bots take it up (and as listings rise).
//
// GET /api/economy/opportunities: the scored items and the money goals.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

const (
	oppChaseCap    = 8                // bots farming the same opportunity item at once
	oppChaseSupply = 3                // units of supply one chaser counts for
	oppGoalLife    = 90 * time.Minute // a money goal lasts this long (then the next Prontera visit picks again)
	oppHealDemand  = 30               // base demand of a healing item (owner: healing items first; ~1,000 bots drink)
	oppHealMult    = 2.0              // healing items' score weight
)

// healItem: an HP restorer players trade - Red..White Potions, the healing herbs and food (not the starter kit).
func healItem(id int) bool {
	if id >= itemRedPotion && id <= itemWhitePotion || id == itemWhiteHerb {
		return true
	}
	return foodHeal[id] > 0
}

type oppItem struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	Price   int     `json:"price"`
	Demand  int     `json:"demand"`
	Listed  int     `json:"listed"`
	Chasers int     `json:"chasers"`
	Score   float64 `json:"score"`
	Heal    bool    `json:"heal,omitempty"`
}

type moneyGoal struct {
	ItemID int       `json:"item_id"`
	Item   string    `json:"item"`
	Mob    string    `json:"mob"`
	Maps   []string  `json:"maps"`
	Until  time.Time `json:"until"`
	Score  float64   `json:"score"`
}

var opps = struct {
	sync.Mutex
	rows  []oppItem
	at    time.Time
	chase map[int]map[*Bot]time.Time // item -> chasers (until)
	goals map[*Bot]*moneyGoal
	n     map[string]int // counters: checks, goals set, no fit
	// market talk (owner: "the information bots found should be shared globally")
	rumor    map[int]time.Time // item -> until: said on #trade as missing; hearers may farm it
	saidItem map[int]time.Time // item -> last line about it (server-wide, 15 min)
	lastSay  time.Time
}{chase: map[int]map[*Bot]time.Time{}, goals: map[*Bot]*moneyGoal{}, n: map[string]int{},
	rumor: map[int]time.Time{}, saidItem: map[int]time.Time{}}

const (
	oppSayGap     = 50 * time.Second // server-wide: about one market-talk line a minute on #trade
	oppSayItemGap = 15 * time.Minute // never the same item twice in 15 min
	oppRumorLife  = 20 * time.Minute
	oppRumorMult  = 1.5 // a gap said out loud is worth more to the bots who heard it
)

// chasersLocked (opps.mu held): live chasers of an item
func chasersLocked(id int, now time.Time) int {
	n := 0
	for b, t := range opps.chase[id] {
		if now.After(t) {
			delete(opps.chase[id], b)
			continue
		}
		n++
	}
	return n
}

// marketOpps: items worth farming for sale, best first (cached 2 min; takes econ.mu via stockDemand, then opps.mu).
func marketOpps(now time.Time) []oppItem {
	opps.Lock()
	if now.Sub(opps.at) < 2*time.Minute && opps.rows != nil {
		rows := opps.rows
		opps.Unlock()
		return rows
	}
	opps.Unlock()
	need, listed := stockDemand()
	dynPrices.RLock()
	rows := dynPrices.rows
	dynPrices.RUnlock()
	buyers := map[int]int{}
	boss := map[int]bool{}
	for _, r := range rows {
		buyers[r.ID], boss[r.ID] = r.Buyers, r.BossOnly
	}
	ids := map[int]bool{}
	for id := range need {
		ids[id] = true
	}
	for id, n := range buyers {
		if n > 0 {
			ids[id] = true
		}
	}
	for id := range foodHeal {
		ids[id] = true
	}
	for id := itemRedPotion; id <= itemWhitePotion; id++ {
		ids[id] = true
	}
	ids[itemWhiteHerb] = true
	opps.Lock()
	defer opps.Unlock()
	var out []oppItem
	for id := range ids {
		x := items[id]
		if x == nil || boss[id] {
			continue
		}
		heal := healItem(id)
		if !heal && len(gearShops(id)) > 0 {
			continue // an NPC sells it: nobody pays a farmer more than the NPC price
		}
		if !heal && !tradeGood(x) {
			continue // NPC junk is not a market opportunity (owner: "do not sell junks")
		}
		price := priceOf(id)
		if price < 10 || glutted(id) { // market.go (run 8): a surplus is no opportunity - farm something else
			continue
		}
		d := need[id] + buyers[id]/5
		if heal {
			d += oppHealDemand
		}
		if id == itemCreamyCard {
			d += 50 // owner: the Teleport card is high priority in the market
		}
		c := chasersLocked(id, now)
		gap := d - listed[id] - c*oppChaseSupply
		if gap <= 0 {
			continue
		}
		s := float64(gap) * float64(price)
		if heal {
			s *= oppHealMult
		}
		if t, ok := opps.rumor[id]; ok && now.Before(t) {
			s *= oppRumorMult
		}
		out = append(out, oppItem{ID: id, Name: x.Name, Price: price, Demand: d, Listed: listed[id], Chasers: c, Score: s, Heal: heal})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > 60 {
		out = out[:60]
	}
	opps.rows, opps.at = out, now
	return out
}

// pickOpportunity (b.mu held): the best opportunity item that drops on a map safe for this bot, nil when none.
func (b *Bot) pickOpportunity(now time.Time) *moneyGoal {
	lv := b.BaseLvOr()
	for _, o := range marketOpps(now) {
		opps.Lock()
		c := chasersLocked(o.ID, now)
		opps.Unlock()
		if c >= oppChaseCap {
			continue
		}
		ms, mob := b.fitFarms(pieceFarms(o.ID, lv, 4), o.ID)
		if len(ms) == 0 {
			continue
		}
		return &moneyGoal{ItemID: o.ID, Item: o.Name, Mob: mob, Maps: ms, Until: now.Add(oppGoalLife), Score: o.Score}
	}
	return nil
}

// checkMarket (b.mu held, town.go trip "done"): a bot back from a town run in Prontera looks over the south-gate
// market and takes the best missing / scarce item it can farm as its money goal.
func (b *Bot) checkMarket(now time.Time) {
	if townOf(b.MapName) != "prontera" || b.charRole() == "vendor" || !b.invReady() || b.BaseLvOr() < 15 {
		return
	}
	opps.Lock()
	g := opps.goals[b]
	opps.n["market checks"]++
	opps.Unlock()
	typing := b.P.Typing
	go marketTalk(b, typing, now)
	if g != nil && now.Before(g.Until) {
		return
	}
	ng := b.pickOpportunity(now)
	opps.Lock()
	defer opps.Unlock()
	if g != nil {
		delete(opps.chase[g.ItemID], b)
	}
	if ng == nil {
		delete(opps.goals, b)
		opps.n["market check: nothing farmable"]++
		return
	}
	opps.goals[b] = ng
	if opps.chase[ng.ItemID] == nil {
		opps.chase[ng.ItemID] = map[*Bot]time.Time{}
	}
	opps.chase[ng.ItemID][b] = ng.Until
	opps.at = time.Time{} // the score drops for the next bot
	opps.n["money goals set"]++
	b.event("market", "", fmt.Sprintf("checked the market at the south gate: %s is scarce - farming it on %s (%s) to sell",
		ng.Item, ng.Maps[0], ng.Mob))
}

// marketTalkLocked (opps.mu held): what a bot that just looked over the south-gate shops might say about it on
// #trade - a wanted item nobody sells, a price spike, a glut. "" when nothing is worth a line or the server-wide
// limits say no (one line per oppSayGap, an item at most once per oppSayItemGap). A gap line starts a rumor.
func marketTalkLocked(rows []oppItem, now time.Time) (line string, item int) {
	if now.Sub(opps.lastSay) < oppSayGap || cfloat() > 0.35 {
		return "", 0
	}
	fresh := func(id int) bool { return now.Sub(opps.saidItem[id]) >= oppSayItemGap }
	// 1. a gap: demand, nothing listed
	for _, o := range rows[:min(len(rows), 8)] {
		if o.Listed > 0 || !fresh(o.ID) || len(gearShops(o.ID)) > 0 {
			continue // run 11: an item an NPC shop sells (White Potion, Blue Gemstone, Ygg Leaf...) is never "none in the shops"
		}
		n := o.Name
		line = pick("hmm no "+n+" for sale, wish someone farmed those", "anyone selling "+n+"? none in the shops",
			"no "+n+" at the gate at all, someone farm some pls", "cant find a single "+n+" in pront",
			"if u farm "+n+" u could sell them fast, shops are empty", "where are all the "+n+" sellers")
		opps.rumor[o.ID] = now.Add(oppRumorLife)
		return line, o.ID
	}
	_, listed := stockDemand() // lock order: opps.mu -> econ.mu (nothing takes them the other way)
	dynPrices.RLock()
	rows2 := dynPrices.rows
	dynPrices.RUnlock()
	for _, r := range rows2 {
		if r.BossOnly || r.Standard <= 0 || !fresh(r.ID) {
			continue
		}
		// 2. a spike: the market price far above the item's standard, with some on sale
		if r.Price >= 2*r.Standard && r.Price >= 200 && listed[r.ID] > 0 {
			opps.rumor[r.ID] = now.Add(oppRumorLife) // EC33: hearers may farm it too (heardRumor: only on a map safe for them)
			return pick(r.Name+" "+fmt.Sprint(r.Price/r.Standard)+"x price today??", "wow "+r.Name+" got expensive",
				r.Name+" prices are crazy rn"), r.ID
		}
	}
	// 3. a glut: lots of one thing on sale
	best, bn := 0, 0
	for id, n := range listed {
		if n > bn && fresh(id) {
			best, bn = id, n
		}
	}
	if bn >= 40 {
		noteGlutSaid(best) // market.go: said out loud, the glut weighs more on farm choices for a while
		n := itemName(best)
		return pick("so many "+n+" in the shops lol", "everyone is selling "+n+" today", n+" everywhere at the gate, cheap"), best
	}
	return "", 0
}

// marketTalk (b.mu NOT held: channelSay sends): after a market check, maybe say what was seen.
func marketTalk(b *Bot, typing string, now time.Time) {
	rows := marketOpps(now)
	opps.Lock()
	line, item := marketTalkLocked(rows, now)
	if line != "" {
		opps.lastSay, opps.saidItem[item] = now, now
		opps.n["market talk lines"]++
	}
	opps.Unlock()
	if line != "" {
		b.channelSay("#trade", styled(typing, line))
	}
}

// heardRumor (b.mu held): a bot without a money goal that heard a gap on #trade may take it up when a map safe for
// it drops the item (owner: information spreads like on a real server). Returns the new goal or nil.
func (b *Bot) heardRumor(now time.Time) *moneyGoal {
	opps.Lock()
	var ids []int
	for id, t := range opps.rumor {
		if now.After(t) {
			delete(opps.rumor, id)
			continue
		}
		if chasersLocked(id, now) < oppChaseCap {
			ids = append(ids, id)
		}
	}
	opps.Unlock()
	lv := b.BaseLvOr()
	for _, id := range ids {
		ms, mob := b.fitFarms(pieceFarms(id, lv, 4), id)
		if len(ms) == 0 {
			continue
		}
		g := &moneyGoal{ItemID: id, Item: itemName(id), Mob: mob, Maps: ms, Until: now.Add(oppGoalLife)}
		opps.Lock()
		opps.goals[b] = g
		if opps.chase[id] == nil {
			opps.chase[id] = map[*Bot]time.Time{}
		}
		opps.chase[id][b] = g.Until
		opps.at = time.Time{}
		opps.n["rumors taken up"]++
		opps.Unlock()
		b.event("market", "", fmt.Sprintf("heard on #trade that %s is short / pricey - farming it on %s (%s)", g.Item, ms[0], mob))
		return g
	}
	return nil
}

// moneyGoalOf (b.mu held): the bot's live money goal, nil when none.
func (b *Bot) moneyGoalOf(now time.Time) *moneyGoal {
	opps.Lock()
	defer opps.Unlock()
	g := opps.goals[b]
	if g == nil || now.After(g.Until) {
		return nil
	}
	return g
}

// MoneyFarmGoal (b.mu held): for lfg.go "zeny" goals - the bot's market opportunity as a farm goal (Kind "zeny",
// the item to farm for sale, its safe drop maps), or nil. Call it after a Prontera visit (checkMarket) or any time:
// without a live money goal it picks one now (no event line).
func (b *Bot) MoneyFarmGoal(now time.Time) *farmGoal {
	if fg := b.farmerGoal(now); fg != nil { // farmers.go (WP3): a farmer's market order, when its own checks pass
		return fg
	}
	g := b.moneyGoalOf(now)
	if g == nil {
		g = b.pickOpportunity(now)
		if g == nil {
			return nil
		}
		opps.Lock()
		opps.goals[b] = g
		if opps.chase[g.ItemID] == nil {
			opps.chase[g.ItemID] = map[*Bot]time.Time{}
		}
		opps.chase[g.ItemID][b] = g.Until
		opps.at = time.Time{}
		opps.Unlock()
	}
	return &farmGoal{Kind: "zeny", ItemID: g.ItemID, Item: g.Item, Mob: g.Mob, Map: g.Maps[0], Maps: g.Maps, Set: now, Until: g.Until}
}

// moneyFarmPick (b.mu held, ai.go solo map picker): the money goal's map when it is one of the fitting candidates.
func (b *Bot) moneyFarmPick(cands []string, now time.Time) (string, *moneyGoal) {
	g := b.moneyGoalOf(now)
	if g == nil && b.rng != nil && b.rng.Float64() < 0.25 {
		g = b.heardRumor(now)
	}
	if g == nil {
		return "", nil
	}
	for _, m := range g.Maps {
		if !b.soloMapOKFor(m, true) { // run 9: re-checked at every pick (partyMapTooHard covers the walk in too)
			continue
		}
		for _, c := range cands {
			if c == m {
				return m, g
			}
		}
	}
	return "", nil
}

func registerOpportunities(mux *http.ServeMux) {
	mux.HandleFunc("/api/economy/opportunities", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		rows := marketOpps(now)
		opps.Lock()
		goals := map[string]*moneyGoal{}
		byItem := map[string]int{}
		for b, g := range opps.goals {
			if now.Before(g.Until) {
				goals[b.P.Name] = g
				byItem[g.Item]++
			}
		}
		n := map[string]int{}
		for k, v := range opps.n {
			n[k] = v
		}
		opps.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"items": rows, "goals": len(goals), "goals_by_item": byItem,
			"money_goals": goals, "counts": n})
	})
}
