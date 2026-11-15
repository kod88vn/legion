package main

// Personas: every bot is a "player" with a profile that shapes its algorithmic behaviour (risk, looting,
// reaction time, session schedule, how often it roams or rests) and gives the LLM mastermind a personality to
// write chat for. Generated deterministically from a seed; stored in data/personas.json.

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Persona struct {
	Index     int    `json:"index"`
	Account   string `json:"account"`
	Password  string `json:"password"`
	AccountID int    `json:"account_id"`
	CharID    int    `json:"char_id"`
	Name      string `json:"name"`
	Sex       string `json:"sex"` // M/F
	Archetype string `json:"archetype"`
	Bio       string `json:"bio"`
	Age       int    `json:"age"`
	Country   string `json:"country"`
	TZ        int    `json:"tz_offset_hours"` // relative to server time

	// traits 0..1
	Aggression  float64 `json:"aggression"`  // picks fights, attacks tougher mobs
	Risk        float64 `json:"risk"`        // trains on dangerous maps, rests late
	Chattiness  float64 `json:"chattiness"`  // how often it talks (LLM side)
	Patience    float64 `json:"patience"`    // long sessions on one map vs map-hopping
	Greed       float64 `json:"greed"`       // loots everything vs only valuables
	Sociability float64 `json:"sociability"` // seeks parties, hangs in towns
	Helpfulness float64 `json:"helpfulness"` // helps others (heals, gives directions)
	Loyalty     float64 `json:"loyalty"`     // sticks with guild/party
	Skill       float64 `json:"skill"`       // reaction speed / play quality
	ReactionMs  int     `json:"reaction_ms"` // base delay before acting

	// play schedule (server-local hours, after tz shift)
	SessionStart int     `json:"session_start_hour"`
	SessionHours int     `json:"session_hours"`
	WeekendBoost float64 `json:"weekend_boost"`
	AFKChance    float64 `json:"afk_chance"` // per minute

	Typing string `json:"typing_style"` // e.g. "lowercase, no punctuation, uses lol"

	Class     int    `json:"class"`
	ClassName string `json:"class_name"`
	Build     string `json:"build"`
	BaseLevel int    `json:"base_level"`
	JobLevel  int    `json:"job_level"`
	Guild     string `json:"guild"` // mastermind assignment ("" = independent)
	Home      string `json:"home_town"`

	Chars []CharSpec `json:"characters"` // [0] = main; alts: vendor, support
}

type CharSpec struct {
	CharID    int    `json:"char_id"`
	Slot      int    `json:"slot"`
	Name      string `json:"name"`
	Role      string `json:"role"` // main, vendor, support
	Class     int    `json:"class"`
	ClassName string `json:"class_name"`
	Build     string `json:"build"`
	BaseLevel int    `json:"base_level"`
	JobLevel  int    `json:"job_level"`
	Plan      string `json:"plan,omitempty"`         // build plan id (builds.go)
	Target    int    `json:"target_class,omitempty"` // final class the plan leads to
}

var classNames = map[int]string{0: "Novice", 1: "Swordman", 2: "Mage", 3: "Archer", 4: "Acolyte", 5: "Merchant", 6: "Thief",
	7: "Knight", 8: "Priest", 9: "Wizard", 10: "Blacksmith", 11: "Hunter", 12: "Assassin", 14: "Crusader", 15: "Monk",
	16: "Sage", 17: "Rogue", 18: "Alchemist", 19: "Bard", 20: "Dancer", 23: "Super Novice", 4001: "High Novice",
	4002: "High Swordman", 4003: "High Mage", 4004: "High Archer", 4005: "High Acolyte", 4006: "High Merchant",
	4007: "High Thief", 4008: "Lord Knight", 4009: "High Priest", 4010: "High Wizard", 4011: "Whitesmith", 4012: "Sniper",
	4013: "Assassin Cross", 4015: "Paladin", 4016: "Champion", 4017: "Professor", 4018: "Stalker", 4019: "Creator",
	4020: "Clown", 4021: "Gypsy", 4054: "Rune Knight", 4055: "Warlock", 4056: "Ranger", 4057: "Arch Bishop",
	4058: "Mechanic", 4059: "Guillotine Cross", 4060: "Rune Knight", 4061: "Warlock", 4062: "Ranger", 4063: "Arch Bishop",
	4064: "Mechanic", 4065: "Guillotine Cross", 4066: "Royal Guard", 4067: "Sorcerer", 4068: "Minstrel", 4069: "Wanderer",
	4070: "Sura", 4071: "Genetic", 4072: "Shadow Chaser", 4073: "Royal Guard", 4074: "Sorcerer", 4075: "Minstrel",
	4076: "Wanderer", 4077: "Sura", 4078: "Genetic", 4079: "Shadow Chaser"}

