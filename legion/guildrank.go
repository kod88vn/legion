package main

// guildrank.go (redesign package G): guilds compete on rank and wealth (owner, RULES.md 3b: "guilds compete. Guild
// leaders are frienemies" / "rank and wealth, we will introduce competition like guild war later"). Computed every
// 10 min whatever guild_duty_on says, one history row per guild per hour (7 days kept, data/guild_rank.json):
//   rank   = guild level x 10 + average level of its 10 best members + leaderboard points (guildduty_logic.go)
//   wealth = market value of what its vendors list in their shops + guild funds (the leader's and its vendors' zeny)
// Relations between guilds (-1 rival .. +1 ally) drift with events - a member moving from one guild to the other
// within hours (poaching) sours it, fair trades and gifts between members warm it - and fade toward neutral without
// events. For now they only feed briefings and chat tone (GET /api/guild-relations).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type guildRankRow struct {
	Guild    string    `json:"guild"`
	Name     string    `json:"name"`
	T        time.Time `json:"t"`
	Place    int       `json:"place"`
	Score    float64   `json:"score"`
	Wealth   int       `json:"wealth"`
	Stock    int       `json:"stock_value"`
	Funds    int       `json:"funds"`
	Level    int       `json:"guild_level"`
	Members  int       `json:"members"`
	AvgTop   float64   `json:"avg_top10_level"`
	TopPlace []int     `json:"top100_places,omitempty"`
}

type guildRelation struct {
	A       string    `json:"a"`
	B       string    `json:"b"`
	V       float64   `json:"value"`
	Events  int       `json:"events"`
	LastWhy string    `json:"last_event,omitempty"`
	LastAt  time.Time `json:"last_at,omitempty"`
	Drift   time.Time `json:"drifted_at"`
}

var grank = struct {
	sync.Mutex
	cur       map[string]*guildRankRow
	hist      map[string][]*guildRankRow
	histAt    time.Time
	funds     map[string][2]int // char -> {zeny, last seen unix} for leaders / vendors (offline ones keep their last purse)
	fundGuild map[string]string // char -> guild id
	owner     map[string]string // shop owner char -> guild id, remembered: its shop still counts while the owner is offline
	tradeSeen time.Time
	dirty     bool
	loaded    bool
}{cur: map[string]*guildRankRow{}, hist: map[string][]*guildRankRow{}, funds: map[string][2]int{}, fundGuild: map[string]string{},
	owner: map[string]string{}}

var grel = struct {
	sync.Mutex
	m map[string]*guildRelation
}{m: map[string]*guildRelation{}}

// relNote: an event between two guilds (any lock order: grel is a leaf lock)
func relNote(a, b string, delta float64, why string) {
	if a == "" || b == "" || a == b {
		return
	}
	now := time.Now()
	grel.Lock()
	k := relKey(a, b)
	r := grel.m[k]
	if r == nil {
		x, y := a, b
		if x > y {
			x, y = y, x
		}
		r = &guildRelation{A: x, B: y, Drift: now}
		grel.m[k] = r
	}
	r.V = relApply(relDrift(r.V, now.Sub(r.Drift).Hours()/24), delta)
	r.Drift, r.LastAt, r.LastWhy = now, now, why
	r.Events++
	grel.Unlock()
	grank.Lock() // after grel is released: guildRankSave takes grank, then grel
	grank.dirty = true
	grank.Unlock()
}

// relationsFromTrades: player trades between members of two guilds (economy.go trade log) warm the relation
func relationsFromTrades(now time.Time) {
	gduty.Lock()
	cg := gduty.charGuild // replaced wholesale every tick, never mutated: safe to read after the unlock
	gduty.Unlock()
	grank.Lock()
	since := grank.tradeSeen
	grank.tradeSeen = now
	grank.Unlock()
	if since.IsZero() {
		return // first round: start from now (the log holds trades from before the restart too)
	}
	econ.mu.Lock()
	var ts []Trade
	for _, t := range econ.trades {
		if t.T.After(since) {
			ts = append(ts, t)
		}
	}
	econ.mu.Unlock()
	for _, t := range ts {
		a, b := cg[t.Seller], cg[t.Buyer]
		if a == "" || b == "" || a == b {
			continue
		}
		if t.Price <= 0 {
			relNote(a, b, relGift, "gift: "+t.Item+" "+t.Seller+" -> "+t.Buyer)
			continue
		}
		std := priceOf(t.ItemID)
		each := t.Price / max(t.Count, 1)
		if std > 0 && each*10 >= std*6 && each*10 <= std*15 {
			relNote(a, b, relFairTrade, "fair trade: "+t.Item+" "+t.Seller+" -> "+t.Buyer)
		}
	}
}

