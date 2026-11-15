package main

// market.go (economy batch 21, step 2b). Owner 2026-11-14: "economy agent should also know basic market supply and
// demand rules, more availability means cheaper" and "if something is not selling for a while, gradually lower its
// price."
//
//   - Supply and demand (marketTick, every 2 min from econRound): per item, units offered (#trade spares + shop
//     listings) against units wanted (open wants). Buyers outnumber offers -> the item's market factor rises 4% a tick
//     (max 2.0x; 1.0 for NPC-sold goods: nobody pays over the NPC); offers more than twice the demand -> it falls 3% a
//     tick (min 0.6x); otherwise it drifts back toward 1. priceOf = reference (standard / dynamic table, nudged by
//     trades) x factor, never below the NPC sell price.
//   - Undercutting (vend.go vendPrice): a vendor lists 2% under the lowest other shop's ask for the same item.
//   - Markdown (askPrice, vendPrice): a seller's item listed 45+ min without selling loses 5% per 30 min (max 6 steps,
//     ~26%), never below the NPC sell price, and valuable goods (cards, drop-only gear, materials) not below half the
//     reference. A sale (trade or shop) resets the seller's markdown; a buyer's want for the item resets everyone's.
//   - GET /api/economy/pricing "market": reference, factor, ask range, supply, demand per item; "markdowns": recent.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type mdEntry struct {
	since time.Time
	steps int
	seen  time.Time // last asked (pruned after 3 h: the seller stopped listing it)
}

// markdown persistence (economy run 8): the clock lived in memory, and a Legion deploy every ~30 min reset it before
// the 45-min mark - unsold items were never marked down. Saved with marketTick, loaded at start (data/markdowns*.json).
type mdSaved struct {
	Since time.Time `json:"since"`
	Steps int       `json:"steps"`
	Seen  time.Time `json:"seen"`
}

func markdownFile() string {
	if coreInstance() {
		return filepath.Join(*dataDir, "markdowns_core.json")
	}
	return filepath.Join(*dataDir, "markdowns.json")
}

// loadMarkdowns (start-up): the markdown clocks of the last run
func loadMarkdowns() {
	raw, err := os.ReadFile(markdownFile())
	if err != nil {
		return
	}
	var m map[string]mdSaved
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	market2.Lock()
	for k, v := range m {
		if time.Since(v.Seen) < 3*time.Hour {
			market2.md[k] = &mdEntry{since: v.Since, steps: v.Steps, seen: v.Seen}
		}
	}
	market2.Unlock()
}

// saveMarkdownsLocked (market2 held): write the clocks, dropping ones not asked for 3 h
func saveMarkdownsLocked(now time.Time) {
	m := map[string]mdSaved{}
	for k, e := range market2.md {
		if now.Sub(e.seen) > 3*time.Hour && !e.seen.IsZero() {
			delete(market2.md, k)
			continue
		}
		m[k] = mdSaved{e.since, e.steps, e.seen}
	}
	if raw, err := json.Marshal(m); err == nil {
		go os.WriteFile(markdownFile(), raw, 0o644)
	}
}

type mdLog struct {
	T    time.Time `json:"t"`
	Who  string    `json:"seller"`
	Item string    `json:"item"`
	Old  int       `json:"old"`
	New  int       `json:"new"`
}

type mktRow struct {
	ID        int     `json:"id"`
	Item      string  `json:"item"`
	Reference int     `json:"reference"`
	Factor    float64 `json:"factor"`
	Price     int     `json:"price"`
	AskLow    int     `json:"ask_low"`
	AskHigh   int     `json:"ask_high"`
	Supply    int     `json:"supply"`
	Demand    int     `json:"demand"`
}

var market2 = struct {
	sync.Mutex
	factor   map[int]float64
	md       map[string]*mdEntry // "persona|item"
	mdLogs   []mdLog
	rows     []mktRow
	lastTick time.Time
	glut     map[int]float64   // run 8: item -> value multiplier for farming (1 = no glut, down to 0.1)
	glutSaid map[int]time.Time // a "so many X" line on #trade (opportunity.go) deepens the glut for 20 min
	supply   map[int]int       // run 10: units on offer (shops + #trade board) at the last tick
	demand   map[int]int       // run 10: units buyers ask for (and can pay) at the last tick
}{factor: map[int]float64{}, md: map[string]*mdEntry{}, glut: map[int]float64{}, glutSaid: map[int]time.Time{}}

