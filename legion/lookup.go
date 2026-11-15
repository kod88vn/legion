package main

// Game-data lookup for the LLM minds (2026-11-14, owner: "the mind bot should have access to drop table for all items,
// monsters and map so it can choose where to farm better"). The rAthena tables live in the container, so the minds
// ask here: GET /api/lookup/item?q=<name|id>, /api/lookup/monster?q=<name|id>, /api/lookup/map?q=<map>
// (optional &lv=34,36,40: the asking party's levels, to grade each map). legionctl drops/mob/mapinfo print them.
// Names are matched fuzzily: case, "_", substrings, word prefixes ("blue pot"), initials ("SS card" = Soldier
// Skeleton Card) and small typos.

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type mobDrop struct {
	Item int
	Rate int // 1/10000
	Mvp  bool
}

type mobExtra struct {
	Race, Size string
	ElemLv     int
	BaseExp    int
	JobExp     int
	Mvp        bool
	Drops      []mobDrop
}

var (
	lookupOnce sync.Once
	mobExtras  = map[int]*mobExtra{}
	droppedBy  = map[int][]int{}     // item id -> mob ids
	mobMaps    = map[int][]Spawn{}   // mob id -> spawns (summed per map)
	spawnKind  = map[string]string{} // map -> "dungeon" / "field" / "town" (from the npc/re/mobs folder)
)

// noteSpawnFile remembers whether a map's spawns come from a dungeons/ or fields/ script (called by loadNPCs).
func noteSpawnFile(m, fn string) {
	f := strings.ToLower(filepath.ToSlash(fn))
	switch {
	case strings.Contains(f, "/dungeons/"):
		spawnKind[m] = "dungeon"
	case strings.Contains(f, "/fields/"):
		if spawnKind[m] == "" {
			spawnKind[m] = "field"
		}
	}
}

func mapKindOf(m string) string {
	if isTown(m) {
		return "town"
	}
	if k := spawnKind[m]; k != "" {
		return k
	}
	if strings.Contains(m, "dun") || strings.Contains(m, "_in") {
		return "dungeon"
	}
	return "field"
}

func loadLookup() {
	byAegis := map[string]int{}
	for _, it := range items {
		if it.Aegis != "" {
			byAegis[it.Aegis] = it.ID
		}
	}
	if f, err := os.Open(filepath.Join(*rathena, "db", "re", "mob_db.yml")); err == nil {
		parseMobExtras(bufio.NewScanner(f), byAegis)
		f.Close()
	}
	for mid, x := range mobExtras {
		seen := map[int]bool{}
		for _, d := range x.Drops {
			if !seen[d.Item] {
				seen[d.Item] = true
				droppedBy[d.Item] = append(droppedBy[d.Item], mid)
			}
		}
	}
	for m, ss := range spawnsByMap {
		sum := map[int]int{}
		for _, s := range ss {
			sum[s.MobID] += s.Count
		}
		for id, n := range sum {
			mobMaps[id] = append(mobMaps[id], Spawn{m, id, n})
		}
	}
}