func className(id int) string {
	if n, ok := classNames[id]; ok {
		return n
	}
	return fmt.Sprintf("class %d", id)
}

func (b *Bot) charSpec() *CharSpec {
	if b.cur != nil {
		return b.cur
	}
	return &b.P.Chars[0]
}
func (b *Bot) charName() string { return b.charSpec().Name }
func (b *Bot) charRole() string { return b.charSpec().Role }
func (b *Bot) build() string    { return b.charSpec().Build }

// pickChar chooses which of the player's characters to log in with this session.
func (b *Bot) pickChar() *CharSpec {
	cs := b.P.Chars
	if c := restoreCharFor(b); c != "" { // a party restored after a restart waits for this character (partypersist.go)
		for i := range cs {
			if strings.EqualFold(cs[i].Name, c) {
				return &cs[i]
			}
		}
	}
	if b.pin != nil {
		for i := range cs {
			if strings.EqualFold(cs[i].Name, b.pin.Char) {
				return &cs[i]
			}
		}
	}
	if len(cs) == 1 {
		return &cs[0]
	}
	if n := b.guildBrewerChar(); n != "" { // alchemy.go (economy run 9): the brewer account plays its brewer
		for i := range cs {
			if cs[i].Name == n {
				return &cs[i]
			}
		}
	}
	if b.leaderVision() != nil { // guild leaders play their main character: it levels and leads (guildlead.go)
		for i := range cs {
			if cs[i].Role == "main" {
				return &cs[i]
			}
		}
	}
	w := make([]float64, len(cs))
	total := 0.0
	for i, c := range cs {
		switch c.Role {
		case "vendor":
			w[i] = 0.35
			if b.P.Archetype == "merchant" {
				w[i] = 0.8
			}
		case "support":
			w[i] = 0.25 + 0.3*b.P.Helpfulness
		default:
			w[i] = 1
		}
		if guildAlchemistChar(c.Name) != "" {
			w[i] += 0.6 // alchemy.go: the guild's alchemist is played more, so the guild gets its potions sooner
			if knownBrewer(c.Name) {
				w[i] += 6 // economy run 9: with the guide it brews the guild's potions - played ~3 sessions in 4
			}
		}
		total += w[i]
	}
	r := b.rng.Float64() * total
	for i := range cs {
		if r < w[i] {
			return &cs[i]
		}
		r -= w[i]
	}
	return &cs[0]
}

var archetypes = []struct {
	name   string
	weight int
	bias   map[string]float64
	bio    []string
}{
	{"grinder", 22, map[string]float64{"aggression": .75, "patience": .9, "chattiness": .15, "greed": .6, "skill": .7}, []string{"min-maxes every stat", "farms the same map for hours", "counts exp per hour"}},
	{"socialite", 14, map[string]float64{"sociability": .9, "chattiness": .85, "helpfulness": .6, "patience": .4}, []string{"in it for the friends", "always looking for a party", "knows everyone in town"}},
	{"merchant", 10, map[string]float64{"greed": .95, "patience": .7, "aggression": .3, "sociability": .6}, []string{"buys low sells high", "dreams of a vending empire", "hoards rare drops"}},
	{"explorer", 10, map[string]float64{"patience": .25, "risk": .7, "chattiness": .4}, []string{"wants to see every map", "gets lost on purpose", "collects screenshots"}},
	{"casual", 18, map[string]float64{"skill": .35, "patience": .35, "chattiness": .45, "risk": .35}, []string{"plays after work", "logs in for an hour or two", "here to relax"}},
	{"hardcore", 8, map[string]float64{"risk": .9, "aggression": .9, "skill": .85, "loyalty": .8}, []string{"lives for WoE", "tanks MVPs for fun", "never backs down"}},
	{"newbie", 10, map[string]float64{"skill": .15, "risk": .5, "chattiness": .55, "helpfulness": .2}, []string{"first MMO ever", "still learning the ropes", "asks a lot of questions"}},
	{"helper", 8, map[string]float64{"helpfulness": .95, "sociability": .8, "chattiness": .6, "aggression": .25}, []string{"loves supporting others", "answers every newbie question", "buffs strangers"}},
}