// marketSupply (run 10): units of the item on offer (open shops + the #trade board) at the last market tick; -1 before
// the first tick
func marketSupply(id int) int {
	market2.Lock()
	defer market2.Unlock()
	if market2.supply == nil {
		return -1
	}
	return market2.supply[id]
}

// marketDemand (run 10): units of the item buyers ask for (and can pay) at the last market tick
func marketDemand(id int) int {
	market2.Lock()
	defer market2.Unlock()
	return market2.demand[id]
}

// glutMul (economy run 8, owner: "if there's a surplus of something and price is too low, market should adjust and
// farm something else"): what one more unit of the item is worth to a farmer, relative to its price. Units on offer
// (shops + #trade board) far above what buyers ask for -> down to 0.1; recovers as the surplus sells (each marketTick).
func glutMul(id int) float64 {
	market2.Lock()
	defer market2.Unlock()
	g, ok := market2.glut[id]
	if !ok {
		g = 1
	}
	if t, ok := market2.glutSaid[id]; ok && time.Since(t) < 20*time.Minute {
		g *= 0.7
	}
	return g
}

// glutted: farmers should pick something else; vendors list only a few
func glutted(id int) bool { return glutMul(id) < 0.5 }

// noteGlutSaid (opportunity.go market talk): a glut line was said on #trade
func noteGlutSaid(id int) {
	market2.Lock()
	market2.glutSaid[id] = time.Now()
	market2.Unlock()
}

// marketFactor: the item's supply/demand factor (1 = balanced)
func marketFactor(id int) float64 {
	market2.Lock()
	defer market2.Unlock()
	if f, ok := market2.factor[id]; ok {
		return f
	}
	return 1
}

// marketFloor: the lowest price an ask may reach
func marketFloor(id, ref int) int {
	fl := max(sellPrice(id), 1)
	if x := items[id]; x != nil && (x.Type == "Card" || valuableEtc[id] || (x.Type == "Weapon" || x.Type == "Armor") && len(gearShops(id)) == 0) {
		fl = max(fl, ref/2)
	}
	return fl
}

// markdown: the price after this seller's markdown steps for an item listed a long time without selling
func markdown(seller string, id, price, ref int) int {
	if seller == "" || price <= 0 {
		return price
	}
	k := seller + "|" + fmt.Sprint(id)
	now := time.Now()
	market2.Lock()
	e := market2.md[k]
	if e == nil {
		e = &mdEntry{since: now}
		market2.md[k] = e
	}
	e.seen = now
	steps := 0
	if age := now.Sub(e.since); age > 45*time.Minute {
		steps = min(6, 1+int((age-45*time.Minute)/(30*time.Minute)))
	}
	newP := price
	for i := 0; i < steps; i++ {
		newP = newP * 95 / 100
	}
	newP = max(newP, marketFloor(id, ref))
	if steps > e.steps {
		e.steps = steps
		old := price
		for i := 0; i < steps-1; i++ {
			old = old * 95 / 100
		}
		market2.mdLogs = append(market2.mdLogs, mdLog{now, seller, itemName(id), max(old, newP), newP})
		if len(market2.mdLogs) > 60 {
			market2.mdLogs = market2.mdLogs[len(market2.mdLogs)-60:]
		}
		market2.Unlock()
		log.Printf("markdown: %s %s %d->%d (%s)", itemName(id), seller, old, newP, "unsold")
		econCount("markdown")
		return newP
	}
	market2.Unlock()
	return newP
}

// marketSold: a sale resets the seller's markdown for the item
func marketSold(seller string, id int) {
	market2.Lock()
	delete(market2.md, seller+"|"+fmt.Sprint(id))
	market2.Unlock()
}

// marketTick: supply vs demand per item (econRound, econ.mu NOT held), at most every 2 min
var mdLoadOnce sync.Once

