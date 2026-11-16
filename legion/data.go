package main

// Game data parsed straight from the rAthena tree: monster db, spawns, warps; plus hunting-ground selection.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Mob struct {
	ID         int
	Name       string
	Level, HP  int
	Aggressive bool
	Assist     bool // AI 03 / 07: joins a packmate's fight (Wolf, Peco Peco) - packs of 6-12 on whoever hits one
	Boss       bool
	Champion   bool // AegisName C1_..C5_: Ringleader / Swift / Solid / Furious / Elusive mini-bosses (bossnote.go)
	Element    string
	Race       string // mob_db Race (Undead, Demon, Brute...): card race bonuses (huntfit.go)
	Exp        int    // BaseExp (0: plants / mushrooms - worthless targets)
	Def        int    // Defense (hard DEF; variantskill.go Occult Impaction picks armoured targets)
	WalkSpeed  int    // mob_db WalkSpeed: ms per cell, lower = faster (player default 150); 0 = unknown (marketengine.go)
}

var mobs = map[int]*Mob{}

// worthless: a monster that gives no base EXP and dies to 1-damage hits (Red/Green/... Plant, Black/Red Mushroom:
// Hp 5-10, BaseExp none). QA 2026-11-14: a lv49 Archer spent a third of 4 minutes on Green Plants, firing Double
// Strafe for 2 damage. Players walk past them.
func (m *Mob) worthless() bool { return m != nil && m.Exp == 0 && m.HP > 0 && m.HP <= 20 }

type Spawn struct {
	Map   string
	MobID int
	Count int
}

var spawnsByMap = map[string][]Spawn{}

type Item struct {
	ID     int
	Aegis  string
	Name   string
	Type   string // Healing, Usable, Etc, Card, Weapon, Armor, Ammo...
	Buy    int
	Weight int
	// equipment
	Jobs       map[string]bool // item_db Jobs block (All/Novice/Swordman/Knight/...); nil = everyone
	Loc        uint32          // equip location bits (EQP_*)
	MinLv      int
	Atk        int
	Def        int
	Mdef       int    // MagicDefense (gearscore.go: armor MDEF)
	Matk       int    // MagicAttack (staves, rods): casters' weapon score (builds.go gearScore)
	Sub        string // SubType: Dagger, 1hSword, Staff, Mace, Bow, Katar, Knuckle, Musical, Whip ...
	Slots      int    // card slots (economy.go: slotted gear is worth more)
	WLv        int    // WeaponLevel (refine.go; 0 = not given, 1 for weapons)
	Refineable bool   // refine.go
}

var locBits = map[string]uint32{"Head_Low": 0x1, "Right_Hand": 0x2, "Garment": 0x4, "Left_Accessory": 0x8, "Armor": 0x10,
	"Left_Hand": 0x20, "Shoes": 0x40, "Right_Accessory": 0x80, "Head_Top": 0x100, "Head_Mid": 0x200,
	"Both_Hand": 0x22, "Both_Accessory": 0x88}

var items = map[int]*Item{}

type Shop struct {
	Map   string
	X, Y  int
	Name  string
	ID    string // the full NPC name ("Tool Dealer#gef"): scripts unload / edit shops by it (applyBarterDealers)
	Items []int
}

var shopsByMap = map[string][]*Shop{}

