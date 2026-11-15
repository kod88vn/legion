package main

import (
	"bufio"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// ---------------------------------------------------------------- hunting-map fit: ammunition, cards
// QA 2026-11-15 (b1794773273mfy, Ayla's party in Payon Cave): the hunter fired plain Arrows at Undead, the wizard
// cast a water spell (Undead take no damage from Water), the priest's staff carried a crit card. Ammunition and
// element / race card bonuses are valued by the monsters of the map the bot hunts (the spawn list of that map, its
// monsters' elements and races from mob_db), not by the item alone. Shares are of the spawn count; a monster without
// an element or race counts as neutral. Behind tuning fit_buy_off (true = the plain pre-fit behaviour).

// fitBuyOn: the element / race fit of ammunition, cards and potions is in force (tuning fit_buy_off false)
func fitBuyOn() bool { return !tune().FitBuyOff }

type huntMix struct {
	ele, race map[string]float64 // share of the map's spawns per monster element / race (lower-case keys)
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
			ele[strings.ToLower(mb.Element)] += n
		}
		if mb.Race != "" {
			race[strings.ToLower(mb.Race)] += n // mob_db spells "Demihuman"; card scripts "DemiHuman"
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

// huntKnown: the spawn list of map m is loaded (without it nothing can be fitted to the monsters there)
func huntKnown(m string) bool { return len(spawnsByMap[m]) > 0 }

// huntMap: the map this bot hunts on (its goal's map, else where it stands)
func (b *Bot) huntMap() string {
	if b.Goal.Map != "" {
		return b.Goal.Map
	}
	return b.MapName
}

// ammoEle: arrow item id -> the element its item script adds to a bow attack (bonus bAtkEle,Ele_X;), read from the
// server's item_db_etc.yml by loadAmmoElements. By id and script, never by name (1755 is "Arrow of Wind").
var ammoEle = map[int]string{}

var (
	ammoEleMu sync.Mutex
	reAtkEle  = regexp.MustCompile(`^bonus bAtkEle,Ele_(\w+);`)
	reIDLine  = regexp.MustCompile(`^- Id: (\d+)$`)
)

// loadAmmoElements reads the bAtkEle script of every Ammo item in a rAthena item DB file (items must be loaded first)
func loadAmmoElements(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	ammoEleMu.Lock()
	defer ammoEleMu.Unlock()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	cur := 0
	for sc.Scan() {
		t := strings.TrimSpace(sc.Text())
		if m := reIDLine.FindStringSubmatch(t); m != nil {
			cur, _ = strconv.Atoi(m[1])
			continue
		}
		if m := reAtkEle.FindStringSubmatch(t); m != nil && cur > 0 {
			if it := items[cur]; it != nil && it.Type == "Ammo" {
				ammoEle[cur] = m[1]
			}
		}
	}
}

// ammoFit: the expected damage multiplier of an arrow item against the monsters of map m (attr_fix level 1 through
// elemMul). A plain Arrow, an unknown map, or the flag off: 1.0
func ammoFit(id int, m string) float64 {
	if !fitBuyOn() {
		return 1
	}
	cardBonOnce.Do(loadCardBonuses)
	ele := ammoEle[id]
	if ele == "" || !huntKnown(m) {
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

// arrowChoice: the arrow a restock buys from a shop's sell list (pk) for hunting map m. An elemental arrow is bought only
// when its fit gain pays for its extra price: each extra zeny per arrow must buy at least tuning arrow_gain_per_z percent
// of damage (default 4: a Silver Arrow at 5z over a plain 1z needs 16%). Plain Arrows (itemArrow) otherwise. Returns the
// item id and its listed price (0 = not listed).
func arrowChoice(pk []byte, m string, zeny int) (int, int) {
	plain := listedPrice(pk, itemArrow)
	bestID, bestPr, bestGain := itemArrow, plain, 0.0
	if !fitBuyOn() {
		return bestID, bestPr
	}
	cardBonOnce.Do(loadCardBonuses)
	per := tune().ArrowGainPerZ
	if per <= 0 {
		per = 4
	}
	base := plain
	if base <= 0 {
		base = arrowPrice()
	}
	for o := 4; o+13 <= len(pk); o += 13 {
		pr, id := int(le.Uint32(pk[o+4:])), int(le.Uint32(pk[o+9:]))
		if pr <= 0 || id == itemArrow || !isArrow(id) || ammoEle[id] == "" || (zeny-arrowReserve)/pr < minArrowTrip {
			continue
		}
		gain := ammoFit(id, m) - 1
		extra := max(0, pr-base)
		if gain <= 0 || gain*100 < float64(extra*per) {
			continue
		}
		if gain > bestGain {
			bestID, bestPr, bestGain = id, pr, gain
		}
	}
	return bestID, bestPr
}

// elemArrowCap: most elemental arrows a single restock buys (the rest of the stock is plain Arrows)
const elemArrowCap = 200

// shareOf: the share of the map's monsters a bonus key hits; "all" (RC_All, Ele_All) hits every monster
func shareOf(m map[string]float64, key string) float64 {
	if key == "all" {
		return 1
	}
	return m[key]
}

// huntFitValue: the value of a card's or gear piece's element / race damage bonuses against the monsters of the bot's
// hunting map: each bonus times the share of spawns it hits, x2 (a +20% Undead card on an all-Undead map is worth about
// a +4 STR card to a STR build). 0 when the bonuses hit nothing there, or the flag is off.
func (b *Bot) huntFitValue(cb *cardBonus) int {
	if !fitBuyOn() || (len(cb.ele) == 0 && len(cb.race) == 0) {
		return 0
	}
	ele, race := huntShares(b.huntMap())
	f := 0.0
	for e, n := range cb.ele {
		f += float64(n) * shareOf(ele, e)
	}
	for r, n := range cb.race {
		f += float64(n) * shareOf(race, r)
	}
	return int(f * 2)
}
