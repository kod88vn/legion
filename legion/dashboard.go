package main

// Bird's-eye monitoring dashboard:
//   GET /dashboard              self-contained HTML page (dashboard.html, embedded)
//   GET /api/overview           one JSON snapshot of the whole world (population, levels, classes, plans, maps,
//                               activity rates, economy, humans, runtime)
//   GET /api/overview/history   one point per minute for the last 6 hours (online, kills/deaths/level-ups per minute...)
// The page also shows the LLM minds (minds.go, /api/minds: thoughts feed + owner messages, refreshed every 5 s)
// and guilds (/api/guilds/detail, guilds.go; rendered with whatever fields exist).
//
// Read-only: a background collector tails the events ring and samples the global counters into per-minute buckets,
// so rates don't depend on how long events stay in the (chat-heavy) ring.

import (
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed dashboard.html
var dashboardHTML []byte

const dashMinutes = 360 // history / bucket depth (6 hours)

type minuteBucket struct {
	Min        int64 // unix minute
	Kills      int64
	Deaths     int64
	Loots      int64
	LevelUps   int64
	JobChanges int64
	Bought     int64 // gear bought at NPCs
	Restocks   int64 // potion restocks
	ShopsOpen  int64 // vending shops opened
	Trades     int64 // bot-to-bot trades completed (economy.go)
	Parties    int64 // parties formed
	Chat       int64 // lines said by Legion characters
	DeathMaps  map[string]int
}

type historyPoint struct {
	T          int64   `json:"t"` // unix seconds (start of minute)
	Online     int     `json:"online"`
	AvgLevel   float64 `json:"avg_level"`
	Kills      int64   `json:"kills"`
	Deaths     int64   `json:"deaths"`
	LevelUps   int64   `json:"levelups"`
	JobChanges int64   `json:"jobchanges"`
	Loots      int64   `json:"loots"`
	Fighting   int     `json:"fighting"`
}

type dashCollector struct {
	mu      sync.Mutex
	buckets map[int64]*minuteBucket
	history []historyPoint
	start   time.Time

	cacheAt  time.Time
	cache    map[string]interface{}
	cacheMu  sync.Mutex
	lastMemT time.Time
	memStats runtime.MemStats
}

var dash = &dashCollector{buckets: map[int64]*minuteBucket{}}

// registerDashboard is called once from serveAPI.
func registerDashboard(mux *http.ServeMux, bots []*Bot, start time.Time) {
	dash.start = start
	go dash.run(bots)
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(dashboardHTML)
	})
	mux.HandleFunc("/api/overview", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, dash.overview(bots))
	})
	if !coreInstance() { // the overview on disk, so the Legion panels show it while the engine is down
		go func() {
			for range time.Tick(dashOverviewEvery) {
				dash.saveOverview(bots)
			}
		}()
	}
	mux.HandleFunc("/api/overview/history", func(w http.ResponseWriter, r *http.Request) {
		dash.mu.Lock()
		out := append([]historyPoint{}, dash.history...)
		dash.mu.Unlock()
		writeJSON(w, map[string]interface{}{"interval_s": 60, "points": out})
	})
}

// dashOverviewEvery: how often <data>/overview.json is rewritten (the engine's /api/overview, same content).
const dashOverviewEvery = time.Minute

// saveOverview writes the overview as /api/overview returns it (atomic). Offline the dashboard serves this file.
func (d *dashCollector) saveOverview(bots []*Bot) {
	raw, err := json.Marshal(d.overview(bots))
	if err != nil {
		return
	}
	if err := writeFileAtomic(filepath.Join(*dataDir, "overview.json"), raw); err != nil {
		log.Printf("overview: save: %v", err)
	}
}

func (d *dashCollector) bucket(min int64) *minuteBucket {
	b := d.buckets[min]
	if b == nil {
		b = &minuteBucket{Min: min, DeathMaps: map[string]int{}}
		d.buckets[min] = b
	}
	return b
}