// loadItemDB parses rAthena item_db*.yml files (Id, AegisName, Name, Type, Buy, Weight).
func loadItemDB(paths ...string) {
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		var cur *Item
		block := ""
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			l := sc.Text()
			t := strings.TrimSpace(l)
			switch {
			case strings.HasPrefix(t, "- Id:"):
				id, _ := strconv.Atoi(strings.TrimSpace(t[5:]))
				cur = &Item{ID: id, Type: "Etc"}
				items[id] = cur
				block = ""
			case cur != nil && strings.HasPrefix(l, "      ") && !strings.HasPrefix(l, "        ") && strings.HasSuffix(t, ": true"):
				k := strings.TrimSuffix(t, ": true")
				switch block {
				case "Jobs":
					if cur.Jobs == nil {
						cur.Jobs = map[string]bool{}
					}
					cur.Jobs[k] = true
				case "Locations":
					cur.Loc |= locBits[k]
				}
			case cur != nil && strings.HasPrefix(l, "      ") && !strings.HasPrefix(l, "        ") && strings.HasSuffix(t, ": false") && block == "Jobs":
				if cur.Jobs == nil {
					cur.Jobs = map[string]bool{}
				}
				cur.Jobs["!"+strings.TrimSuffix(t, ": false")] = true
			case cur == nil || !strings.HasPrefix(l, "    ") || strings.HasPrefix(l, "      "):
			case strings.HasSuffix(t, ":") && !strings.Contains(t, " "):
				block = strings.TrimSuffix(t, ":")
			case strings.HasPrefix(t, "EquipLevelMin:"):
				cur.MinLv, _ = strconv.Atoi(strings.TrimSpace(t[14:]))
			case strings.HasPrefix(t, "Attack:"):
				cur.Atk, _ = strconv.Atoi(strings.TrimSpace(t[7:]))
			case strings.HasPrefix(t, "MagicAttack:"):
				cur.Matk, _ = strconv.Atoi(strings.TrimSpace(t[12:]))
			case strings.HasPrefix(t, "SubType:"):
				cur.Sub = strings.TrimSpace(t[8:])
			case strings.HasPrefix(t, "Defense:"):
				cur.Def, _ = strconv.Atoi(strings.TrimSpace(t[8:]))
			case strings.HasPrefix(t, "MagicDefense:"):
				cur.Mdef, _ = strconv.Atoi(strings.TrimSpace(t[13:]))
			case strings.HasPrefix(t, "AegisName:"):
				cur.Aegis = strings.TrimSpace(t[10:])
			case strings.HasPrefix(t, "Name:"):
				cur.Name = strings.TrimSpace(t[5:])
			case strings.HasPrefix(t, "Type:"):
				cur.Type = strings.TrimSpace(t[5:])
			case strings.HasPrefix(t, "WeaponLevel:"):
				cur.WLv, _ = strconv.Atoi(strings.TrimSpace(t[12:]))
			case t == "Refineable: true":
				cur.Refineable = true
			case strings.HasPrefix(t, "Slots:"):
				cur.Slots, _ = strconv.Atoi(strings.TrimSpace(t[6:]))
			case strings.HasPrefix(t, "Buy:"):
				cur.Buy, _ = strconv.Atoi(strings.TrimSpace(t[4:]))
			case strings.HasPrefix(t, "Weight:"):
				cur.Weight, _ = strconv.Atoi(strings.TrimSpace(t[7:]))
			}
		}
		f.Close()
	}
}

// loadItemOverrides (economy run 8): the rathena db-import item_db.yml (bind-mounted at /rathena/db/import) changes
// only the fields it lists - here Buy (Butterfly Wing 1,000 -> 300z, owner 2026-11-14). Applied on top of the base
// db/re items; unknown ids are ignored. The shops' real list price still wins at runtime (town.go wingPriceSeen).
func loadItemOverrides(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	var cur *Item
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		t := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(t, "- Id:"):
			id, _ := strconv.Atoi(strings.TrimSpace(t[5:]))
			cur = items[id]
		case cur != nil && strings.HasPrefix(t, "Buy:"):
			if v, err := strconv.Atoi(strings.TrimSpace(t[4:])); err == nil && v > 0 {
				cur.Buy = v
			}
		}
	}
}

func itemName(id int) string {
	if it := items[id]; it != nil && it.Name != "" {
		return it.Name
	}
	return fmt.Sprintf("item %d", id)
}

// sellPrice is what an NPC pays (half the buy price, as in RO)
func sellPrice(id int) int {
	if it := items[id]; it != nil {
		return it.Buy / 2
	}
	return 1
}

var aggressiveAI = map[string]bool{"04": true, "05": true, "07": true, "08": true, "09": true, "13": true,
	"19": true, "20": true, "21": true, "26": true, "27": true}

