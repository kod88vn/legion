package main

import (
	"strings"
	"sync"
)

// ---------------------------------------------------------------- hunting-map fit: ammunition, cards
// QA 2026-11-15 (b1794773273mfy, Ayla's party in Payon Cave): the hunter fired plain Arrows at Undead, the wizard
// cast a water spell (Undead take no damage from Water), the priest's staff carried a crit card. Ammunition and
// element / race card bonuses are valued by the monsters of the map the bot hunts (the spawn list of that map, its
// monsters' elements and races), not by the item alone. Shares are of the spawn count; a monster without an element
// counts as neutral.

type huntMix struct {
	ele, race map[string]float64 // share of the map's spawns per monster element / race
}

var (
	huntMu    sync.Mutex
	huntCache = map[string]huntMix{}
)

// huntShares: the element and race shares of the monsters spawning on map m (cached once the spawn list is known)
func huntShares(m string) (ele, race map[string]float64) {
	huntMu.Lock()
	defer huntMu.Unlock()
	if c, ok := huntCache[m]; ok {
		return c.ele, c.race
	}
	ele, race = map[string]float64{}, map[string]float64{}
	tot := 0.0
	for _, s := range spawnsByMap[m] {
		mb := mobs[s.MobID]
		if mb == nil || s.Count <= 0 {
			continue
		}
		n := float64(s.Count)
		tot += n
		if mb.Element != "" {
			ele[mb.Element] += n
		}
		if mb.Race != "" {
			race[mb.Race] += n
		}
	}
	if tot == 0 {
		return ele, race // nothing known yet: not cached
	}
	for k := range ele {
		ele[k] /= tot
	}
	for k := range race {
		race[k] /= tot
	}
	huntCache[m] = huntMix{ele, race}
	return ele, race
}

// huntMap: the map this bot hunts on (its goal's map, else where it stands)
func (b *Bot) huntMap() string {
	if b.Goal.Map != "" {
		return b.Goal.Map
	}
	return b.MapName
}

// arrowElement: the element an arrow adds to a bow attack, from its item name (rAthena ammo names); "" = none
func arrowElement(name string) string {
	switch {
	case strings.HasPrefix(name, "Fire "):
		return "Fire"
	case strings.HasPrefix(name, "Crystal "):
		return "Water"
	case strings.HasPrefix(name, "Wind "):
		return "Wind"
	case strings.HasPrefix(name, "Stone "):
		return "Earth"
	}
	return ""
}

// ammoFit: the expected damage multiplier of an arrow item against the monsters of map m (elemMul's table; a plain
// Arrow and any neutral monster are 1.0)
func ammoFit(id int, m string) float64 {
	ele := arrowElement(itemName(id))
	if ele == "" {
		return 1
	}
	shares, _ := huntShares(m)
	f, sum := 0.0, 0.0
	for e, s := range shares {
		f += s * elemMul(ele, e)
		sum += s
	}
	return f + (1 - sum) // monsters without an element: neutral
}

// arrowChoice: the arrow a restock buys from a shop's sell list (pk) for hunting map m: the listed arrow that fits its
// monsters best, when the bot can afford a useful stock of it (minArrowTrip); else plain Arrows (itemArrow). Returns the
// item id and its price (0 = not listed).
func arrowChoice(pk []byte, m string, zeny int) (int, int) {
	plain := listedPrice(pk, itemArrow)
	bestID, bestPr, bestFit := itemArrow, plain, 1.0
	for o := 4; o+13 <= len(pk); o += 13 {
		pr, id := int(le.Uint32(pk[o+4:])), int(le.Uint32(pk[o+9:]))
		if pr <= 0 || id == itemArrow || !isArrow(id) || (zeny-arrowReserve)/pr < minArrowTrip {
			continue
		}
		if f := ammoFit(id, m); f > bestFit+0.01 {
			bestID, bestPr, bestFit = id, pr, f
		}
	}
	return bestID, bestPr
}

// huntFitValue: the value of a card's or gear piece's element / race damage bonuses against the monsters of the bot's
// hunting map: each bonus times the share of spawns it hits, x2 (a +20% Undead card on an all-Undead map is worth about
// a +4 STR card to a STR build). 0 when the bonuses hit nothing there.
func (b *Bot) huntFitValue(cb *cardBonus) int {
	if len(cb.ele) == 0 && len(cb.race) == 0 {
		return 0
	}
	ele, race := huntShares(b.huntMap())
	f := 0.0
	for e, n := range cb.ele {
		f += float64(n) * ele[e]
	}
	for r, n := range cb.race {
		f += float64(n) * race[r]
	}
	return int(f * 2)
}