// rankShop: one open shop and its listings, copied out of econ.shops
type rankShop struct {
	name  string
	items []vendListing
}

// rankStockOf (pure): market value of the open shops of each guild; owner maps a shop's character to its guild
func rankStockOf(shops []rankShop, owner map[string]string, price func(int) int) map[string]int {
	stock := map[string]int{}
	prices := map[int]int{}
	for _, sh := range shops {
		g := owner[sh.name]
		if g == "" {
			continue
		}
		for _, l := range sh.items {
			p, ok := prices[l.ItemID]
			if !ok {
				p = price(l.ItemID)
				prices[l.ItemID] = p
			}
			stock[g] += p * l.Count
		}
	}
	return stock
}

// guildRankRound: rank and wealth of every guild (no bot lock: the snapshot, the level book, guild stats, shops)
func guildRankRound(snaps []*dutySnap, now time.Time, starveil string) {
	if len(snaps) == 0 {
		return // no bot feed (engine start or reset): keep the last round instead of publishing zero wealth
	}
	ids := []string{humanGuildID}
	for _, g := range guildDefs {
		ids = append(ids, g.ID)
	}
	// funds: leaders' and vendors' purses (offline ones keep the last seen purse for a day)
	charGuild := map[string]string{}
	hName, hZeny, hTS, hOK := humanGuildPurse(starveil) // guildrank_human.go: Starveil's master is played outside the engine
	grank.Lock()
	for _, s := range snaps {
		charGuild[s.name] = s.guild
		if s.guild != "" {
			grank.owner[s.name] = s.guild
		}
		if s.guild != "" && (s.vendor || s.vision || s.master) {
			grank.funds[s.name] = [2]int{s.zeny, int(now.Unix())}
			grank.fundGuild[s.name] = s.guild
		}
	}
	// an owner missing from this feed (offline, or not loaded after a restart) keeps its remembered guild
	for c, g := range grank.owner {
		if charGuild[c] == "" {
			charGuild[c] = g
		}
	}
	if hOK {
		grank.funds[hName], grank.fundGuild[hName] = [2]int{hZeny, int(hTS)}, humanGuildID
	}
	funds := map[string]int{}
	for c, f := range grank.funds {
		if now.Unix()-int64(f[1]) > 24*3600 {
			delete(grank.funds, c)
			delete(grank.fundGuild, c)
			continue
		}
		funds[grank.fundGuild[c]] += f[0]
	}
	grank.Unlock()
	// stock value: open shops of the guild's vendors (copied under econ.mu, priced after)
	econ.mu.Lock()
	shops := make([]rankShop, 0, len(econ.shops))
	for _, sh := range econ.shops {
		shops = append(shops, rankShop{sh.name, append([]vendListing(nil), sh.items...)})
	}
	econ.mu.Unlock()
	stock := rankStockOf(shops, charGuild, priceOf)
	// members' levels and leaderboard places from the level book (offline characters included)
	levelBook.Lock()
	es := make([]lvEntry, 0, len(levelBook.m))
	for _, e := range levelBook.m {
		es = append(es, *e)
	}
	levelBook.Unlock()
	sort.Slice(es, func(i, j int) bool {
		if es[i].Level != es[j].Level {
			return es[i].Level > es[j].Level
		}
		return es[i].Exp > es[j].Exp
	})
	lvs := map[string][]int{}
	tops := map[string][]int{}
	for i, e := range es {
		g := dutyGuildID(e.Guild, starveil)
		if g == "" {
			continue
		}
		lvs[g] = append(lvs[g], e.Level)
		if i < 100 {
			tops[g] = append(tops[g], i+1)
		}
	}
	levels := map[string]int{}
	guildStats.Lock()
	for _, id := range ids {
		if s := guildStats.m[strings.ToLower(dutyGuildName(id, starveil))]; s != nil {
			levels[id] = s.Level
		}
	}
	guildStats.Unlock()
	score := map[string]float64{}
	wealth := map[string]int{}
	rows := map[string]*guildRankRow{}
	for _, id := range ids {
		name := dutyGuildName(id, starveil)
		if id == humanGuildID && name == "" {
			continue
		}
		in := rankIn{GuildLevel: levels[id], MemberLvs: lvs[id], TopPlaces: tops[id]}
		r := &guildRankRow{Guild: id, Name: name, T: now, Score: guildRankScore(in), Stock: stock[id], Funds: funds[id],
			Level: levels[id], Members: len(lvs[id]), TopPlace: tops[id]}
		r.Wealth = guildWealth(r.Stock, r.Funds)
		top := append([]int(nil), lvs[id]...)
		sort.Sort(sort.Reverse(sort.IntSlice(top)))
		if len(top) > 10 {
			top = top[:10]
		}
		for _, x := range top {
			r.AvgTop += float64(x)
		}
		if len(top) > 0 {
			r.AvgTop = float64(int(r.AvgTop/float64(len(top))*10)) / 10
		}
		score[id], wealth[id], rows[id] = r.Score, r.Wealth, r
	}
	for id, p := range rankOrder(score, wealth) {
		rows[id].Place = p
	}
	grank.Lock()
	grank.cur = rows
	if now.Sub(grank.histAt) >= time.Hour {
		grank.histAt = now
		for id, r := range rows {
			c := *r
			c.TopPlace = nil
			h := append(grank.hist[id], &c)
			if len(h) > 7*24 {
				h = h[len(h)-7*24:]
			}
			grank.hist[id] = h
		}
	}
	grank.dirty = true
	grank.Unlock()
	// relations fade toward neutral
	grel.Lock()
	for _, r := range grel.m {
		r.V = relDrift(r.V, now.Sub(r.Drift).Hours()/24)
		r.Drift = now
	}
	grel.Unlock()
}