func loadMobDB(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var cur *Mob
	ai := ""
	flush := func() {
		if cur != nil {
			if ai == "" {
				ai = "06"
			}
			cur.Aggressive = aggressiveAI[ai]
			cur.Assist = ai == "03" || ai == "07"
			mobs[cur.ID] = cur
		}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "- Id:"):
			flush()
			id, _ := strconv.Atoi(strings.TrimSpace(t[5:]))
			cur = &Mob{ID: id}
			ai = ""
		case cur == nil:
		case strings.HasPrefix(t, "AegisName:"):
			a := strings.TrimSpace(t[10:])
			cur.Champion = len(a) > 3 && a[0] == 'C' && a[1] >= '1' && a[1] <= '5' && a[2] == '_'
		case strings.HasPrefix(t, "Name:") && cur.Name == "":
			cur.Name = strings.TrimSpace(t[5:])
		case strings.HasPrefix(t, "Level:") && strings.HasPrefix(l, "    Level:"):
			cur.Level, _ = strconv.Atoi(strings.TrimSpace(t[6:]))
		case strings.HasPrefix(t, "Hp:") && strings.HasPrefix(l, "    Hp:"):
			cur.HP, _ = strconv.Atoi(strings.TrimSpace(t[3:]))
		case strings.HasPrefix(t, "BaseExp:"):
			cur.Exp, _ = strconv.Atoi(strings.TrimSpace(t[8:]))
		case strings.HasPrefix(t, "Defense:") && strings.HasPrefix(l, "    Defense:"):
			cur.Def, _ = strconv.Atoi(strings.TrimSpace(t[8:]))
		case strings.HasPrefix(t, "WalkSpeed:"):
			cur.WalkSpeed, _ = strconv.Atoi(strings.TrimSpace(t[10:]))
		case strings.HasPrefix(t, "Element:"):
			cur.Element = strings.TrimSpace(t[8:])
		case strings.HasPrefix(t, "Race:"):
			cur.Race = strings.TrimSpace(t[5:])
		case strings.HasPrefix(t, "Ai:"):
			ai = strings.TrimSpace(t[3:])
		case strings.HasPrefix(t, "Class:"):
			c := strings.TrimSpace(t[6:])
			cur.Boss = c == "Boss" || c == "Guardian" || c == "Battlefield"
		case strings.HasPrefix(t, "Mvp") || strings.HasPrefix(t, "MvpExp:"):
			cur.Boss = true
		}
	}
	flush()
	return sc.Err()
}

func npcFiles(root string) []string {
	var out []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, ".txt") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// loadNPCs parses "map,x,y,...<TAB>monster<TAB>name<TAB>id,amount,..." and "...<TAB>warp<TAB>name<TAB>sx,sy,map,x,y".
func loadNPCs(roots ...string) {
	seenWarp := map[string]bool{}
	for _, root := range roots {
		for _, fn := range npcFiles(root) {
			f, err := os.Open(fn)
			if err != nil {
				continue
			}
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				l := sc.Text()
				if strings.HasPrefix(l, "//") {
					continue
				}
				p := strings.Split(l, "\t")
				if len(p) < 4 {
					continue
				}
				head := strings.Split(p[0], ",")
				kind := strings.TrimSpace(p[1])
				switch {
				case kind == "monster" || kind == "boss_monster":
					a := strings.Split(p[3], ",")
					if len(a) < 2 {
						continue
					}
					id, _ := strconv.Atoi(strings.TrimSpace(a[0]))
					n, _ := strconv.Atoi(strings.TrimSpace(a[1]))
					spawnsByMap[head[0]] = append(spawnsByMap[head[0]], Spawn{head[0], id, n})
					if len(a) >= 3 { // bosshunt.go: respawn delay (ms) of a boss / mini-boss spawn
						d, _ := strconv.Atoi(strings.TrimSpace(a[2]))
						boss := kind == "boss_monster" || mobs[id] != nil && mobs[id].Boss
						mini := n <= 2 && d >= 20*60000 // Renewal mini-bosses (Vocal, Eclipse...) are plain monsters on a 30-min timer
						if d > 0 && (boss || mini) {
							noteBossSpawn(id, head[0], d)
						}
					}
					noteSpawnFile(head[0], fn) // lookup.go: dungeon / field
				case kind == "shop" && len(head) >= 3:
					x, _ := strconv.Atoi(head[1])
					y, _ := strconv.Atoi(head[2])
					s := &Shop{Map: head[0], X: x, Y: y, Name: strings.SplitN(p[2], "#", 2)[0], ID: strings.TrimSpace(p[2])}
					for _, e := range strings.Split(p[3], ",")[1:] {
						if id, err := strconv.Atoi(strings.SplitN(strings.TrimSpace(e), ":", 2)[0]); err == nil {
							s.Items = append(s.Items, id)
						}
					}
					shopsByMap[s.Map] = append(shopsByMap[s.Map], s)
				case kind == "warp" || kind == "warp2":
					a := strings.Split(p[3], ",")
					if len(a) < 5 || len(head) < 3 {
						continue
					}
					x, _ := strconv.Atoi(head[1])
					y, _ := strconv.Atoi(head[2])
					tx, _ := strconv.Atoi(strings.TrimSpace(a[3]))
					ty, _ := strconv.Atoi(strings.TrimSpace(a[4]))
					xs, _ := strconv.Atoi(strings.TrimSpace(a[0]))
					ys, _ := strconv.Atoi(strings.TrimSpace(a[1]))
					w := Warp{From: head[0], X: x, Y: y, To: strings.TrimSpace(a[2]), ToX: tx, ToY: ty, XS: xs, YS: ys}
					key := w.From + "," + strconv.Itoa(x) + "," + strconv.Itoa(y)
					if seenWarp[key] {
						continue
					}
					seenWarp[key] = true
					warpsFrom[w.From] = append(warpsFrom[w.From], w)
				}
			}
			f.Close()
		}
	}
}