var classes = []struct {
	id    int
	name  string
	build []string
	base  [2]int // base level range
}{
	{1, "Swordman", []string{"str-vit", "str-agi"}, [2]int{12, 45}},
	{2, "Mage", []string{"int-dex"}, [2]int{12, 45}},
	{3, "Archer", []string{"dex-agi"}, [2]int{12, 45}},
	{4, "Acolyte", []string{"int-vit", "str-agi"}, [2]int{12, 45}},
	{5, "Merchant", []string{"str-vit", "str-dex"}, [2]int{12, 45}},
	{6, "Thief", []string{"agi-str"}, [2]int{12, 45}},
	{7, "Knight", []string{"str-agi", "str-vit"}, [2]int{50, 95}},
	{8, "Priest", []string{"int-vit", "int-dex"}, [2]int{50, 95}},
	{9, "Wizard", []string{"int-dex"}, [2]int{50, 95}},
	{10, "Blacksmith", []string{"str-dex", "str-vit"}, [2]int{50, 95}},
	{11, "Hunter", []string{"dex-agi", "dex-luk"}, [2]int{50, 95}},
	{12, "Assassin", []string{"agi-str", "agi-crit"}, [2]int{50, 95}},
	{14, "Crusader", []string{"str-vit", "vit-int"}, [2]int{50, 95}},
	{15, "Monk", []string{"agi-str", "str-vit"}, [2]int{50, 95}},
	{16, "Sage", []string{"int-dex"}, [2]int{50, 95}},
	{17, "Rogue", []string{"agi-str", "dex-agi"}, [2]int{50, 95}},
	{18, "Alchemist", []string{"str-int", "int-dex"}, [2]int{50, 95}},
}

var countries = []struct {
	name string
	tz   int
}{{"US", -8}, {"US", -5}, {"Brazil", -3}, {"UK", 0}, {"Germany", 1}, {"Philippines", 8}, {"Indonesia", 7},
	{"Vietnam", 7}, {"Thailand", 7}, {"Korea", 9}, {"Japan", 9}, {"Australia", 10}, {"Canada", -5}, {"Spain", 1}}

var typingStyles = []string{
	"all lowercase, no punctuation, says 'lol' a lot",
	"proper sentences with punctuation",
	"short replies, uses 'k', 'ty', 'np'",
	"lots of emoticons like :D xD ^^",
	"ALL CAPS when excited, otherwise normal",
	"casual slang, abbreviations (brb, afk, omw)",
	"polite and formal, says please and thank you",
	"broken English, second-language speaker",
	"roleplays a little, flowery fantasy speech",
	"terse, mostly one or two words",
}