// run tails the events ring every 2 s and closes a history point at each minute boundary.
func (d *dashCollector) run(bots []*Bot) {
	var cursor int64
	lastKills, lastLoots := atomic.LoadInt64(&stats.kills), atomic.LoadInt64(&stats.loots)
	curMin := time.Now().Unix() / 60
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		now := time.Now()
		nowMin := now.Unix() / 60
		ev, next := events.since(cursor)
		cursor = next
		k, l := atomic.LoadInt64(&stats.kills), atomic.LoadInt64(&stats.loots)
		d.mu.Lock()
		cb := d.bucket(nowMin)
		cb.Kills += k - lastKills
		cb.Loots += l - lastLoots
		lastKills, lastLoots = k, l
		for i := range ev {
			e := &ev[i]
			m := e.T.Unix() / 60
			if m < nowMin-dashMinutes || m > nowMin {
				m = nowMin
			}
			b := d.bucket(m)
			switch e.Kind {
			case "death":
				b.Deaths++
				b.DeathMaps[e.Map]++
			case "levelup":
				b.LevelUps++
			case "jobchange":
				b.JobChanges++
			case "market":
				if strings.HasPrefix(e.Text, "sold ") { // economy.go: a completed player trade (seller side)
					b.Trades++
				} else if !strings.HasPrefix(e.Text, "bought ") {
					b.ShopsOpen++
				}
			case "said":
				b.Chat++
			case "party":
				if strings.HasPrefix(e.Text, "formed") || strings.HasPrefix(e.Text, "created") {
					b.Parties++
				}
			case "info":
				if strings.HasPrefix(e.Text, "bought ") {
					b.Bought++
				} else if strings.HasPrefix(e.Text, "restocked") {
					b.Restocks++
				}
			}
		}
		for m := range d.buckets {
			if m < nowMin-dashMinutes-1 {
				delete(d.buckets, m)
			}
		}
		closed := nowMin > curMin
		var done minuteBucket
		if closed {
			if b := d.buckets[curMin]; b != nil {
				done = *b
			}
		}
		d.mu.Unlock()
		if closed {
			online, sumLv, fighting := 0, 0, 0
			for _, b := range bots {
				b.mu.Lock()
				if b.Online {
					online++
					sumLv += b.BaseLvOr()
					if stateGroup(b.State) == "fighting" {
						fighting++
					}
				}
				b.mu.Unlock()
			}
			p := historyPoint{T: curMin * 60, Online: online, Kills: done.Kills, Deaths: done.Deaths,
				LevelUps: done.LevelUps, JobChanges: done.JobChanges, Loots: done.Loots, Fighting: fighting}
			if online > 0 {
				p.AvgLevel = round1(float64(sumLv) / float64(online))
			}
			d.mu.Lock()
			d.history = append(d.history, p)
			if len(d.history) > dashMinutes {
				d.history = d.history[len(d.history)-dashMinutes:]
			}
			d.mu.Unlock()
			curMin = nowMin
		}
		<-t.C
	}
}

// window sums the last n minutes of buckets (current partial minute included) and how many minutes that covers.
func (d *dashCollector) window(n int) (minuteBucket, float64) {
	now := time.Now()
	nowMin := now.Unix() / 60
	sum := minuteBucket{DeathMaps: map[string]int{}}
	for m := nowMin - int64(n) + 1; m <= nowMin; m++ {
		b := d.buckets[m]
		if b == nil {
			continue
		}
		sum.Kills += b.Kills
		sum.Deaths += b.Deaths
		sum.Loots += b.Loots
		sum.LevelUps += b.LevelUps
		sum.JobChanges += b.JobChanges
		sum.Bought += b.Bought
		sum.Restocks += b.Restocks
		sum.ShopsOpen += b.ShopsOpen
		sum.Trades += b.Trades
		sum.Parties += b.Parties
		sum.Chat += b.Chat
		for k, v := range b.DeathMaps {
			sum.DeathMaps[k] += v
		}
	}
	// minutes actually covered: window start is clipped to collector start
	from := now.Add(-time.Duration(n-1)*time.Minute - time.Duration(now.Second())*time.Second)
	if d.start.After(from) {
		from = d.start
	}
	cov := now.Sub(from).Minutes()
	if cov < 0.5 {
		cov = 0.5
	}
	return sum, cov
}

