package main

// farm.go (economy batch 21 step 3b). Owner 2026-11-14: "The economy agents should tell bots to farm where highly
// sought after are available but farmable to get items and sell. Do not sell junks, sell useful items like ss cards,
// katana, etc."
//
//   - mapTradeZeny(m): expected zeny per kill from player-trade goods (cards, weapons/armor players buy, valuable
//     materials) at the market price (priceOf: the dynamic table when on), spawn-weighted, bosses excluded. Added to
//     the NPC loot value (mapFarmValue) where a broke bot picks the richest map (beginner.go, lootvalue.go).
//   - GET /api/economy/farm?lv=N: the best farming targets for a level: sought-after items (buyers > supply in the
//     price table, or worth 1,000z+) that drop from normal monsters of a fitting level, with the map, rate and zeny per
//     kill - for the guild / Ayla minds choosing where a party farms.

import (
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

var tradeZeny = struct {
	sync.Mutex
	m  map[string]float64
	at time.Time
}{m: map[string]float64{}}

// tradeGood: a drop players buy from each other (not NPC junk)
func tradeGood(x *Item) bool {
	// run 8 (owner: healing items first in the market; more bots farm them): HP restorers are trade goods too
	return x != nil && (x.Type == "Card" || x.Type == "Weapon" || x.Type == "Armor" || valuableEtc[x.ID] || healItem(x.ID))
}

// mobTradeZ: expected trade-goods zeny per kill of a monster
func mobTradeZ(mid int) float64 {
	lookupOnce.Do(loadLookup)
	x := mobExtras[mid]
	if x == nil {
		return 0
	}
	z := 0.0
	for _, d := range x.Drops {
		if d.Mvp {
			continue
		}
		if it := items[d.Item]; tradeGood(it) {
			boss := mobs[mid] != nil && mobs[mid].Boss
			// market.go glutMul (run 8): a glutted item is worth little to one more farmer
			z += float64(effDropRate(d.Item, d.Rate, boss)) / 10000 * float64(priceOf(d.Item)) * glutMul(d.Item)
		}
	}
	return z
}

// mapTradeZeny: spawn-weighted trade-goods zeny per kill on a map (cached 15 min: prices drift)
func mapTradeZeny(m string) float64 {
	tradeZeny.Lock()
	if time.Since(tradeZeny.at) > 15*time.Minute {
		tradeZeny.m, tradeZeny.at = map[string]float64{}, time.Now()
	}
	if v, ok := tradeZeny.m[m]; ok {
		tradeZeny.Unlock()
		return v
	}
	tradeZeny.Unlock()
	tot, z := 0, 0.0
	for _, s := range spawnsByMap[m] {
		mb := mobs[s.MobID]
		if mb == nil || mb.Boss || s.Count <= 0 {
			continue
		}
		tot += s.Count
		z += mobTradeZ(s.MobID) * float64(s.Count)
	}
	v := 0.0
	if tot > 0 {
		v = z / float64(tot)
	}
	tradeZeny.Lock()
	tradeZeny.m[m] = v
	tradeZeny.Unlock()
	return v
}

// mapFarmValue: NPC loot plus trade goods per kill
func mapFarmValue(m string) float64 { return mapLootZeny(m) + mapTradeZeny(m) }

type farmTarget struct {
	Item      string  `json:"item"`
	ItemID    int     `json:"item_id"`
	Price     int     `json:"price"`
	Buyers    int     `json:"buyers"`
	Supply    float64 `json:"supply"`
	Monster   string  `json:"monster"`
	MobLv     int     `json:"mob_lv"`
	Map       string  `json:"map"`
	Count     int     `json:"spawn_count"`
	RatePct   float64 `json:"rate_pct"`
	ZenyKill  float64 `json:"item_zeny_per_kill"`
	MapZeny   float64 `json:"map_farm_zeny_per_kill"`
	MapMeanLv int     `json:"map_mean_lv"`
}

// farmTargets: sought-after drops farmable at a level (normal monsters within -10..+5 levels)
func farmTargets(lv, limit int) []farmTarget {
	lookupOnce.Do(loadLookup)
	dynPrices.RLock()
	rows := dynPrices.rows
	dynPrices.RUnlock()
	want := map[int]dynRow{}
	for _, r := range rows {
		if !r.BossOnly && (float64(r.Buyers) > float64(r.Held)+r.DropsDay || r.Price >= 1000) {
			want[r.ID] = r
		}
	}
	var out []farmTarget
	for id, r := range want {
		for _, mid := range droppedBy[id] {
			mb, x := mobs[mid], mobExtras[mid]
			if mb == nil || x == nil || mb.Boss || x.Mvp || mb.Level > lv+5 || mb.Level < lv-10 {
				continue
			}
			rate := 0
			for _, d := range x.Drops {
				if d.Item == id && !d.Mvp && effDropRate(d.Item, d.Rate, mb.Boss) > rate {
					rate = effDropRate(d.Item, d.Rate, mb.Boss) // droprate.go: the server's rate
				}
			}
			for _, s := range mobMaps[mid] {
				if s.Count < 5 || isTown(s.Map) {
					continue
				}
				price := int(float64(priceOf(id)) * glutMul(id)) // market.go: a glut lowers what farming it pays
				out = append(out, farmTarget{Item: r.Name, ItemID: id, Price: price, Buyers: r.Buyers, Supply: float64(r.Held) + r.DropsDay,
					Monster: mb.Name, MobLv: mb.Level, Map: s.Map, Count: s.Count, RatePct: float64(rate) / 100,
					ZenyKill: float64(rate) / 10000 * float64(price), MapZeny: mapFarmValue(s.Map), MapMeanLv: mapMeanLv(s.Map)})
			}
		}
	}
	// best by item zeny per kill x how much of the map is that monster
	sort.Slice(out, func(i, j int) bool {
		return out[i].ZenyKill*float64(out[i].Count) > out[j].ZenyKill*float64(out[j].Count)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func registerFarm(mux *http.ServeMux) {
	mux.HandleFunc("/api/economy/farm", func(w http.ResponseWriter, r *http.Request) {
		lv, _ := strconv.Atoi(r.URL.Query().Get("lv"))
		if lv <= 0 {
			lv = 40
		}
		writeJSON(w, map[string]interface{}{"lv": lv, "targets": farmTargets(lv, 25),
			"note": "sought-after drops (buyers > supply, or 1,000z+) from normal monsters lv-10..lv+5; pick a map that also fits the party (danger, mean level)"})
	})
}
