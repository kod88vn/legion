package main

// Loot value of a hunting ground (economy, 2026-11-14): with EXP 10x and drops 1x, zeny comes only from loot sold to
// NPCs, and bots got poorer as they levelled (lv0-9 ~2,200z, lv20-29 ~1,000z). A bot short of money picks, among the
// equally good nearby hunting grounds, the one whose monsters drop the most NPC-sellable loot per kill
// (mob_db Drops x the item's NPC sell price; e.g. pay_fild01 ~14z/kill vs prt_fild06 ~5z at lv5-8, pay_fild02 ~36z
// vs pay_fild08 ~20z at lv18).

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var (
	lootOnce   sync.Once
	mobLootZ   map[int]float64  // mob id -> expected NPC zeny per kill (Etc drops, base rates; see mobLootValue)
	mobLootDr  map[int][][2]int // mob id -> Etc drops (item, base rate)
	mapLootMu  sync.Mutex
	mapLootMem = map[string]float64{}
	mapLootSig float64
)

// mobLootValue (run 8): expected NPC zeny per kill at the server's drop rates (droprate.go, each drop capped at 100%)
func mobLootValue(mid int) float64 {
	lootOnce.Do(loadMobLoot)
	boss := false
	if mb := mobs[mid]; mb != nil {
		boss = mb.Boss
	}
	z := 0.0
	for _, d := range mobLootDr[mid] {
		z += float64(sellPrice(d[0])) * float64(effDropRate(d[0], d[1], boss)) / 10000
	}
	return z
}

func loadMobLoot() {
	mobLootZ, mobLootDr = map[int]float64{}, map[int][][2]int{}
	byAegis := map[string]*Item{}
	for _, it := range items {
		if it.Aegis != "" {
			byAegis[it.Aegis] = it
		}
	}
	f, err := os.Open(filepath.Join(*rathena, "db", "re", "mob_db.yml"))
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	id, inDrops := 0, false
	var item *Item
	for sc.Scan() {
		l := sc.Text()
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "- Id:") && !strings.HasPrefix(l, "      "):
			v := strings.TrimSpace(t[5:])
			if i := strings.IndexByte(v, '#'); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
			id, _ = strconv.Atoi(v)
			inDrops, item = false, nil
			if _, seen := mobLootZ[id]; seen {
				id = 0 // the first entry wins (later ones are other modes of the same id)
			}
		case strings.HasPrefix(l, "    ") && !strings.HasPrefix(l, "      "):
			inDrops = t == "Drops:"
		case inDrops && strings.HasPrefix(t, "- Item:"):
			item = byAegis[strings.TrimSpace(t[7:])]
		case inDrops && strings.HasPrefix(t, "Rate:") && item != nil && id > 0:
			r, _ := strconv.Atoi(strings.TrimSpace(t[5:]))
			if item.Type == "Etc" {
				mobLootZ[id] += float64(sellPrice(item.ID)) * float64(r) / 10000
				mobLootDr[id] = append(mobLootDr[id], [2]int{item.ID, r})
			}
			item = nil
		}
	}
}

// mapLootZeny: expected NPC zeny per kill on a map (spawn-weighted, bosses excluded).
func mapLootZeny(m string) float64 {
	lootOnce.Do(loadMobLoot)
	mapLootMu.Lock()
	defer mapLootMu.Unlock()
	if sg := dropSig(); sg != mapLootSig {
		mapLootMem, mapLootSig = map[string]float64{}, sg
	}
	if v, ok := mapLootMem[m]; ok {
		return v
	}
	tot, z := 0, 0.0
	for _, s := range spawnsByMap[m] {
		mb := mobs[s.MobID]
		if mb == nil || mb.Boss || s.Count <= 0 {
			continue
		}
		tot += s.Count
		z += mobLootValue(s.MobID) * float64(s.Count)
	}
	v := 0.0
	if tot > 0 {
		v = z / float64(tot)
	}
	mapLootMem[m] = v
	return v
}

// shortOfZeny: a bot that should hunt for money as well as EXP (b.mu held). The guild alchemist saving for the 100k
// Potion Creation Guide counts too (economy batch 13): its own loot is part of the guild's fund.
func (b *Bot) shortOfZeny() bool {
	return b.Zeny < 500+b.BaseLvOr()*80 || b.needsGuide()
}

// richestLoot: of the candidate maps, the one with the most loot zeny per kill ("" if none).
func richestLoot(ms []string) string {
	best, bz := "", -1.0
	for _, m := range ms {
		if z := mapFarmValue(m); z > bz { // farm.go (batch 21): trade goods at market price count too
			best, bz = m, z
		}
	}
	return best
}

// heavyLootMinZ: over the regen line (50% weight) a bot still picks up NPC junk that sells for at least this much each
const heavyLootMinZ = 100

// heavyJunk: a drop a bot over 50% weight leaves on the ground: NPC junk (npcJunk: never cards, gear, valuable
// materials or potions) selling for under heavyLootMinZ. Lesson (QA 2026-11-14 05:40): 18 bots at 50-69% weight kept
// looting Jellopy and Fluff for 4+ min (lilyukiheart 62->67%): natural regen stops at 50% while the town trip waits for
// trip_bag_pct 70. Leaving cheap junk keeps them near the regen line; only valuable drops push them to the real-need 70%.
func heavyJunk(id int) bool {
	x := items[id]
	return x != nil && npcJunk(x) && sellPrice(id) < heavyLootMinZ
}

// spareGearFull (economy batch 16): the bag already holds enough unequipped copies of this weapon/armor (1; a vendor
// keeps 3 for its shop). Weapons and armor never go to an NPC (owner rule), so a field bot that looted every Club and
// Knife carried them forever: 06:30 heavy bots held ~7 Clubs and 3 Knives each (Taco_senpai 18 Clubs), 4-18% of
// their weight limit. One spare stays for trade (econSpare offers it); the rest is left on the ground. b.mu held.
func (b *Bot) spareGearFull(id int) bool {
	x := items[id]
	if x == nil || (x.Type != "Weapon" && x.Type != "Armor") {
		return false
	}
	limit := 1
	if b.charRole() == "vendor" {
		limit = 3
	}
	n := 0
	for _, it := range b.inv {
		if it.ItemID == id && !it.Equipped && it.Count > 0 {
			n += it.Count
		}
	}
	return n >= limit
}