// stateGroup folds the free-text AI state into a few buckets.
func stateGroup(s string) string {
	switch {
	case s == "offline (schedule)":
		return "offline (schedule)"
	case strings.HasPrefix(s, "offline"):
		return "offline"
	case s == "login failed":
		return "login failed"
	case strings.HasPrefix(s, "fighting"), strings.HasPrefix(s, "engaging"), strings.HasPrefix(s, "defending"),
		strings.HasPrefix(s, "looting"), strings.HasPrefix(s, "assisting"), strings.HasPrefix(s, "protecting"):
		return "fighting"
	case strings.HasPrefix(s, "healing"), strings.HasPrefix(s, "buffing"):
		return "supporting"
	case strings.HasPrefix(s, "travel"), strings.HasPrefix(s, "no route"):
		return "travel"
	case strings.HasPrefix(s, "following"), strings.HasPrefix(s, "catching up"), strings.HasPrefix(s, "with "),
		strings.HasPrefix(s, "waiting for"):
		return "with party"
	case strings.HasPrefix(s, "roaming"):
		return "roaming"
	case strings.HasPrefix(s, "resting"), strings.HasPrefix(s, "standing up"):
		return "resting"
	case strings.HasPrefix(s, "vending"), strings.HasPrefix(s, "setting up shop"), strings.HasPrefix(s, "walking to market"),
		strings.HasPrefix(s, "vendor"):
		return "vending"
	case strings.HasPrefix(s, "town trip"):
		return "town trip"
	case strings.HasPrefix(s, "hanging out"):
		return "hanging out"
	case s == "afk":
		return "afk"
	case strings.HasPrefix(s, "idle"):
		return "idle"
	case s == "dead":
		return "dead"
	}
	return "other"
}

// classTier: novice / first / second / transcendent / third / expanded
func classTier(c int) string {
	switch {
	case c == 0 || c == 4001 || c == 4023:
		return "novice"
	case c >= 1 && c <= 6, c >= 4002 && c <= 4007:
		return "first"
	case c >= 7 && c <= 21:
		return "second"
	case c >= 4008 && c <= 4022:
		return "transcendent"
	case c >= 4054 && c <= 4079:
		return "third"
	}
	return "expanded"
}

var dungeonPrefixes = []string{"anthell", "orcsdun", "in_sphinx", "prt_maze", "prt_sewb", "moc_pryd", "c_tower", "treasure",
	"juperos", "abyss", "thor_v", "ra_san", "odin_tem", "tha_t", "gl_", "mjo_dun", "ice_dun", "beach_dun", "nyd_dun",
	"xmas_dun", "jupe_", "lhz_dun", "kh_dun", "ein_dun", "mag_dun", "tur_dun", "alde_dun", "iz_dun", "gef_dun", "pay_dun",
	"ama_dun", "lou_dun", "um_dun", "gon_dun", "ayo_dun", "dic_dun", "mosk_dun", "bra_dun", "dew_dun", "ma_dun"}