// HuntSpot is a candidate training map for a level range.
type HuntSpot struct {
	Map          string
	MinLv, MaxLv int
	Mobs         int
	AggroDanger  int
}

var huntSpots []HuntSpot

// plain fields and dungeons only (excludes academy maps like prt_fild08a, instances, guild dungeons...)
var huntMapName = regexp.MustCompile(`^[a-z]+_(fild|dun)[0-9]{2}$|^mjolnir_[0-9]{2}$`)

func buildHuntSpots() {
	for m, ss := range spawnsByMap {
		gm := maps[m]
		if gm == nil || len(warpsFrom[m]) == 0 || !huntMapName.MatchString(m) { // must exist, be reachable, be a plain field/dungeon
			continue
		}
		h := HuntSpot{Map: m, MinLv: 999}
		for _, s := range ss {
			mb := mobs[s.MobID]
			if mb == nil || mb.Boss || s.Count <= 0 {
				continue
			}
			h.Mobs += s.Count
			if mb.Level < h.MinLv {
				h.MinLv = mb.Level
			}
			if mb.Level > h.MaxLv {
				h.MaxLv = mb.Level
			}
			if mb.Aggressive {
				h.AggroDanger += s.Count * mb.Level
			}
		}
		if h.Mobs >= 20 {
			huntSpots = append(huntSpots, h)
		}
	}
	sort.Slice(huntSpots, func(i, j int) bool { return huntSpots[i].Map < huntSpots[j].Map })
}

// SpotsFor returns maps suitable for a character level, safest first for cautious personas.
func SpotsFor(level int, risk float64) []HuntSpot {
	var out []HuntSpot
	tn := tune()
	maxLv := max(level+tn.TargetLvOver+int(risk*tn.TargetLvAggression), tn.TargetLvMin)
	for _, h := range huntSpots {
		if h.MinLv <= level+2 && h.MaxLv >= level-12 && h.MaxLv <= level+int(8+risk*12) && fitShare(h.Map, maxLv) >= minFitShare &&
			!swarmVeto(h.Map, level) && !mapReadsEmpty(h.Map) { // swarm.go: dense aggressive packs (mjolnir_06); seen.go: reads empty
			out = append(out, h)
		}
	}
	return out
}

// minFitShare: at least this share of a map's monsters must pass the hunter's level filter (targetMaxLv)
const minFitShare = 0.5