func parseMobExtras(sc *bufio.Scanner, byAegis map[string]int) {
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var cur *mobExtra
	block := ""
	var item int
	for sc.Scan() {
		l := sc.Text()
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(l, "  - Id:"):
			v := strings.TrimSpace(t[5:])
			id, _ := strconv.Atoi(v)
			cur, block, item = nil, "", 0
			if mobExtras[id] == nil { // the first entry wins
				cur = &mobExtra{}
				mobExtras[id] = cur
			}
		case cur == nil:
		case strings.HasPrefix(l, "    ") && !strings.HasPrefix(l, "     "):
			block = ""
			switch {
			case t == "Drops:" || t == "MvpDrops:":
				block = t
			case strings.HasPrefix(t, "Race:"):
				cur.Race = strings.TrimSpace(t[5:])
			case strings.HasPrefix(t, "Size:"):
				cur.Size = strings.TrimSpace(t[5:])
			case strings.HasPrefix(t, "ElementLevel:"):
				cur.ElemLv, _ = strconv.Atoi(strings.TrimSpace(t[13:]))
			case strings.HasPrefix(t, "BaseExp:"):
				cur.BaseExp, _ = strconv.Atoi(strings.TrimSpace(t[8:]))
			case strings.HasPrefix(t, "JobExp:"):
				cur.JobExp, _ = strconv.Atoi(strings.TrimSpace(t[7:]))
			case strings.HasPrefix(t, "MvpExp:"):
				cur.Mvp = true
			}
		case block != "" && strings.HasPrefix(t, "- Item:"):
			item = byAegis[strings.TrimSpace(t[7:])]
		case block != "" && strings.HasPrefix(t, "Rate:") && item > 0:
			r, _ := strconv.Atoi(strings.TrimSpace(t[5:]))
			cur.Drops = append(cur.Drops, mobDrop{item, r, block == "MvpDrops:"})
			item = 0
		}
	}
}

// ---- fuzzy matching ----