func marketTick(now time.Time) {
	mdLoadOnce.Do(loadMarkdowns)
	market2.Lock()
	if now.Sub(market2.lastTick) < 2*time.Minute {
		market2.Unlock()
		return
	}
	market2.lastTick = now
	saveMarkdownsLocked(now)
	market2.Unlock()

	supply, demand := map[int]int{}, map[int]int{}
	lo, hi := map[int]int{}, map[int]int{}
	ask := func(id, p int) {
		if p <= 0 {
			return
		}
		if lo[id] == 0 || p < lo[id] {
			lo[id] = p
		}
		hi[id] = max(hi[id], p)
	}
	econ.mu.Lock()
	for _, o := range econ.offers {
		supply[o.ItemID] += o.Qty
		ask(o.ItemID, o.LowAsk)
	}
	for _, sh := range econ.shops {
		for _, l := range sh.items {
			if l.Count > 0 {
				supply[l.ItemID] += l.Count
				ask(l.ItemID, l.Price)
			}
		}
	}
	for _, w := range econ.wants {
		// run 7: only the units the buyer can pay for are demand. Guild-leader party members ask for Blue Potions with
		// no money (their alchemist gifts some), and those bids (priceOf x 1.15+) fed the factor, which raised priceOf,
		// which raised the bids: 18 buyers with 80-1,200z bid 8,100-8,950z each at 15:50 (reference 5,000).
		n := max(w.Count, 1)
		if w.Buyer != nil && w.Each > 0 {
			n = min(n, w.Buyer.Zeny/w.Each) // read without the bot's lock: an int snapshot is fine here
		}
		if n > 0 {
			demand[w.ItemID] += n
		}
	}
	econ.mu.Unlock()

	ids := map[int]bool{}
	for id := range supply {
		ids[id] = true
	}
	for id := range demand {
		ids[id] = true
	}
	market2.Lock()
	market2.supply, market2.demand = supply, demand
	for id := range demand { // demand showed up: everyone's markdown for it starts over
		suffix := "|" + fmt.Sprint(id)
		for k := range market2.md {
			if len(k) > len(suffix) && k[len(k)-len(suffix):] == suffix {
				delete(market2.md, k)
			}
		}
	}
	for id, f := range market2.factor { // items gone from the market drift home
		if !ids[id] {
			f += (1 - f) * 0.1
			if f > 0.99 && f < 1.01 {
				delete(market2.factor, id)
			} else {
				market2.factor[id] = f
			}
		}
	}
	var rows []mktRow
	for id := range ids {
		f, ok := market2.factor[id]
		if !ok {
			f = 1
		}
		s, d := supply[id], demand[id]
		switch {
		case d > s:
			f *= 1.04
		case s > 2*d:
			f *= 0.97
		default:
			f += (1 - f) * 0.05
		}
		ceil := 2.0
		if x := items[id]; x != nil && x.Buy > 0 && len(gearShops(id)) > 0 || npcMatPrice[id] > 0 {
			ceil = 1.0 // an NPC sells it (Dietrich too: Phracon was at x1.53 = 360z vs his 200z): the NPC price is the ceiling
		}
		f = min(max(f, 0.6), ceil)
		market2.factor[id] = f
		// glut: more than 3x the units buyers ask for (+10 slack) on offer -> farming it is worth less
		if lim := 3 * (d + 10); s > lim {
			market2.glut[id] = max(0.1, float64(lim)/float64(s))
		} else {
			delete(market2.glut, id)
		}
		rows = append(rows, mktRow{ID: id, Item: itemName(id), Factor: float64(int(f*100)) / 100, AskLow: lo[id], AskHigh: hi[id],
			Supply: s, Demand: d})
	}
	market2.Unlock()
	for i := range rows { // prices outside market2's lock (priceOf reads the factor)
		rows[i].Reference = standardPrice(rows[i].ID)
		rows[i].Price = priceOf(rows[i].ID)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Supply+rows[i].Demand > rows[j].Supply+rows[j].Demand })
	market2.Lock()
	market2.rows = rows
	market2.Unlock()
}

// marketReport: for /api/economy/pricing
func marketReport() map[string]interface{} {
	market2.Lock()
	defer market2.Unlock()
	rows := market2.rows
	if len(rows) > 150 {
		rows = rows[:150]
	}
	return map[string]interface{}{"items": rows, "markdowns": market2.mdLogs, "listings_tracked": len(market2.md)}
}
