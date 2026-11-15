package main

// pricing.go (economy batch 21, step 2). Owner rules 2026-11-14 (RULES.md section 2):
//   "the economy pricing is not fixed price but overall affordability of the entire server. Rare items should cost a
//    fortune to ensure distribution matches rarity."
//   "if it's an item that only comes from boss, an entire guild must contribute in to purchase."
//
// Every 15 min priceRound measures the money supply (zeny of every online character, by level band and by guild; the
// whole population is estimated from the online share) and prices the trade goods that NPCs don't sell (cards,
// drop-only weapons/armor, slotted variants):
//   supply  = copies in circulation (online bags + carts, so shops too) + a day of drops (drop rate x spawn count x
//             6 respawn cycles an hour x 24, normal monsters only)
//   buyers  = online characters whose build wants it (buildgear.go: the item is on its table for its level, or the card
//             suits its build's stats), richest first; guide savers count with 1k (the fund is not theirs)
//   price   = supply >= buyers: the old standard (a common thing stays cheap);
//             otherwise the purse of the (supply+1)-th richest buyer: only as many players as there are copies can
//             afford one, so the distribution follows the rarity;
//   boss-only items (every spawning source is a boss / MVP, or none spawns at all) cost a guild's pooled wealth: the
//             median guild's online zeny.
// NPC-sold items keep the NPC price (standardPrice). The live trades still nudge it (priceOf: 30% blend).
// Gated: tuning dynamic_prices (default false) switches standardPrice to the table; the table and the money supply are
// always computed and shown at GET /api/economy/pricing.