// guildRankView: rank places, wealth and the number of ranked guilds (for the join preference)
func guildRankView() (map[string]int, map[string]int, int) {
	grank.Lock()
	defer grank.Unlock()
	places, wealth := map[string]int{}, map[string]int{}
	for id, r := range grank.cur {
		places[id], wealth[id] = r.Place, r.Wealth
	}
	return places, wealth, len(grank.cur)
}

type guildRankFile struct {
	Saved     time.Time                  `json:"saved"`
	History   map[string][]*guildRankRow `json:"history"`
	HistAt    time.Time                  `json:"hist_at"`
	Relations []*guildRelation           `json:"relations"`
	Funds     map[string][2]int          `json:"funds,omitempty"`
	FundGuild map[string]string          `json:"fund_guild,omitempty"`
	Owners    map[string]string          `json:"owners,omitempty"`
}

func guildRankPath() string { return filepath.Join(*dataDir, "guild_rank.json") }

func guildRankLoad() {
	raw, err := os.ReadFile(guildRankPath())
	grank.Lock()
	grank.loaded = true
	var f guildRankFile
	ok := err == nil && json.Unmarshal(raw, &f) == nil
	if ok {
		for id, h := range f.History {
			grank.hist[id] = h
		}
		grank.histAt = f.HistAt
		// purses and shop owners survive a restart: without them every guild reads 0 until its members are back
		for c, v := range f.Funds {
			grank.funds[c] = v
		}
		for c, g := range f.FundGuild {
			grank.fundGuild[c] = g
		}
		for c, g := range f.Owners {
			grank.owner[c] = g
		}
	}
	grank.Unlock()
	if ok {
		grel.Lock()
		for _, r := range f.Relations {
			grel.m[relKey(r.A, r.B)] = r
		}
		grel.Unlock()
	}
}

// guildRankSave: marshal under the locks, write after
func guildRankSave() {
	grank.Lock()
	if !grank.dirty || !grank.loaded {
		grank.Unlock()
		return
	}
	f := guildRankFile{Saved: time.Now(), History: grank.hist, HistAt: grank.histAt, Funds: grank.funds,
		FundGuild: grank.fundGuild, Owners: grank.owner}
	grank.dirty = false
	grel.Lock()
	for _, r := range grel.m {
		f.Relations = append(f.Relations, r)
	}
	raw, err := json.Marshal(f)
	grel.Unlock()
	grank.Unlock()
	if err == nil {
		writeFileAtomic(guildRankPath(), raw)
	}
}