func mapKind(m string) string {
	if isTown(m) {
		return "town"
	}
	for _, p := range dungeonPrefixes {
		if strings.HasPrefix(m, p) {
			return "dungeon"
		}
	}
	if strings.Contains(m, "_dun") {
		return "dungeon"
	}
	if strings.Contains(m, "fild") || strings.HasPrefix(m, "mjolnir") || strings.Contains(m, "_fild") {
		return "field"
	}
	if strings.HasSuffix(m, "_in") || strings.Contains(m, "_in0") || strings.HasSuffix(m, "_gld") {
		return "indoor"
	}
	return "other"
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

type kv struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func sortedKV(m map[string]int, limit int) []kv {
	out := make([]kv, 0, len(m))
	for k, v := range m {
		out = append(out, kv{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// overview computes the snapshot (cached for 2 s so several open dashboards don't multiply the cost).
func (d *dashCollector) overview(bots []*Bot) map[string]interface{} {
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()
	if d.cache != nil && time.Since(d.cacheAt) < 2*time.Second {
		return d.cache
	}
	t0 := time.Now()

	type planAgg struct {
		Name, Target                       string
		Online, OnlineReached, OnlineLvSum int
		Known, KnownReached, KnownLvSum    int
		Profiles                           int
	}
	type mapAgg struct {
		n, lvSum int
		fighting int
	}
	byRole := map[string]int{}
	states := map[string]int{}
	lvHist := make([]int, 0, 30)
	classes := map[string]int{}
	tiers := map[string]int{}
	plans := map[string]*planAgg{}
	maps := map[string]*mapAgg{}
	var zeny []int
	online, lvSum, jobSum, openShops, vendorsOnline, pinned := 0, 0, 0, 0, 0, 0
	humans := map[string]map[string]interface{}{}
	humanNear := map[string]int{}

	for _, b := range bots {
		b.mu.Lock()
		cs := b.charSpec()
		live := b.BaseLv > 0 // this character has been on the map since Legion started
		lv := b.BaseLvOr()
		if b.pin != nil {
			pinned++
		}
		// plans: every profile's characters count as "profiles"; the current/last-played one carries live data
		for i := range b.P.Chars {
			c := &b.P.Chars[i]
			if c.Plan == "" {
				continue
			}
			pa := plans[c.Plan]
			if pa == nil {
				pa = &planAgg{}
				if bp := builds[c.Plan]; bp != nil {
					pa.Name, pa.Target = bp.Name, className(bp.Class)
				} else {
					pa.Name = c.Plan
				}
				plans[c.Plan] = pa
			}
			pa.Profiles++
		}
		if pa := plans[cs.Plan]; cs.Plan != "" && live && pa != nil {
			reached := false
			if bp := builds[cs.Plan]; bp != nil {
				reached = b.Class == bp.Class || transToSecond[b.Class] == bp.Class
			} else if cs.Target != 0 {
				reached = b.Class == cs.Target || transToSecond[b.Class] == cs.Target
			}
			pa.Known++
			pa.KnownLvSum += lv
			if reached {
				pa.KnownReached++
			}
			if b.Online {
				pa.Online++
				pa.OnlineLvSum += lv
				if reached {
					pa.OnlineReached++
				}
			}
		}
		if !b.Online {
			states[stateGroup(b.State)]++
			b.mu.Unlock()
			continue
		}
		online++
		byRole[cs.Role]++
		g := stateGroup(b.State)
		states[g]++
		lvSum += lv
		jobSum += b.JobLv
		bk := lv / 10
		for len(lvHist) <= bk {
			lvHist = append(lvHist, 0)
		}
		lvHist[bk]++
		classes[className(b.Class)]++
		tiers[classTier(b.Class)]++
		zeny = append(zeny, b.Zeny)
		if cs.Role == "vendor" {
			vendorsOnline++
		}
		if b.vend != nil && b.vend.Opened {
			openShops++
		}
		ma := maps[b.MapName]
		if ma == nil {
			ma = &mapAgg{}
			maps[b.MapName] = ma
		}
		ma.n++
		ma.lvSum += lv
		if g == "fighting" {
			ma.fighting++
		}
		for _, e := range b.ents {
			if e.Type == objPC && e.Name != "" && !allChars[e.Name] {
				if humans[e.Name] == nil {
					humans[e.Name] = map[string]interface{}{"name": e.Name, "map": b.MapName, "x": e.X, "y": e.Y}
				}
				humanNear[e.Name]++
			}
		}
		b.mu.Unlock()
	}

	// levels
	lvBuckets := []map[string]interface{}{}
	for i, n := range lvHist {
		lvBuckets = append(lvBuckets, map[string]interface{}{"from": i * 10, "to": i*10 + 9, "count": n})
	}
	// plans
	planRows := []map[string]interface{}{}
	for id, p := range plans {
		row := map[string]interface{}{"id": id, "name": p.Name, "target": p.Target, "profiles": p.Profiles,
			"online": p.Online, "online_reached": p.OnlineReached, "known": p.Known, "known_reached": p.KnownReached}
		if p.Online > 0 {
			row["online_avg_level"] = round1(float64(p.OnlineLvSum) / float64(p.Online))
		}
		if p.Known > 0 {
			row["known_avg_level"] = round1(float64(p.KnownLvSum) / float64(p.Known))
		}
		planRows = append(planRows, row)
	}
	sort.Slice(planRows, func(i, j int) bool {
		a, b := planRows[i]["online"].(int), planRows[j]["online"].(int)
		if a != b {
			return a > b
		}
		return planRows[i]["id"].(string) < planRows[j]["id"].(string)
	})
	// maps
	mapRows := []map[string]interface{}{}
	kindCount := map[string]int{}
	for name, m := range maps {
		kindCount[mapKind(name)] += m.n
		mapRows = append(mapRows, map[string]interface{}{"map": name, "online": m.n, "kind": mapKind(name),
			"avg_level": round1(float64(m.lvSum) / float64(m.n)), "fighting": m.fighting})
	}
	sort.Slice(mapRows, func(i, j int) bool {
		a, b := mapRows[i]["online"].(int), mapRows[j]["online"].(int)
		if a != b {
			return a > b
		}
		return mapRows[i]["map"].(string) < mapRows[j]["map"].(string)
	})
	mapsTotal := len(mapRows)
	if len(mapRows) > 25 {
		mapRows = mapRows[:25]
	}
	// economy
	sort.Ints(zeny)
	econ := map[string]interface{}{"open_shops": openShops, "vendors_online": vendorsOnline}
	econ["trades_total"], econ["open_wants"], econ["deals_active"] = econSummary() // economy.go
	if n := len(zeny); n > 0 {
		total := int64(0)
		for _, z := range zeny {
			total += int64(z)
		}
		econ["zeny_median"], econ["zeny_p90"], econ["zeny_max"] = zeny[n/2], zeny[n*9/10], zeny[n-1]
		econ["zeny_total_online"] = total
	}
	parties.Lock()
	nParties, nMembers, nHumanLed := len(parties.byName), 0, 0
	for _, p := range parties.byName {
		nMembers += len(p.Members)
		if p.Human != "" {
			nHumanLed++
		}
	}
	parties.Unlock()
	pt := map[string]interface{}{"count": nParties, "members": nMembers, "human_led": nHumanLed, "avg_size": 0.0}
	if nParties > 0 {
		pt["avg_size"] = round1(float64(nMembers) / float64(nParties))
	}
	// humans
	humanRows := []map[string]interface{}{}
	for n, h := range humans {
		h["seen_by"] = humanNear[n]
		humanRows = append(humanRows, h)
	}
	sort.Slice(humanRows, func(i, j int) bool { return humanRows[i]["name"].(string) < humanRows[j]["name"].(string) })
	// activity
	d.mu.Lock()
	act := map[string]interface{}{}
	var deathMaps []kv
	for _, w := range []int{5, 15, 60} {
		s, cov := d.window(w)
		pm := func(v int64) float64 { return round1(float64(v) / cov) }
		act["m"+strconv.Itoa(w)] = map[string]interface{}{
			"minutes_covered": round1(cov),
			"kills":           s.Kills, "deaths": s.Deaths, "levelups": s.LevelUps, "jobchanges": s.JobChanges,
			"bought": s.Bought, "restocks": s.Restocks, "loots": s.Loots, "shops_opened": s.ShopsOpen, "trades": s.Trades,
			"parties_formed": s.Parties, "chat_lines": s.Chat,
			"per_min": map[string]float64{"kills": pm(s.Kills), "deaths": pm(s.Deaths), "levelups": pm(s.LevelUps),
				"jobchanges": pm(s.JobChanges), "loots": pm(s.Loots), "bought": pm(s.Bought), "chat_lines": pm(s.Chat)},
		}
		if w == 60 {
			deathMaps = sortedKV(s.DeathMaps, 12)
		}
	}
	d.mu.Unlock()
	// runtime
	if time.Since(d.lastMemT) > 10*time.Second {
		runtime.ReadMemStats(&d.memStats)
		d.lastMemT = time.Now()
	}
	avgLv, avgJob := 0.0, 0.0
	if online > 0 {
		avgLv, avgJob = round1(float64(lvSum)/float64(online)), round1(float64(jobSum)/float64(online))
	}
	out := map[string]interface{}{
		"time":     time.Now().Unix(),
		"profiles": len(bots),
		"population": map[string]interface{}{
			"online": online, "profiles": len(bots), "by_role": byRole, "states": states, "pinned": pinned,
			"avg_base_level": avgLv, "avg_job_level": avgJob,
		},
		"levels":      lvBuckets,
		"classes":     sortedKV(classes, 0),
		"class_tiers": tiers,
		"plans":       planRows,
		"maps":        mapRows,
		"maps_total":  mapsTotal,
		"map_kinds":   kindCount,
		"activity":    act,
		"death_maps":  deathMaps,
		"economy":     econ,
		"parties":     pt,
		"humans":      humanRows,
		"totals": map[string]int64{"kills": atomic.LoadInt64(&stats.kills), "deaths": atomic.LoadInt64(&stats.deaths),
			"loots": atomic.LoadInt64(&stats.loots), "trades": atomic.LoadInt64(&econTrades), "login_failures": atomic.LoadInt64(&stats.loginFail),
			"packets_in": atomic.LoadInt64(&stats.pktsIn)},
		"runtime": map[string]interface{}{"uptime_s": int(time.Since(d.start).Seconds()), "goroutines": runtime.NumGoroutine(),
			"heap_mb": d.memStats.HeapAlloc >> 20, "sys_mb": d.memStats.Sys >> 20, "gc_count": d.memStats.NumGC,
			"connected": atomic.LoadInt64(&stats.online)},
	}
	out["compute_ms"] = round1(float64(time.Since(t0).Microseconds()) / 1000)
	d.cache, d.cacheAt = out, time.Now()
	return out
}