var nameParts = struct{ a, b, styles []string }{
	a:      []string{"Shadow", "Luna", "Kira", "Dark", "Holy", "Storm", "Neko", "Frost", "Blaze", "Ash", "Rin", "Kai", "Zero", "Mochi", "Sky", "Iron", "Silent", "Crimson", "Silver", "Moon", "Sun", "Wolf", "Raven", "Lily", "Echo", "Nova", "Zen", "Yuki", "Hana", "Ryu", "Oni", "Pudding", "Kimchi", "Taco", "Pixel", "Mint", "Ghost", "Star", "Thorn", "Vex"},
	b:      []string{"blade", "heart", "fang", "rose", "wing", "knight", "fox", "storm", "dancer", "hunter", "mage", "soul", "song", "fire", "leaf", "claw", "shot", "bunny", "cat", "dragon", "smith", "monk", "sage", "rogue", "chan", "kun", "senpai", "lord", "queen", "boy", "girl", "x", "z", "99", "07", "2k"},
	styles: []string{"%s%s", "%s %s", "xX%s%sXx", "%s_%s", "%s%s", "%s%s", "Lil%s%s", "%s.%s", "The%s%s"},
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func makeName(r *rand.Rand, used map[string]bool) string {
	for {
		a := nameParts.a[r.Intn(len(nameParts.a))]
		b := nameParts.b[r.Intn(len(nameParts.b))]
		st := nameParts.styles[r.Intn(len(nameParts.styles))]
		n := fmt.Sprintf(st, a, b)
		if r.Intn(4) == 0 {
			n = strings.ToLower(n)
		}
		if r.Intn(5) == 0 {
			n = fmt.Sprintf("%s%d", n, r.Intn(99)+1)
		}
		if len(n) > 23 || used[strings.ToLower(n)] {
			continue
		}
		used[strings.ToLower(n)] = true
		return n
	}
}

func generatePersonas(n int, seed int64, guilds int) []*Persona {
	r := rand.New(rand.NewSource(seed))
	used := map[string]bool{"admin": true, "aibot": true}
	total := 0
	for _, a := range archetypes {
		total += a.weight
	}
	var out []*Persona
	for i := 0; i < n; i++ {
		pick := r.Intn(total)
		ai := 0
		for k, a := range archetypes {
			if pick < a.weight {
				ai = k
				break
			}
			pick -= a.weight
		}
		at := archetypes[ai]
		trait := func(k string) float64 {
			base := 0.5
			if v, ok := at.bias[k]; ok {
				base = v
			}
			return clamp01(base + r.NormFloat64()*0.15)
		}
		c := countries[r.Intn(len(countries))]
		// population: more first-jobbers than second-jobbers
		cl := classes[r.Intn(6)]
		if r.Float64() < 0.4 {
			cl = classes[6+r.Intn(len(classes)-6)]
		}
		lv := cl.base[0] + int(math.Pow(r.Float64(), 1.6)*float64(cl.base[1]-cl.base[0]))
		jl := 10 + r.Intn(40)
		if cl.id <= 6 && jl > 50 {
			jl = 50
		}
		p := &Persona{
			Index: i + 1, Account: fmt.Sprintf("lg%04d", i+1), Password: "legion",
			AccountID: 2100001 + i, CharID: 170001 + i,
			Name: makeName(r, used), Archetype: at.name,
			Bio: at.bio[r.Intn(len(at.bio))], Age: 15 + int(math.Abs(r.NormFloat64()*8)) + r.Intn(10),
			Country: c.name, TZ: c.tz,
			Aggression: trait("aggression"), Risk: trait("risk"), Chattiness: trait("chattiness"),
			Patience: trait("patience"), Greed: trait("greed"), Sociability: trait("sociability"),
			Helpfulness: trait("helpfulness"), Loyalty: trait("loyalty"), Skill: trait("skill"),
			Typing: typingStyles[r.Intn(len(typingStyles))],
			Class:  cl.id, ClassName: cl.name, Build: cl.build[r.Intn(len(cl.build))],
			BaseLevel: lv, JobLevel: jl,
		}
		p.Sex = "M"
		if r.Intn(100) < 38 {
			p.Sex = "F"
		}
		p.ReactionMs = int(150 + (1-p.Skill)*700 + r.Float64()*150)
		p.SessionStart = ((18+r.Intn(6)-p.TZ)%24 + 24) % 24 // evenings local time
		p.SessionHours = 2 + int(p.Patience*5) + r.Intn(3)
		p.WeekendBoost = 0.2 + r.Float64()*0.8
		p.AFKChance = 0.01 + (1-p.Patience)*0.04
		if guilds > 0 && r.Float64() < 0.8 {
			p.Guild = fmt.Sprintf("guild%02d", r.Intn(guilds)+1)
		}
		p.Chars = []CharSpec{{CharID: p.CharID, Slot: 0, Name: p.Name, Role: "main", Class: p.Class,
			ClassName: p.ClassName, Build: p.Build, BaseLevel: p.BaseLevel, JobLevel: p.JobLevel}}
		altName := func(suffixes []string) string {
			base := strings.Fields(strings.NewReplacer("xX", "", "Xx", "", "_", " ", ".", " ").Replace(p.Name))[0]
			for _, s := range r.Perm(len(suffixes)) {
				n := base + suffixes[s]
				if len(n) <= 23 && !used[strings.ToLower(n)] {
					used[strings.ToLower(n)] = true
					return n
				}
			}
			return makeName(r, used)
		}
		vendorP := 0.22
		if p.Archetype == "merchant" {
			vendorP = 0.9
		}
		if r.Float64() < vendorP {
			cls, lv, cname := 5, 30+r.Intn(30), "Merchant"
			if r.Float64() < 0.5 {
				cls, lv, cname = []int{10, 18}[r.Intn(2)], 60+r.Intn(30), ""
				cname = map[int]string{10: "Blacksmith", 18: "Alchemist"}[cls]
			}
			p.Chars = append(p.Chars, CharSpec{CharID: 180000 + p.Index, Slot: 1,
				Name: altName([]string{" Mart", " Shop", "Store", " Trade", " Goods", "Vend", " Deals", " Bazaar"}),
				Role: "vendor", Class: cls, ClassName: cname, Build: "str-dex", BaseLevel: lv, JobLevel: 40})
		}
		supportP := 0.12
		if p.Archetype == "helper" || p.Archetype == "socialite" {
			supportP = 0.55
		}
		if r.Float64() < supportP {
			cls, lv, cname := 4, 20+r.Intn(25), "Acolyte"
			if r.Float64() < 0.55 {
				cls, lv, cname = 8, 55+r.Intn(35), "Priest"
			}
			p.Chars = append(p.Chars, CharSpec{CharID: 190000 + p.Index, Slot: 2,
				Name: altName([]string{"Buffs", " Heals", "Priest", " Aco", "Prist", " Bless", "Healz"}),
				Role: "support", Class: cls, ClassName: cname, Build: "int-vit", BaseLevel: lv, JobLevel: 40})
		}
		out = append(out, p)
	}
	return out
}

func savePersonas(path string, ps []*Persona) error {
	b, _ := json.MarshalIndent(ps, "", "  ")
	return os.WriteFile(path, b, 0644)
}

func loadPersonas(path string) ([]*Persona, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ps []*Persona
	err = json.Unmarshal(b, &ps)
	return ps, err
}

// ---------------------------------------------------------------- DB seeding

var weaponFor = map[int]int{1: 1101, 7: 1101, 14: 1101, 2: 1601, 9: 1601, 16: 1601, 3: 1701, 11: 1701,
	4: 1501, 8: 1501, 15: 1501, 5: 1301, 10: 1301, 18: 1301, 6: 1201, 12: 1201, 17: 1201}

func statPoints(level int) int {
	t := 48
	for l := 1; l < level; l++ {
		t += l/5 + 3
	}
	return t
}

// allocate distributes stat points according to a build like "str-agi".
func allocate(build string, level int) (st [6]int) {
	idx := map[string]int{"str": 0, "agi": 1, "vit": 2, "int": 3, "dex": 4, "luk": 5, "crit": 5}
	parts := strings.Split(build, "-")
	weights := [6]float64{.05, .05, .05, .05, .05, .05}
	weights[idx[parts[0]]] += .5
	if len(parts) > 1 {
		weights[idx[parts[1]]] += .3
	}
	for i := range st {
		st[i] = 1
	}
	pts := statPoints(level)
	cost := func(v int) int { return (v-1)/10 + 2 }
	for pts > 0 {
		// pick stat with lowest value/weight ratio that is affordable
		best, bv := -1, 1e9
		for i := range st {
			if st[i] >= 99 || cost(st[i]) > pts {
				continue
			}
			v := float64(st[i]) / weights[i]
			if v < bv {
				best, bv = i, v
			}
		}
		if best < 0 {
			break
		}
		pts -= cost(st[best])
		st[best]++
	}
	return
}

var townSpawn = map[string][2]int{"prontera": {156, 191}, "izlude": {128, 114}, "geffen": {119, 59},
	"payon": {162, 233}, "morocc": {156, 93}, "alberta": {116, 57}, "aldebaran": {140, 131}, "yuno": {158, 125}}

// nearestTown picks the town with the fewest warps to the map.
var nearestTownCache sync.Map // map -> town

func nearestTown(m string) string {
	if v, ok := nearestTownCache.Load(m); ok {
		return v.(string)
	}
	t := nearestTownCalc(m)
	nearestTownCache.Store(m, t)
	return t
}

func nearestTownCalc(m string) string {
	// a town (or one of its buildings) is its own nearest town: RouteMaps(t, .., t) has no hops and returned nil, so
	// nearestTown("prontera") was "izlude" and Prontera shop trips were announced as "heading to izlude"
	if t := townOf(m); t != "" {
		return t
	}
	best, bl := "prontera", 99
	towns := make([]string, 0, len(townSpawn))
	for t := range townSpawn {
		towns = append(towns, t)
	}
	sort.Strings(towns)
	for _, t := range towns {
		if r := RouteMaps(t, townSpawn[t][0], townSpawn[t][1], m); r != nil && len(r) < bl {
			best, bl = t, len(r)
		}
	}
	return best
}

// seedSQL writes accounts, characters and starter items for every persona (idempotent: deletes old Legion rows).
func seedSQL(ps []*Persona, r *rand.Rand) string {
	var b strings.Builder
	// only ever touch characters that belong to Legion accounts: human characters made in the client can get ids
	// in the same numeric range (it happened once and a human character was lost)
	// accounts kept as they are (-keep): excluded from every delete and insert
	keepSQL := ""
	if len(seedKeep) > 0 {
		var ids []string
		for id := range seedKeep {
			ids = append(ids, strconv.Itoa(id))
		}
		keepSQL = " AND account_id NOT IN (" + strings.Join(ids, ",") + ")"
	}
	legionChars := "SELECT char_id FROM `char` WHERE account_id BETWEEN 2100001 AND 2199999" + keepSQL
	for _, t := range []string{"inventory", "cart_inventory", "skill", "char_reg_num"} {
		fmt.Fprintf(&b, "DELETE FROM %s WHERE char_id IN (%s);\n", t, legionChars)
	}
	b.WriteString("DELETE FROM `party` WHERE leader_id BETWEEN 2100001 AND 2199999" + strings.ReplaceAll(keepSQL, "account_id", "leader_id") + ";\n")
	b.WriteString("DELETE FROM `char` WHERE account_id BETWEEN 2100001 AND 2199999" + keepSQL + ";\n")
	// keep client-made characters out of the Legion id range
	b.WriteString("ALTER TABLE `char` AUTO_INCREMENT = 200100;\n")
	b.WriteString("DELETE FROM login WHERE account_id BETWEEN 2100001 AND 2199999" + keepSQL + ";\n")
	for _, p := range ps {
		// G06: only Legion's own accounts are seeded (a persona outside the bot range, e.g. Ayla's 2000000, is never
		// inserted or deleted by a re-seed; the DELETEs above are range-bound too)
		if p.AccountID < 2100001 || p.AccountID > 2199999 || seedKeep[p.AccountID] {
			continue
		}
		// everyone starts as a Novice, so in a beginner town that's connected to the rest of the world by road
		// (Renewal Morroc's desert fields have no warps onward; Yuno/Aldebaran are surrounded by high-level maps)
		home := newbieTowns[r.Intn(len(newbieTowns))]
		p.Home = home
		fmt.Fprintf(&b, "INSERT INTO login (account_id,userid,user_pass,sex,email,group_id) VALUES (%d,'%s','%s','%s','legion@local',0);\n",
			p.AccountID, p.Account, p.Password, p.Sex)
		for ci := range p.Chars {
			c := &p.Chars[ci]
			// real progression: every character starts as a level 1 Novice with a plan for its endgame build
			pl := planForClass(c.Class, p.Sex, r.Intn)
			if c.Role == "support" {
				// a healer: the support or the Magnus priest (run 5 variety; the battle priest is a main-role build)
				pl = builds["priest"]
				if r.Intn(2) == 0 {
					pl = builds["priest_me"]
				}
			}
			if c.Role == "vendor" {
				fam := "blacksmith"
				if r.Intn(3) == 0 {
					fam = "alchemist"
				}
				var vs []*Build
				for _, id := range buildOrder() {
					if v := builds[id]; v.base() == fam {
						vs = append(vs, v)
					}
				}
				pl = pickVariant(vs, r.Intn) // run 5: mace smiths and potion / battle alchemists too
			}
			c.Plan, c.Target = pl.ID, pl.Class
			c.Class, c.ClassName, c.BaseLevel, c.JobLevel = 0, "Novice", 1, 1
			town := home
			ts := townSpawn[town]
			sx, sy := seedSpawnCell(town, ts, r)
			esc := strings.ReplaceAll(c.Name, "'", "''")
			// a fresh Novice: all stats 1, the 48 starting status points, the official starter kit
			fmt.Fprintf(&b, "INSERT INTO `char` (char_id,account_id,char_num,name,class,base_level,job_level,str,agi,vit,`int`,dex,luk,max_hp,hp,max_sp,sp,status_point,skill_point,hair,hair_color,clothes_color,zeny,weapon,last_map,last_x,last_y,save_map,save_x,save_y,sex,`option`) VALUES (%d,%d,%d,'%s',0,1,1,1,1,1,1,1,1,40,40,11,11,48,0,%d,%d,%d,%d,1201,'%s',%d,%d,'%s',%d,%d,'%s',0);\n",
				c.CharID, p.AccountID, c.Slot, esc, 1+r.Intn(22), r.Intn(9), r.Intn(4),
				1500+r.Intn(1500), town, sx, sy, town, ts[0], ts[1], p.Sex)
			// starter bonus to get through the first levels fast: light Novice Potions (weight 1), a few Light
			// Yellow and Red Potions, Fly / Butterfly Wings
			fmt.Fprintf(&b, "INSERT INTO inventory (char_id,nameid,amount,equip,identify) VALUES (%d,1201,1,2,1),(%d,2301,1,16,1),(%d,569,%d,0,1),(%d,11500,%d,0,1),(%d,501,%d,0,1),(%d,601,%d,0,1),(%d,602,%d,0,1);\n",
				c.CharID, c.CharID, c.CharID, 120+r.Intn(60), c.CharID, 10+r.Intn(10), c.CharID, 10+r.Intn(10), c.CharID, 5+r.Intn(5), c.CharID, 2+r.Intn(2))
			// which 1st and 2nd job the plan goes through (read by rathena/npc-custom/legion.txt)
			fmt.Fprintf(&b, "INSERT INTO char_reg_num (char_id,`key`,`index`,value) VALUES (%d,'legion_first',0,%d),(%d,'legion_branch',0,%d);\n",
				c.CharID, pl.First, c.CharID, pl.Branch)
		}
		p.Class, p.ClassName, p.BaseLevel, p.JobLevel = 0, "Novice", 1, 1
	}
	return b.String()
}

// newbieTowns: where new characters start (weighted: Prontera twice).
var newbieTowns = []string{"prontera", "prontera", "izlude", "payon", "geffen", "alberta"}

// seedKeep: account ids the seed must leave untouched (set from -keep).
var seedKeep = map[int]bool{}

// seedSpawnCell: a walkable cell within 3 of the town spawn, in the spawn's own walk region. "spawn +-3" without a
// check put Novices on blocked cells (227 Legion chars stood on one, 17 walled in: geffen 116,56 is inside the
// fountain's wall - Silver_storm & co. stood there "travel -> gef_fild07" for 11+ min).
func seedSpawnCell(town string, ts [2]int, r *rand.Rand) (int, int) {
	gm := maps[town]
	for i := 0; i < 30 && gm != nil; i++ {
		x, y := ts[0]+r.Intn(7)-3, ts[1]+r.Intn(7)-3
		if gm.Walkable(x, y) && !gm.OnWarp(x, y) && gm.Region(x, y) == gm.Region(ts[0], ts[1]) {
			return x, y
		}
	}
	return ts[0], ts[1]
}