func normName(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("_", " ", "-", " ", "'", "", ".", " ", ",", " ", "(", " ", ")", " ", "[", " ", "]", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func initials(words []string) string {
	b := make([]byte, 0, len(words))
	for _, w := range words {
		if w != "" {
			b = append(b, w[0])
		}
	}
	return string(b)
}

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := 1
			if a[i-1] == b[j-1] {
				c = 0
			}
			cur[j] = minInt3(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func minInt3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// matchScore: how well query q (normalised) names a thing called name (and aegis); 0 = no match.
func matchScore(q, name, aegis string) int {
	n := normName(name)
	a := normName(aegis)
	if q == "" || n == "" {
		return 0
	}
	if q == n {
		return 100
	}
	if q == a || strings.ReplaceAll(q, " ", "") == strings.ReplaceAll(n, " ", "") {
		return 95
	}
	qw := strings.Fields(q)
	nw := strings.Fields(n)
	// initials: "ss card" / "ssc" -> Soldier Skeleton Card, "bsb" -> ...
	{
		var qi []string
		var qr []string
		for _, w := range qw {
			if w == "card" {
				continue
			}
			qr = append(qr, w)
		}
		qi = qr
		hasCard := len(qr) < len(qw)
		var core []string
		nameCard := false
		for _, w := range nw {
			if w == "card" {
				nameCard = true
				continue
			}
			core = append(core, w)
		}
		if len(qi) == 1 && len(core) >= 2 && len(qi[0]) >= 2 {
			if hasCard == nameCard && qi[0] == initials(core) {
				return 85
			}
			if !hasCard && qi[0] == initials(nw) {
				return 80
			}
			if !hasCard && !nameCard && qi[0] == initials(core) {
				return 82
			}
		}
	}
	if strings.HasPrefix(n, q) {
		return 78 - minInt(len(n)-len(q), 20)/4
	}
	// every query word is a prefix (or substring) of a distinct name word
	used := make([]bool, len(nw))
	pref, sub := 0, 0
	for _, w := range qw {
		hit := false
		for i, x := range nw {
			if !used[i] && strings.HasPrefix(x, w) {
				used[i], hit = true, true
				pref++
				break
			}
		}
		if !hit {
			for i, x := range nw {
				if !used[i] && strings.Contains(x, w) && len(w) >= 3 {
					used[i], hit = true, true
					sub++
					break
				}
			}
		}
		if !hit {
			pref = -1
			break
		}
	}
	if pref >= 0 {
		extra := len(nw) - len(qw)
		return 70 - sub*5 - minInt(extra, 6)*2
	}
	if len(q) >= 4 && strings.Contains(n, q) {
		return 50
	}
	if a != "" && len(q) >= 4 && strings.Contains(a, q) {
		return 45
	}
	// typos: whole name, or the name without " card"
	if len(q) >= 4 {
		lim := 1
		if len(q) >= 8 {
			lim = 2
		}
		if len(q) >= 14 {
			lim = 3
		}
		if d := levenshtein(q, n); d <= lim {
			return 40 - d*5
		}
	}
	return 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type lookupHit struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Score int    `json:"score"`
}

func rankHits(hits []lookupHit, limit int) []lookupHit {
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].ID < hits[j].ID // classic items/monsters have the low ids
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// farmBonus: items a spawning monster drops rank above look-alikes nobody can farm ("SS card" = Soldier Skeleton
// Card, not the instance-only Sharp Spell Card): +1 dropped at all, +2 by a spawning monster, +1 more if 50+ spawn.
func farmBonus(id int) int {
	b, n := 0, 0
	for _, mid := range droppedBy[id] {
		b = 1
		for _, s := range mobMaps[mid] {
			n += s.Count
		}
	}
	if n > 0 {
		b += 2
	}
	if n >= 50 {
		b++
	}
	return b
}

// findItems: best matching items; items some monster drops get a small bonus (the real card over "Enhanced ...").
func findItems(query string, limit int) []lookupHit {
	lookupOnce.Do(loadLookup)
	if id, err := strconv.Atoi(strings.TrimSpace(query)); err == nil {
		if it := items[id]; it != nil {
			return []lookupHit{{id, it.Name, 100}}
		}
		return nil
	}
	q := normName(query)
	var hits []lookupHit
	for id, it := range items {
		if s := matchScore(q, it.Name, it.Aegis); s > 0 {
			s += farmBonus(id)
			hits = append(hits, lookupHit{id, it.Name, s})
		}
	}
	return rankHits(hits, limit)
}

// findMobs: best matching monsters; spawning ones get a bonus (the field Soldier Skeleton over event copies).
func findMobs(query string, limit int) []lookupHit {
	lookupOnce.Do(loadLookup)
	if id, err := strconv.Atoi(strings.TrimSpace(query)); err == nil {
		if mb := mobs[id]; mb != nil {
			return []lookupHit{{id, mb.Name, 100}}
		}
		return nil
	}
	q := normName(query)
	var hits []lookupHit
	for id, mb := range mobs {
		if s := matchScore(q, mb.Name, ""); s > 0 {
			if len(mobMaps[id]) > 0 {
				s += 2
			}
			hits = append(hits, lookupHit{id, mb.Name, s})
		}
	}
	return rankHits(hits, limit)
}

func findMaps(query string, limit int) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if _, ok := spawnsByMap[q]; ok {
		return []string{q}
	}
	var hits []lookupHit
	for m := range spawnsByMap {
		s := 0
		switch {
		case strings.HasPrefix(m, q):
			s = 80 - len(m)
		case strings.Contains(m, q):
			s = 60 - len(m)
		case len(q) >= 4 && levenshtein(q, m) <= 1:
			s = 40
		}
		if s > 0 {
			hits = append(hits, lookupHit{Name: m, Score: s})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Name < hits[j].Name
	})
	var out []string
	for i, h := range hits {
		if i >= limit {
			break
		}
		out = append(out, h.Name)
	}
	return out
}

// ---- answers ----

// lookupDanger: what makes a map risky (MVPs, mini-bosses, aggressive share, its strongest monster).
func lookupDanger(m string) map[string]interface{} {
	var mvps, minis []string
	maxLv, maxName := 0, ""
	for _, s := range spawnsByMap[m] {
		mb := mobs[s.MobID]
		if mb == nil || s.Count <= 0 {
			continue
		}
		x := mobExtras[s.MobID]
		switch {
		case x != nil && x.Mvp:
			mvps = appendUniq(mvps, mb.Name)
		case mb.Boss || mb.Champion:
			minis = appendUniq(minis, mb.Name)
		}
		if mb.Level > maxLv && !(x != nil && x.Mvp) {
			maxLv, maxName = mb.Level, mb.Name
		}
	}
	d := map[string]interface{}{"aggressive_pct": mapAggroPct(m), "strongest": maxName, "strongest_level": maxLv}
	if len(mvps) > 0 {
		d["mvp"] = mvps
	}
	if len(minis) > 0 {
		d["mini_boss"] = minis
	}
	return d
}

func appendUniq(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

// mvpUnsafe: "" or why map m is unsafe for these levels: an MVP spawns there and the party isn't far above it
// (unknown levels count as unsafe). Boss hunts are planned separately (bosshunt.go).
func mvpUnsafe(m string, lvs []int) string {
	mv := mapMVP(m)
	if mv == nil {
		return ""
	}
	lo := 0
	for i, l := range lvs {
		if i == 0 || l < lo {
			lo = l
		}
	}
	if lo == 0 || lo < mv.Level+partyMVPOverLv {
		return "unsafe: MVP " + mv.Name + " (lv " + strconv.Itoa(mv.Level) + ") spawns here"
	}
	return ""
}

// partyFit grades a monster level against the asking party's levels.
func partyFit(mobLv int, lvs []int) string {
	if len(lvs) == 0 {
		return ""
	}
	lo, hi := lvs[0], lvs[0]
	for _, l := range lvs {
		if l < lo {
			lo = l
		}
		if l > hi {
			hi = l
		}
	}
	switch {
	case mobLv > hi+15:
		return "too strong"
	case mobLv > hi+5:
		return "hard"
	case mobLv < lo-25:
		return "too weak (little exp)"
	case mobLv < lo-10:
		return "easy"
	}
	return "good"
}

func parseLevels(s string) []int {
	var out []int
	for _, p := range strings.Split(s, ",") {
		if v, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && v > 0 {
			out = append(out, v)
		}
	}
	return out
}

func ratePct(r int) float64 { return float64(r) / 100 }

func itemAnswer(id int, lvs []int) map[string]interface{} {
	it := items[id]
	ans := map[string]interface{}{"id": id, "name": it.Name, "aegis": it.Aegis, "type": it.Type, "npc_buy": it.Buy,
		"npc_sell": it.Buy / 2, "weight": it.Weight, "slots": it.Slots}
	var srcs []map[string]interface{}
	var farm []map[string]interface{}
	for _, mid := range droppedBy[id] {
		mb := mobs[mid]
		x := mobExtras[mid]
		if mb == nil || x == nil {
			continue
		}
		rate, mvp := 0, false
		for _, d := range x.Drops {
			if d.Item == id && effDropRate(d.Item, d.Rate, mb.Boss) > rate {
				rate, mvp = effDropRate(d.Item, d.Rate, mb.Boss), d.Mvp // droprate.go: the server's rate
			}
		}
		total := 0
		maps := []map[string]interface{}{}
		for _, s := range mobMaps[mid] {
			total += s.Count
			maps = append(maps, map[string]interface{}{"map": s.Map, "count": s.Count, "kind": mapKindOf(s.Map)})
			e := map[string]interface{}{"map": s.Map, "kind": mapKindOf(s.Map), "monster": mb.Name, "monster_id": mid,
				"level": mb.Level, "rate_pct": ratePct(rate), "count": s.Count, "score": float64(rate) * float64(s.Count) / 100,
				"boss": mb.Boss, "mvp": x.Mvp, "aggressive": mb.Aggressive, "danger": lookupDanger(s.Map)}
			if f := partyFit(mb.Level, lvs); f != "" {
				e["fit"] = f
			}
			if f := mvpUnsafe(s.Map, lvs); f != "" { // map agent: an MVP's map is no farming ground (QA 20:49 Eddga wipe)
				e["fit"], e["mvp_map"] = f, true
			}
			farm = append(farm, e)
		}
		sort.Slice(maps, func(i, j int) bool { return maps[i]["count"].(int) > maps[j]["count"].(int) })
		srcs = append(srcs, map[string]interface{}{"monster": mb.Name, "monster_id": mid, "level": mb.Level,
			"rate_pct": ratePct(rate), "mvp_drop": mvp, "boss": mb.Boss, "mvp": x.Mvp, "spawn_total": total, "maps": maps})
	}
	// farmable sources first: normal monsters, then mini-bosses, then MVPs (one spawn, hours apart), then none spawning
	srcRank := func(e map[string]interface{}) int {
		switch {
		case e["spawn_total"].(int) == 0:
			return 3
		case e["mvp"].(bool):
			return 2
		case e["boss"].(bool):
			return 1
		}
		return 0
	}
	sort.SliceStable(srcs, func(i, j int) bool {
		ri, rj := srcRank(srcs[i]), srcRank(srcs[j])
		if ri != rj {
			return ri < rj
		}
		return srcs[i]["rate_pct"].(float64)*float64(srcs[i]["spawn_total"].(int)) > srcs[j]["rate_pct"].(float64)*float64(srcs[j]["spawn_total"].(int))
	})
	sort.Slice(farm, func(i, j int) bool {
		gi, gj := fitRank(farm[i]), fitRank(farm[j])
		if gi != gj {
			return gi < gj
		}
		if mi, mj := farm[i]["mvp"].(bool), farm[j]["mvp"].(bool); mi != mj {
			return mj // an MVP is no farm spot
		}
		return farm[i]["score"].(float64) > farm[j]["score"].(float64)
	})
	ans["dropped_by"] = srcs
	ans["farm_maps"] = farm
	var sold []map[string]interface{}
	for m, ss := range shopsByMap {
		for _, s := range ss {
			for _, i := range s.Items {
				if i == id {
					sold = append(sold, map[string]interface{}{"map": m, "shop": s.Name, "x": s.X, "y": s.Y})
				}
			}
		}
	}
	sort.Slice(sold, func(i, j int) bool { return sold[i]["map"].(string) < sold[j]["map"].(string) })
	ans["npc_shops"] = sold
	return ans
}

// fitRank: with party levels given, maps the party can farm sort first (good, easy, hard, weak, too strong).
func fitRank(e map[string]interface{}) int {
	f, _ := e["fit"].(string)
	switch f {
	case "", "good":
		return 0
	case "easy":
		return 1
	case "hard":
		return 2
	case "too weak (little exp)":
		return 3
	}
	return 4
}

func mobAnswer(id int, lvs []int) map[string]interface{} {
	mb := mobs[id]
	x := mobExtras[id]
	if x == nil {
		x = &mobExtra{}
	}
	ans := map[string]interface{}{"id": id, "name": mb.Name, "level": mb.Level, "hp": mb.HP, "element": mb.Element,
		"element_level": x.ElemLv, "race": x.Race, "size": x.Size, "base_exp": x.BaseExp, "job_exp": x.JobExp,
		"aggressive": mb.Aggressive, "boss": mb.Boss, "mvp": x.Mvp, "mini_boss": (mb.Boss || mb.Champion) && !x.Mvp}
	if f := partyFit(mb.Level, lvs); f != "" {
		ans["fit"] = f
	}
	var drops []map[string]interface{}
	for _, d := range x.Drops {
		it := items[d.Item]
		if it == nil {
			continue
		}
		drops = append(drops, map[string]interface{}{"id": d.Item, "name": it.Name, "type": it.Type, "rate_pct": ratePct(effDropRate(d.Item, d.Rate, false)),
			"npc_sell": it.Buy / 2, "mvp_drop": d.Mvp})
	}
	ans["drops"] = drops
	var sp []map[string]interface{}
	for _, s := range mobMaps[id] {
		sp = append(sp, map[string]interface{}{"map": s.Map, "count": s.Count, "kind": mapKindOf(s.Map), "danger": lookupDanger(s.Map)})
	}
	sort.Slice(sp, func(i, j int) bool { return sp[i]["count"].(int) > sp[j]["count"].(int) })
	ans["spawns"] = sp
	return ans
}

func mapAnswer(m string, lvs []int) map[string]interface{} {
	sum := map[int]int{}
	for _, s := range spawnsByMap[m] {
		sum[s.MobID] += s.Count
	}
	var ms []map[string]interface{}
	total := 0
	for id, n := range sum {
		mb := mobs[id]
		if mb == nil {
			continue
		}
		x := mobExtras[id]
		mvp := x != nil && x.Mvp
		total += n
		e := map[string]interface{}{"id": id, "name": mb.Name, "level": mb.Level, "hp": mb.HP, "count": n, "element": mb.Element,
			"aggressive": mb.Aggressive, "boss": mb.Boss, "mvp": mvp, "mini_boss": (mb.Boss || mb.Champion) && !mvp}
		if x != nil {
			e["race"] = x.Race
			e["base_exp"] = x.BaseExp
			var best []string
			ds := append([]mobDrop(nil), x.Drops...)
			sort.Slice(ds, func(i, j int) bool { return ds[i].Rate > ds[j].Rate })
			for _, d := range ds {
				if it := items[d.Item]; it != nil && (it.Type == "Card" || len(best) < 3) {
					best = append(best, it.Name+" "+strconv.FormatFloat(ratePct(effDropRate(d.Item, d.Rate, false)), 'g', -1, 64)+"%")
				}
			}
			e["drops"] = best
		}
		if f := partyFit(mb.Level, lvs); f != "" {
			e["fit"] = f
		}
		ms = append(ms, e)
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i]["count"].(int) > ms[j]["count"].(int) })
	ans := map[string]interface{}{"map": m, "kind": mapKindOf(m), "monsters": ms, "total": total, "mean_level": mapMeanLv(m),
		"danger": lookupDanger(m), "loot_zeny_per_kill": mapLootZeny(m)}
	if mv := mapMVP(m); mv != nil {
		ans["mvp"] = mv.Name
		if f := mvpUnsafe(m, lvs); f != "" {
			ans["fit"] = f
		}
	}
	var out []string
	for _, w := range warpsFrom[m] {
		out = appendUniq(out, w.To)
	}
	sort.Strings(out)
	ans["warps_to"] = out
	return ans
}

func registerLookup(mux *http.ServeMux) {
	handle := func(kind string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			lookupOnce.Do(loadLookup)
			q := r.URL.Query().Get("q")
			lvs := parseLevels(r.URL.Query().Get("lv"))
			if strings.TrimSpace(q) == "" {
				http.Error(w, "q= missing", 400)
				return
			}
			res := lookupQuery(kind, q, lvs)
			if res == nil {
				http.Error(w, "nothing matches "+strconv.Quote(q), 404)
				return
			}
			writeJSON(w, res)
		}
	}
	mux.HandleFunc("/api/lookup/item", handle("item"))
	mux.HandleFunc("/api/lookup/monster", handle("monster"))
	mux.HandleFunc("/api/lookup/map", handle("map"))
}

// lookupQuery: the answer for the best match plus the other close matches (nil when nothing matches).
func lookupQuery(kind, q string, lvs []int) map[string]interface{} {
	lookupOnce.Do(loadLookup)
	switch kind {
	case "item":
		hits := findItems(q, 8)
		if len(hits) == 0 {
			return nil
		}
		return map[string]interface{}{"query": q, "result": itemAnswer(hits[0].ID, lvs), "matches": hits}
	case "monster":
		hits := findMobs(q, 8)
		if len(hits) == 0 {
			return nil
		}
		return map[string]interface{}{"query": q, "result": mobAnswer(hits[0].ID, lvs), "matches": hits}
	case "map":
		ms := findMaps(q, 8)
		if len(ms) == 0 {
			return nil
		}
		return map[string]interface{}{"query": q, "result": mapAnswer(ms[0], lvs), "matches": ms}
	}
	return nil
}