import (
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type dynRow struct {
	ID       int     `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Standard int     `json:"standard"` // the fixed formula (economy.go standardPrice without the table)
	Price    int     `json:"price"`
	Held     int     `json:"held"`      // copies in online bags and carts
	DropsDay float64 `json:"drops_day"` // expected world drops a day (normal monsters)
	Buyers   int     `json:"buyers"`    // online characters whose build wants it
	BossOnly bool    `json:"boss_only"`
	Rule     string  `json:"rule"`
}

type moneyBand struct {
	Band   string `json:"band"`
	Online int    `json:"online"`
	Total  int64  `json:"total"`
	Median int    `json:"median"`
	P90    int    `json:"p90"`
}

type moneySnap struct {
	T            time.Time   `json:"t"`
	Online       int         `json:"online"`
	OnlineZeny   int64       `json:"online_zeny"`
	EstTotalZeny int64       `json:"est_total_zeny"` // online zeny x profiles / online
	Median       int         `json:"median"`
	Bands        []moneyBand `json:"bands,omitempty"`
	GuildMedian  int64       `json:"guild_median"` // median guild's online pooled zeny
	Guilds       int         `json:"guilds"`
}

var dynPrices = struct {
	sync.RWMutex
	price   map[int]int
	rows    []dynRow
	money   []moneySnap // history, newest last (max 96 = 24 h)
	updated time.Time
}{price: map[int]int{}}

// dynPrice: the table price, when the table has one and dynamic_prices is on
func dynPrice(id int) (int, bool) {
	if !tune().DynamicPrices {
		return 0, false
	}
	dynPrices.RLock()
	defer dynPrices.RUnlock()
	p, ok := dynPrices.price[id]
	return p, ok && p > 0
}

// dropsPerDay: expected drops a day from normal (non-boss) spawns, and whether every spawning source is a boss
func dropsPerDay(id int) (float64, bool) {
	lookupOnce.Do(loadLookup)
	day, normal, any := 0.0, false, false
	for _, mid := range droppedBy[id] {
		m, x := mobs[mid], mobExtras[mid]
		if m == nil || x == nil {
			continue
		}
		n := 0
		for _, s := range mobMaps[mid] {
			n += s.Count
		}
		if n == 0 {
			continue
		}
		any = true
		if m.Boss || x.Mvp {
			continue
		}
		normal = true
		rate := 0
		for _, d := range x.Drops {
			if d.Item == id && !d.Mvp && effDropRate(d.Item, d.Rate, m.Boss) > rate {
				rate = effDropRate(d.Item, d.Rate, m.Boss) // droprate.go: rarity at the server's rates
			}
		}
		day += float64(rate) / 10000 * float64(n) * 6 * 24
	}
	return day, any && !normal || !any
}

type buyerInfo struct {
	build string
	lv    int
	zeny  int
	guild string
}

// priceRound: the money supply and the dynamic price table (no bot lock held by the caller)
func priceRound() {
	bots, _ := allBots.Load().([]*Bot)
	var buyers []buyerInfo
	held := map[int]int{}
	snap := moneySnap{T: time.Now()}
	bandZ := map[int][]int{}
	guildZ := map[string]int64{}
	var all []int
	for _, b := range bots {
		b.mu.Lock()
		if !b.Online {
			b.mu.Unlock()
			continue
		}
		z, lv := b.Zeny, b.BaseLvOr()
		for _, it := range b.inv {
			if it.Count > 0 {
				held[it.ItemID] += it.Count
			}
		}
		for _, it := range b.cart {
			if it.Count > 0 {
				held[it.ItemID] += it.Count
			}
		}
		build := ""
		if p := b.plan(); p != nil {
			build = p.ID
		}
		saver := b.needsGuide()
		vendor := b.charRole() == "vendor"
		guild := b.P.Guild
		b.mu.Unlock()
		snap.Online++
		snap.OnlineZeny += int64(z)
		all = append(all, z)
		bandZ[min(lv/20, 4)] = append(bandZ[min(lv/20, 4)], z)
		if guild != "" {
			guildZ[guild] += int64(z)
		}
		if saver {
			z = min(z, 1000)
		}
		if build != "" && !vendor {
			buyers = append(buyers, buyerInfo{build, lv, z, guild})
		}
	}
	if snap.Online == 0 {
		return
	}
	sort.Ints(all)
	snap.Median = all[len(all)/2]
	if profiles := len(bots); profiles > 0 {
		snap.EstTotalZeny = snap.OnlineZeny * int64(profiles) / int64(snap.Online)
	}
	names := []string{"1-19", "20-39", "40-59", "60-79", "80-99"}
	for i := 0; i < 5; i++ {
		l := bandZ[i]
		if len(l) == 0 {
			continue
		}
		sort.Ints(l)
		var t int64
		for _, z := range l {
			t += int64(z)
		}
		snap.Bands = append(snap.Bands, moneyBand{names[i], len(l), t, l[len(l)/2], l[len(l)*9/10]})
	}
	var gz []int64
	for _, z := range guildZ {
		gz = append(gz, z)
	}
	sort.Slice(gz, func(i, j int) bool { return gz[i] < gz[j] })
	snap.Guilds = len(gz)
	if len(gz) > 0 {
		snap.GuildMedian = gz[len(gz)/2]
	}
	sort.Slice(buyers, func(i, j int) bool { return buyers[i].zeny > buyers[j].zeny })

	// the goods: cards, and weapons/armor no NPC sells, that exist in this world
	lookupOnce.Do(loadLookup)
	// gear sets per build and level band (suitsBuild, precomputed: thousands of items x hundreds of buyers)
	table := buildGearTable()
	gearSet := map[string]map[int]bool{}
	anyGear := map[int]bool{}
	setFor := func(build string, lv int) map[int]bool {
		k := build + "|" + strconv.Itoa(min(lv/20, 4))
		if s, ok := gearSet[k]; ok {
			return s
		}
		s := map[int]bool{}
		rep := min(lv/20, 4)*20 + 19
		for _, gb := range table[build] {
			if rep >= gb.Min-20 && rep <= gb.Max+20 {
				for _, l := range gb.Slots {
					for _, e := range l {
						s[e.ID] = true
					}
				}
				for _, e := range gb.Cards { // the build's cards for its band (not every card with a stat on it)
					s[e.ID] = true
				}
			}
		}
		gearSet[k] = s
		return s
	}
	for _, t := range table {
		for _, gb := range t {
			for _, l := range gb.Slots {
				for _, e := range l {
					anyGear[e.ID] = true
				}
			}
			for _, e := range gb.Cards {
				anyGear[e.ID] = true
			}
		}
	}
	var rows []dynRow
	price := map[int]int{}
	for id, it := range items {
		if it.Type != "Card" && !((it.Type == "Weapon" || it.Type == "Armor") && it.Loc != 0) {
			continue
		}
		if it.Buy > 0 && len(gearShops(id)) > 0 {
			continue // an NPC sells it: NPC price (standardPrice)
		}
		day, bossOnly := dropsPerDay(id)
		if day == 0 && !bossOnly {
			continue
		}
		if len(droppedBy[id]) == 0 && held[id] == 0 {
			continue // nobody can get it: not a trade good
		}
		std := fixedStandardPrice(id)
		r := dynRow{ID: id, Name: it.Name, Type: it.Type, Standard: std, Held: held[id], DropsDay: day, BossOnly: bossOnly}
		var purses []int
		for _, by := range buyers {
			ok := false
			if anyGear[id] {
				ok = setFor(by.build, by.lv)[id]
			}
			// guildstock.go (owner: every bot aims for the Teleport card): every character from lv 15 is a buyer
			ok = ok || id == itemCreamyCard && by.lv >= 15
			if ok {
				purses = append(purses, by.zeny)
			}
		}
		r.Buyers = len(purses)
		supply := float64(r.Held) + day
		floor := max(sellPrice(id), 1)
		switch {
		case bossOnly:
			// a guild's pooled wealth (the old fixed 250k for boss cards was 2x the richest guild's online zeny)
			r.Price, r.Rule = max(int(snap.GuildMedian), 20000), "boss-only: a guild's pooled wealth"
		case len(purses) == 0:
			r.Price, r.Rule = std, "no buyer: standard"
		case supply >= float64(len(purses)):
			// common: what a typical buyer can pay at most (the fixed formula priced high-level gear at 100k+)
			r.Price, r.Rule = max(min(std, purses[len(purses)/2]), floor), "plenty: standard, capped at the median buyer's purse"
		default:
			k := int(supply) // the (supply+1)-th richest buyer's purse
			r.Price, r.Rule = max(purses[k], min(std, purses[len(purses)/2]), floor), "scarce: purse of buyer #"+strconv.Itoa(k+1)
		}
		r.Price = roundPrice(r.Price)
		price[id] = r.Price
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Price > rows[j].Price })
	dynPrices.Lock()
	dynPrices.price, dynPrices.rows, dynPrices.updated = price, rows, time.Now()
	dynPrices.money = append(dynPrices.money, snap)
	if len(dynPrices.money) > 96 {
		dynPrices.money = dynPrices.money[len(dynPrices.money)-96:]
	}
	dynPrices.Unlock()
}

func registerPricing(mux *http.ServeMux) {
	go func() {
		time.Sleep(3 * time.Minute) // bots log in first
		for {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("pricing: priceRound panic: %v", r) // never take the legion down
					}
				}()
				priceRound()
			}()
			time.Sleep(15 * time.Minute)
		}
	}()
	mux.HandleFunc("/api/economy/pricing", func(w http.ResponseWriter, r *http.Request) {
		dynPrices.RLock()
		rows := dynPrices.rows
		money := dynPrices.money
		upd := dynPrices.updated
		dynPrices.RUnlock()
		n := 200
		if q := r.URL.Query().Get("q"); q != "" { // ?q=card / item name part
			var f []dynRow
			for _, x := range rows {
				if strings.Contains(strings.ToLower(x.Name), strings.ToLower(q)) {
					f = append(f, x)
				}
			}
			rows = f
		}
		if r.URL.Query().Get("all") == "1" {
			n = len(rows)
		}
		if len(rows) < n {
			n = len(rows)
		}
		var last *moneySnap
		if len(money) > 0 {
			last = &money[len(money)-1]
		}
		hist := make([]map[string]interface{}, 0, len(money))
		for _, m := range money {
			hist = append(hist, map[string]interface{}{"t": m.T, "online": m.Online, "online_zeny": m.OnlineZeny,
				"est_total_zeny": m.EstTotalZeny, "median": m.Median, "guild_median": m.GuildMedian})
		}
		writeJSON(w, map[string]interface{}{"updated": upd, "dynamic_prices_on": tune().DynamicPrices, "money_supply": last,
			"money_history": hist, "items": len(rows), "top": rows[:n], "market": marketReport()})
	})
}