// fitShare: the share of the map's (non-boss) spawned monsters at or below maxLv. QA 2026-11-14: low-level parties
// and solo bots were sent to maps where their own target filter rejected every monster (pay_fild08 lv17-18 mobs for
// a lv9 Archer whose limit was 11-15; prt_fild07 for lv7) - 0 kills while "roaming" for 5+ minutes.
func fitShare(m string, maxLv int) float64 {
	tot, ok, killers := 0, 0, 0
	for _, s := range spawnsByMap[m] {
		mb := mobs[s.MobID]
		if mb == nil || mb.Boss || s.Count <= 0 {
			continue
		}
		tot += s.Count
		if mb.Level <= maxLv {
			ok += s.Count
		}
		// QA 2026-11-14: moc_fild03's Argos (lv47, aggressive) killed lv24-45 bots that fled and winged in vain. A common
		// aggressive monster ~10+ levels over the hunter's effective level (maxLv = powerLv + ~4) vetoes the map.
		if mb.Aggressive && mb.Level > maxLv+4 {
			killers += s.Count
		}
	}
	if tot == 0 {
		return 0
	}
	if killers >= 3 || float64(killers) >= 0.05*float64(tot) {
		return 0
	}
	return float64(ok) / float64(tot)
}

// mapMeanLv: the spawn-weighted mean level of map m's non-boss monsters (0 = no spawns). Spawns are static data;
// test maps (spawnsByMap edited at run time) are not cached.
func mapMeanLv(m string) int {
	if v, ok := meanLvCache.Load(m); ok {
		return v.(int)
	}
	n, sum := 0, 0
	for _, s := range spawnsByMap[m] {
		if mb := mobs[s.MobID]; mb != nil && !mb.Boss && s.Count > 0 {
			n += s.Count
			sum += s.Count * mb.Level
		}
	}
	lv := 0
	if n > 0 {
		lv = sum / n
	}
	if len(huntSpots) > 0 { // game data loaded: cache
		meanLvCache.Store(m, lv)
	}
	return lv
}

var meanLvCache sync.Map

// mapAggroPct: the share (0-100) of map m's non-boss spawned monsters that are aggressive.
func mapAggroPct(m string) int {
	if v, ok := aggroPctCache.Load(m); ok {
		return v.(int)
	}
	// assist packs (Wolf, Peco Peco) count half: they don't start the fight, but 6-12 join it (QA 17:47-18:18:
	// moc_fild03's 164 Wolves - 25 deaths in 30 min at effective levels 43-48, "6-12 on me")
	n, ag := 0, 0
	for _, s := range spawnsByMap[m] {
		if mb := mobs[s.MobID]; mb != nil && !mb.Boss && s.Count > 0 {
			n += s.Count
			if mb.Aggressive {
				ag += 2 * s.Count
			} else if mb.Assist {
				ag += s.Count
			}
		}
	}
	pct := 0
	if n > 0 {
		pct = ag * 50 / n
	}
	if len(huntSpots) > 0 {
		aggroPctCache.Store(m, pct)
	}
	return pct
}

var aggroPctCache sync.Map

// mapAggroTopLv: the level of the strongest aggressive monster that makes up 5%+ of map m's non-boss spawns (0 = none).
func mapAggroTopLv(m string) int {
	if v, ok := aggroTopCache.Load(m); ok {
		return v.(int)
	}
	n := 0
	cnt := map[int]int{}
	for _, s := range spawnsByMap[m] {
		if mb := mobs[s.MobID]; mb != nil && !mb.Boss && s.Count > 0 {
			n += s.Count
			if mb.Aggressive || mb.Assist {
				cnt[s.MobID] += s.Count
			}
		}
	}
	top := 0
	for id, c := range cnt {
		if c*20 >= n && mobs[id].Level > top {
			top = mobs[id].Level
		}
	}
	if len(huntSpots) > 0 {
		aggroTopCache.Store(m, top)
	}
	return top
}

var aggroTopCache sync.Map

func isTown(m string) bool {
	switch m {
	case "prontera", "izlude", "geffen", "payon", "morocc", "alberta", "aldebaran", "yuno", "comodo",
		"einbroch", "lighthalzen", "hugel", "rachel", "veins", "amatsu", "gonryun", "louyang", "ayothaya",
		"umbala", "niflheim", "xmas", "moscovia", "brasilis", "dewata", "malangdo", "malaya", "eclage", "lasagna":
		return true
	}
	return false
}
