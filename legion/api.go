package main

// HTTP API for the strategy layer (LLM masterminds) and for humans:
//   GET  /api/stats                      population summary (online, states, maps, kills/hour...)
//   GET  /api/bots[?guild=&map=&state=]  compact list
//   GET  /api/bots/{name}                full detail incl. persona, inventory, nearby counts
//   POST /api/bots/{name}/goal           {"mode":"hunt|idle|follow","map":"prt_fild08","who":"Name"}
//   POST /api/bots/{name}/say            {"text":"..."}            public chat
//   POST /api/bots/{name}/whisper        {"to":"Name","text":"..."}
//   POST /api/guilds/{guild}/goal        same body as bot goal, applied to every member
//   GET  /api/guilds                     member counts, leader + leader_online/level/rank (guildlead.go)
//   GET  /api/guild-leaders[/{guild}]    guild leaders: always-online status, rank, party (guildlead.go)
//   POST /api/guild-leaders/{guild}/party {"map":"","members":[]} form the leader's party (auto-picked)
//   GET  /api/leaderboard[?limit=N]      top characters by level + every guild leader's rank
//   GET  /api/events?since=N             chat heard, whispers, deaths, level-ups (poll with returned "next")
//   GET  /api/spots?level=N              suggested hunting grounds for a level

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type BotSummary struct {
	Name      string `json:"name"`
	Player    string `json:"player"`
	Role      string `json:"role"`
	Class     string `json:"class"`
	Level     int    `json:"level"`
	Guild     string `json:"guild,omitempty"`
	Archetype string `json:"archetype"`
	Online    bool   `json:"online"`
	State     string `json:"state"`
	Map       string `json:"map"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	HP        string `json:"hp"`
	Weight    string `json:"weight,omitempty"` // "cur/max" raw server units (1/10 of the client's figure)
	Goal      Goal   `json:"goal"`
	Kills     int    `json:"kills"`
	Deaths    int    `json:"deaths"`
	Error     string `json:"error,omitempty"`
	PrevError string `json:"prev_error,omitempty"` // loginready.go: the error before the last good login
	// the guild's one leader (guilds.go pickGuildLeaders / in-game master); its Guild is the guild it leads
	GuildLeader bool `json:"guild_leader,omitempty"`
	// healerstick.go: the engine party (who it follows) and the in-game party the server put it in. The field was missing,
	// so tools read "party: None" for Ayla's priest although it was in her party (2026-11-14)
	Party       string `json:"party,omitempty"`
	GameParty   string `json:"game_party,omitempty"`
}

func (b *Bot) summary() BotSummary {
	guild, lead := b.P.Guild, false
	if v := b.leaderVision(); v != nil {
		guild, lead = v.ID, true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return BotSummary{Name: b.charName(), Player: b.P.Name, Role: b.charRole(), Class: className(b.Class), Level: b.BaseLvOr(), Guild: guild, GuildLeader: lead,
		Archetype: b.P.Archetype, Online: b.Online, State: b.State, Map: b.MapName, X: b.X, Y: b.Y,
		HP: strconv.Itoa(b.HP) + "/" + strconv.Itoa(b.MaxHP), Weight: weightStr(b.Weight, b.MaxWeight), Goal: b.Goal, Kills: b.Kills, Deaths: b.Deaths, Error: b.LastErr, PrevError: b.PrevErr,
		Party: partyNameOf(b), GameParty: b.PartyName}
}

// weightStr: "cur/max" raw weight, "" when the limit isn't known yet.
func weightStr(w, max int) string {
	if max <= 0 {
		return ""
	}
	return strconv.Itoa(w) + "/" + strconv.Itoa(max)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	e := json.NewEncoder(w)
	e.SetIndent("", " ")
	e.Encode(v)
}

func serveAPI(addr string, bots []*Bot) {
	byName := map[string]*Bot{}
	for _, b := range bots {
		for _, c := range b.P.Chars {
			byName[strings.ToLower(c.Name)] = b
		}
	}
	start := time.Now()
	mux := http.NewServeMux()
	registerTraceAPI(mux)
	registerSkillAuditAPI(mux) // skillaudit.go: skill requests answered / refused / silent
	registerDebugAPI(mux)      // skillaudit.go: /debug/pprof/ (goroutine dumps when the API hangs)

	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		states, mapsN := map[string]int{}, map[string]int{}
		online := 0
		for _, b := range bots {
			s := b.summary()
			if s.Online {
				online++
				mapsN[s.Map]++
			}
			k := s.State
			if i := strings.IndexAny(k, " "); i > 0 && !strings.HasPrefix(k, "offline") && !strings.HasPrefix(k, "login") {
				k = k[:i]
			}
			states[k]++
		}
		hrs := time.Since(start).Hours()
		writeJSON(w, map[string]interface{}{
			"bots": len(bots), "online": online, "states": states, "maps": mapsN,
			"kills": atomic.LoadInt64(&stats.kills), "deaths": atomic.LoadInt64(&stats.deaths),
			"loots": atomic.LoadInt64(&stats.loots), "login_failures": atomic.LoadInt64(&stats.loginFail),
			"packets_in": atomic.LoadInt64(&stats.pktsIn), "uptime_min": int(time.Since(start).Minutes()),
			"kills_per_hour": int(float64(atomic.LoadInt64(&stats.kills)) / maxf(hrs, 1.0/60)),
		})
	})

	mux.HandleFunc("/api/bots", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var out []BotSummary
		for _, b := range bots {
			s := b.summary()
			if (q.Get("guild") == "" || s.Guild == q.Get("guild")) && (q.Get("map") == "" || s.Map == q.Get("map")) &&
				(q.Get("state") == "" || strings.HasPrefix(s.State, q.Get("state"))) {
				out = append(out, s)
			}
		}
		writeJSON(w, out)
	})

	// owner rule (2026-11-14): save point = the Kafra town nearest the hunting map. Online bots that break it.
	mux.HandleFunc("/api/savecheck", func(w http.ResponseWriter, r *http.Request) {
		var bad []saveMismatch
		online, ok := 0, 0
		for _, b := range bots {
			b.mu.Lock()
			if b.Online && b.MapName != "" {
				hunt := b.Goal.Map
				if hunt == "" || isTown(hunt) || townOf(hunt) != "" {
					hunt = b.MapName
				}
				if !isTown(hunt) && townOf(hunt) == "" {
					online++
					want := nearestTown(hunt)
					if p := partyOf(b); p != nil && p.Human != "" {
						want = humanSaveTown(p) // a human leader's friends save where she saved (together.go)
					}
					saved, known := b.knownSave()
					if want != "" && len(kafraSpots[want]) > 0 && (!known || saved != want) {
						bad = append(bad, saveMismatch{Name: b.charName(), Map: b.MapName, Goal: b.Goal.Map, Saved: saved, Want: want, Known: known})
					} else {
						ok++
					}
				}
			}
			b.mu.Unlock()
		}
		writeJSON(w, map[string]interface{}{"hunting": online, "ok": ok, "mismatched": len(bad), "bots": bad})
	})

	mux.HandleFunc("/api/bots/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/bots/"), "/")
		b := byName[strings.ToLower(parts[0])]
		if b == nil {
			http.Error(w, "no such bot", 404)
			return
		}
		if len(parts) == 1 {
			b.mu.Lock()
			inv := []InvItem{}
			for _, it := range b.inv {
				inv = append(inv, *it)
			}
			sort.Slice(inv, func(i, j int) bool { return inv[i].Index < inv[j].Index })
			nearby := map[string]int{}
			for _, e := range b.ents {
				switch e.Type {
				case objPC:
					nearby["players"]++
				case objMob:
					nearby["monsters"]++
				default:
					nearby["other"]++
				}
			}
			// why each nearby monster is (not) a target: used by tools/legion/classprobe.py and the sentinel
			var mobsDbg []string
			maxLv := b.targetMaxLv()
			for _, e := range b.ents {
				if e.Type != objMob || len(mobsDbg) >= 14 {
					continue
				}
				why := "ok"
				mb := mobs[e.Class]
				dd := dist(b.X, b.Y, e.X, e.Y)
				switch {
				case mb != nil && mb.Boss:
					why = "boss"
				case mb != nil && mb.Level > maxLv:
					why = fmt.Sprintf("too high (lv%d > %d)", mb.Level, maxLv)
				case mb != nil && mb.worthless():
					why = "worthless"
				case b.packAround(e) >= 3 && partyOf(b) == nil:
					why = "pack"
				case dd > 14:
					why = "far"
				case bossSpotNear(b.MapName, e.X, e.Y, 12, b.powerLv()):
					why = "boss spot" // bossnote.go: a threatening boss/champion was noted within 12 cells
				case !claims.free(b.MapName, e.ID, b.aid) && !b.partyClaim(e.ID):
					why = "claimed"
				}
				mobsDbg = append(mobsDbg, fmt.Sprintf("%s d%d seen%ds %s", mobName(e.Class), dd, int(time.Since(e.Seen).Seconds()), why))
			}
			skl := map[int]int{}
			for id, si := range b.skills {
				if si.Lv > 0 {
					skl[id] = si.Lv
				}
			}
			tgtDist := -1
			if e := b.ents[b.target]; e != nil {
				ex, ey := e.pos(time.Now())
				tgtDist = dist(b.X, b.Y, ex, ey)
			}
			d := map[string]interface{}{"skills": skl, "target_dist": tgtDist, "skill_ready_in_ms": time.Until(b.skillReady).Milliseconds(), "mobs": mobsDbg, "target": b.target, "persona": b.P, "online": b.Online, "state": b.State, "map": b.MapName,
				"x": b.X, "y": b.Y, "hp": b.HP, "max_hp": b.MaxHP, "sp": b.SP, "max_sp": b.MaxSP,
				"base_level": b.BaseLv, "job_level": b.JobLv, "zeny": b.Zeny, "weight": b.Weight, "max_weight": b.MaxWeight,
				"goal": b.Goal, "kills": b.Kills, "deaths": b.Deaths, "loots": b.Loots, "inventory": inv,
				"nearby": nearby, "error": b.LastErr, "prev_error": b.PrevErr, "prev_error_cleared": prevErrAt(b.PrevErrAt), "plan": b.planStatus(), "class": className(b.Class),
				"gear_next": b.gearStepView()} // gearpool.go: next gear step (item, cost, funded by, party turn)
			b.mu.Unlock()
			writeJSON(w, d)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", 405)
			return
		}
		var body struct {
			Goal
			Text   string  `json:"text"`
			To     string  `json:"to"`
			Markup float64 `json:"markup"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		switch parts[1] {
		case "goal":
			// a human's party members follow the human, nobody else (a guild mind once sent Ayla's priest to another
			// map, which split her party)
			if inHumanParty(b) {
				http.Error(w, b.charName()+" is in "+humanOf(b)+"'s party and follows them; goals can't be set for it", 409)
				return
			}
			b.mu.Lock()
			body.Goal.Set = "api"
			b.Goal, b.route, b.path = body.Goal, nil, nil
			b.mu.Unlock()
		case "say":
			go b.say(body.Text)
		case "whisper":
			go b.whisper(body.To, body.Text)
		case "party-chat":
			msg := b.charName() + " : " + body.Text
			go b.send(newPkt(0x0108).u16(0).str(msg, len(msg)+1).varlen())
			b.event("said-party", b.charName(), body.Text)
		case "shop": // {"text": "shop title", "markup": 1.5} - reopens the shop with the new title
			var sb struct {
				Text   string  `json:"text"`
				Markup float64 `json:"markup"`
			}
			sb.Text, sb.Markup = body.Text, body.Markup
			b.mu.Lock()
			if b.vend == nil {
				b.vend = &VendState{}
			}
			b.vend.Title, b.vend.Markup = sb.Text, sb.Markup
			rememberShopTitle(b.charName(), sb.Text, sb.Markup) // vend.go: survives reopens and restarts
			reopen := b.vend.Opened
			b.vend.Opened, b.vend.TriedAt = false, time.Time{}
			b.mu.Unlock()
			if reopen {
				go b.send(newPkt(0x012e).b) // close shop; vendThink reopens it with the new title
			}
		default:
			http.Error(w, "unknown action", 404)
			return
		}
		writeJSON(w, map[string]string{"ok": "1"})
	})

	mux.HandleFunc("/api/guilds", func(w http.ResponseWriter, r *http.Request) {
		g := map[string]map[string]int{}
		for _, b := range bots {
			s := b.summary()
			k := s.Guild
			if k == "" {
				k = "(independent)"
			}
			if g[k] == nil {
				g[k] = map[string]int{}
			}
			g[k]["members"]++
			if s.Online {
				g[k]["online"]++
			}
		}
		// additive (guildlead.go): each guild's leader, whether it is online (leaders are always online) and its rank
		res := map[string]map[string]interface{}{}
		for k, v := range g {
			res[k] = map[string]interface{}{}
			for kk, vv := range v {
				res[k][kk] = vv
			}
		}
		for id, s := range leaderSummary(bots) {
			if res[id] == nil {
				res[id] = map[string]interface{}{"members": 0, "online": 0}
			}
			res[id]["leader"], res[id]["leader_online"], res[id]["leader_level"], res[id]["leader_rank"] = s.Name, s.Online, s.Level, s.Rank
		}
		writeJSON(w, res)
	})

	mux.HandleFunc("/api/guilds/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/guilds/"), "/")
		if len(parts) < 2 || parts[1] != "goal" || r.Method != http.MethodPost {
			http.Error(w, "POST /api/guilds/{guild}/goal", 400)
			return
		}
		var gl Goal
		json.NewDecoder(r.Body).Decode(&gl)
		gl.Set = "guild:" + parts[0]
		n := 0
		for _, b := range bots {
			if b.P.Guild == parts[0] && !inHumanParty(b) {
				b.mu.Lock()
				b.Goal, b.route, b.path = gl, nil, nil
				b.mu.Unlock()
				n++
			}
		}
		writeJSON(w, map[string]int{"updated": n})
	})

	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		ev, next := events.since(since)
		writeJSON(w, map[string]interface{}{"events": ev, "next": next})
	})

	// inbox: things players said that a bot could answer (whispers to bots, public lines near bots), deduplicated
	// across the many bots that hear the same line. Lines spoken by Legion characters are marked from_bot.
	// allChars (main.go) holds EVERY profile's characters, also those another instance runs: this instance's `bots`
	// alone missed the core instance (-exclude), and its bots' lines were tagged HUMAN in legionctl (QA 2026-11-14)
	charNames := allChars
	mux.HandleFunc("/api/inbox", func(w http.ResponseWriter, r *http.Request) {
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		// read the whole ring: a line already heard before `since` (same speaker and text within inboxRepeat) was
		// delivered by an earlier call, so later hearers (bots walking up, other bots on the map) don't re-deliver it.
		all, next := events.since(0)
		first := next - int64(len(all)) // id of all[0]
		type msg struct {
			Kind    string   `json:"kind"`
			From    string   `json:"from"`
			FromBot bool     `json:"from_bot"`
			Text    string   `json:"text"`
			Map     string   `json:"map"`
			HeardBy []string `json:"heard_by"`
			Heard   int      `json:"heard_count"`
			T       string   `json:"t"`
		}
		const inboxRepeat = 3 * time.Minute
		var out []*msg
		seen := map[string]*msg{}
		lastAt := map[string]time.Time{} // key -> last time heard (any window)
		for i, e := range all {
			if first+int64(i) < since {
				if e.Kind == "chat" || e.Kind == "party" {
					if j := strings.Index(e.Text, " : "); j > 0 && e.Text[:j] != e.Bot {
						lastAt[e.Kind+"|"+e.Text] = e.T
					}
				}
				continue
			}
			var from, text string
			switch e.Kind {
			case "whisper":
				from, text = e.From, e.Text
			case "chat", "party":
				if i := strings.Index(e.Text, " : "); i > 0 {
					from, text = e.Text[:i], e.Text[i+3:]
				} else {
					continue
				}
			default:
				continue
			}
			if from == e.Bot && len(e.HeardBy) == 0 {
				continue
			}
			k := e.Kind + "|" + from + "|" + text
			if m := seen[k]; m != nil && e.Kind != "whisper" {
				for _, h := range append([]string{e.Bot}, e.HeardBy...) {
					if h != from && !containsStr(m.HeardBy, h) {
						m.Heard++
						if len(m.HeardBy) < 12 {
							m.HeardBy = append(m.HeardBy, h)
						}
					}
				}
				continue
			}
			if e.Kind != "whisper" {
				lk := e.Kind + "|" + from + " : " + text
				if t, ok := lastAt[lk]; ok && e.T.Sub(t) < inboxRepeat {
					lastAt[lk] = e.T // still the same line being heard by more bots: already delivered
					continue
				}
			}
			m := &msg{Kind: e.Kind, From: from, FromBot: charNames[from], Text: text, Map: e.Map, T: e.T.Format("15:04:05")}
			for _, h := range append([]string{e.Bot}, e.HeardBy...) { // party line logged once (eventRing.add)
				if h != from && !containsStr(m.HeardBy, h) && len(m.HeardBy) < 12 {
					m.HeardBy, m.Heard = append(m.HeardBy, h), m.Heard+1
				}
			}
			seen[k] = m
			out = append(out, m)
		}
		writeJSON(w, map[string]interface{}{"messages": out, "next": next})
	})

	// humans: non-Legion players that bots can currently see (where the real players are)
	mux.HandleFunc("/api/humans", func(w http.ResponseWriter, r *http.Request) {
		type seenAt struct {
			Name   string   `json:"name"`
			Map    string   `json:"map"`
			X      int      `json:"x"`
			Y      int      `json:"y"`
			Nearby []string `json:"nearby_bots"`
		}
		found := map[string]*seenAt{}
		for _, b := range bots {
			b.mu.Lock()
			for _, e := range b.ents {
				if e.Type == objPC && e.Name != "" && !charNames[e.Name] {
					h := found[e.Name]
					if h == nil {
						h = &seenAt{Name: e.Name, Map: b.MapName, X: e.X, Y: e.Y}
						found[e.Name] = h
					}
					if len(h.Nearby) < 12 {
						h.Nearby = append(h.Nearby, b.charName())
					}
				}
			}
			b.mu.Unlock()
		}
		out := []*seenAt{}
		for _, h := range found {
			out = append(out, h)
		}
		writeJSON(w, out)
	})

	// market: open vending shops
	mux.HandleFunc("/api/market", func(w http.ResponseWriter, r *http.Request) {
		type shop struct {
			Bot    string   `json:"bot"`
			Player string   `json:"player"`
			Title  string   `json:"title"`
			Open   bool     `json:"open"`
			X      int      `json:"x"`
			Y      int      `json:"y"`
			Items  []string `json:"items"`
		}
		out := []shop{}
		for _, b := range bots {
			b.mu.Lock()
			if b.Online && b.charRole() == "vendor" {
				s := shop{Bot: b.charName(), Player: b.P.Name, X: b.X, Y: b.Y}
				if b.vend != nil {
					s.Title, s.Open = b.vend.Title, b.vend.Opened
				}
				for _, it := range b.cart {
					s.Items = append(s.Items, fmt.Sprintf("%s x%d", itemName(it.ItemID), it.Count))
				}
				out = append(out, s)
			}
			b.mu.Unlock()
		}
		writeJSON(w, out)
	})

	// party: {"leader":"Name","members":["A","B"],"map":"prt_fild08"} forms a party now
	mux.HandleFunc("/api/party", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Leader  string   `json:"leader"`
			Members []string `json:"members"`
			Map     string   `json:"map"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		lead := byName[strings.ToLower(body.Leader)]
		if lead == nil {
			http.Error(w, "no such leader", 404)
			return
		}
		if inHumanParty(lead) {
			http.Error(w, lead.charName()+" is in "+humanOf(lead)+"'s party; pick another leader", 409)
			return
		}
		for _, n := range body.Members {
			if m := byName[strings.ToLower(n)]; m != nil && inHumanParty(m) {
				http.Error(w, m.charName()+" is in "+humanOf(m)+"'s party and can't join another one", 409)
				return
			}
		}
		var ms []*Bot
		for _, n := range body.Members {
			if m := byName[strings.ToLower(n)]; m != nil && m != lead {
				if m.alwaysOnline() {
					continue // a guild leader leads its own party, it is never a member (owner rule 3b)
				}
				if p := partyOf(m); p != nil {
					disband(p)
				}
				ms = append(ms, m)
			}
		}
		if p := partyOf(lead); p != nil {
			disband(p)
		}
		if body.Map == "" {
			lead.mu.Lock()
			body.Map = lead.MapName
			lead.mu.Unlock()
		}
		time.Sleep(500 * time.Millisecond)
		if formParty(lead, ms, body.Map) == nil {
			http.Error(w, "could not form party", 409)
			return
		}
		writeJSON(w, map[string]interface{}{"ok": 1, "members": len(ms) + 1})
	})

	// human-led party: POST {"human":"Admin","members":["<char>",...],"hold":20} pins those characters online and
	// has them party with the human; DELETE ?human=Admin lets them go; GET shows the party and what each member sees.
	mux.HandleFunc("/api/human-party", func(w http.ResponseWriter, r *http.Request) {
		find := func(human string) *Party {
			parties.Lock()
			defer parties.Unlock()
			for _, p := range parties.byName {
				if p.Human != "" && strings.EqualFold(p.Human, human) {
					return p
				}
			}
			return nil
		}
		switch r.Method {
		case http.MethodPost:
			var body struct {
				Human   string   `json:"human"`
				Members []string `json:"members"`
				Hold    int      `json:"hold"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			var ms []*Bot
			var chars []string
			for _, n := range body.Members {
				m := byName[strings.ToLower(n)]
				if m == nil {
					http.Error(w, "no such character "+n, 404)
					return
				}
				for _, c := range m.P.Chars {
					if strings.EqualFold(c.Name, n) {
						chars = append(chars, c.Name)
					}
				}
				ms = append(ms, m)
			}
			if body.Human == "" || len(ms) == 0 {
				http.Error(w, "need human and members", 400)
				return
			}
			if old := find(body.Human); old != nil {
				stopHumanParty(old)
			}
			p := startHumanParty(body.Human, ms, chars, time.Duration(body.Hold)*time.Second)
			saveHumanParties()
			writeJSON(w, map[string]interface{}{"ok": 1, "party": p.Name, "members": chars})
		case http.MethodDelete:
			if p := find(r.URL.Query().Get("human")); p != nil {
				stopHumanParty(p)
				saveHumanParties()
			}
			writeJSON(w, map[string]interface{}{"ok": 1})
		default:
			p := find(r.URL.Query().Get("human"))
			if p == nil {
				http.Error(w, "no human party", 404)
				return
			}
			now := time.Now()
			h, _ := humanAt(p.Human)
			out := map[string]interface{}{"party": p.Name, "human": p.Human, "checkpoint": p.Checkpoint,
				"human_last_seen": map[string]interface{}{"map": h.Map, "x": h.X, "y": h.Y, "ago_s": int(now.Sub(h.At).Seconds())}}
			out["rally"] = rallyView(p, now)
			var mem []map[string]interface{}
			for _, m := range p.Members {
				m.mu.Lock()
				hs, ok := m.humanState(p.Human, now)
				row := map[string]interface{}{"name": m.charName(), "class": className(m.Class), "level": m.BaseLvOr(),
					"online": m.Online, "state": m.State, "map": m.MapName, "x": m.X, "y": m.Y,
					"hp": fmt.Sprintf("%d/%d", m.HP, m.MaxHP), "sp": fmt.Sprintf("%d/%d", m.SP, m.MaxSP), "weight": weightStr(m.Weight, m.MaxWeight),
					"in_party": m.PartyID != 0, "party_roster": m.roster, "goal": m.Goal, "waiting_at_checkpoint": m.waitingAtCheckpoint,
					"power": m.powerLv(), "kills": m.Kills, "deaths": m.Deaths} // gearscore.go: gear-rated level; partylead.pl's map choice and step
				if st, known := m.knownSave(); known {
					row["saved_town"] = st // together.go: Ayla's departure gate waits until every friend saved at the rally Kafra
				}
				if ok && hs.Map == m.MapName {
					row["sees_human"] = map[string]interface{}{"x": hs.X, "y": hs.Y, "dist": dist(m.X, m.Y, hs.X, hs.Y),
						"hp": hs.HPFrac, "target": hs.Target}
				}
				m.mu.Unlock()
				mem = append(mem, row)
			}
			out["members"] = mem
			writeJSON(w, out)
		}
	})

	// party rally (rally.go): POST /api/rally?party=<party name or human or leader>  -> meet at the destination's Kafra
	mux.HandleFunc("/api/rally", func(w http.ResponseWriter, r *http.Request) {
		q := strings.ToLower(r.URL.Query().Get("party"))
		var p *Party
		parties.Lock()
		for _, pp := range parties.byName {
			if strings.ToLower(pp.Name) == q || strings.ToLower(pp.Human) == q || (pp.Leader != nil && strings.ToLower(pp.Leader.charName()) == q) {
				p = pp
			}
		}
		parties.Unlock()
		if p == nil {
			http.Error(w, "no such party", 404)
			return
		}
		if r.Method == http.MethodPost {
			ok := p.startRally(p.rallyDest(), "requested through the API", time.Now())
			writeJSON(w, map[string]interface{}{"ok": ok, "rally": rallyView(p, time.Now())})
			return
		}
		writeJSON(w, rallyView(p, time.Now()))
	})

	mux.HandleFunc("/api/parties", func(w http.ResponseWriter, r *http.Request) {
		parties.Lock()
		out := []map[string]interface{}{}
		for _, p := range parties.byName {
			var ms []string
			for _, m := range p.Members {
				ms = append(ms, m.charName())
			}
			out = append(out, map[string]interface{}{"name": p.Name, "leader": p.Leader.charName(), "map": p.Map, "members": ms})
		}
		parties.Unlock()
		writeJSON(w, out)
	})

	// memory: GET /api/memory/{name}[?diary=40]  POST {"note":"...","fact":"...","goal":"...","goal_done":"...",
	//   "person":"Name","relation":"friend","person_note":"..."}   (name = any character of the player)
	mux.HandleFunc("/api/memory/", func(w http.ResponseWriter, r *http.Request) {
		b := byName[strings.ToLower(strings.TrimPrefix(r.URL.Path, "/api/memory/"))]
		if b == nil {
			http.Error(w, "no such player", 404)
			return
		}
		m := memoryOf(b.P)
		if r.Method == http.MethodPost {
			var body struct {
				Note, Fact, Goal, GoalDone, Person, Relation, PersonNote string
			}
			var raw map[string]string
			json.NewDecoder(r.Body).Decode(&raw)
			body.Note, body.Fact, body.Goal, body.GoalDone = raw["note"], raw["fact"], raw["goal"], raw["goal_done"]
			body.Person, body.Relation, body.PersonNote = raw["person"], raw["relation"], raw["person_note"]
			if body.Note != "" {
				m.add(body.Note)
			}
			m.mu.Lock()
			if body.Fact != "" {
				m.Facts = append(m.Facts, body.Fact)
				if len(m.Facts) > 40 {
					m.Facts = m.Facts[len(m.Facts)-40:]
				}
			}
			if body.Goal != "" {
				m.Goals = append(m.Goals, MemGoal{Text: body.Goal, Status: "active", Created: time.Now()})
			}
			if body.GoalDone != "" {
				for i := range m.Goals {
					if strings.Contains(strings.ToLower(m.Goals[i].Text), strings.ToLower(body.GoalDone)) {
						m.Goals[i].Status = "done"
					}
				}
			}
			if body.Person != "" {
				p := m.People[body.Person]
				if p == nil {
					p = &Person{Relation: "stranger", LastSeen: time.Now()}
					m.People[body.Person] = p
				}
				if body.Relation != "" {
					p.Relation = body.Relation
				}
				if body.PersonNote != "" {
					p.Notes = body.PersonNote
				}
			}
			m.dirty = true
			m.mu.Unlock()
			writeJSON(w, map[string]string{"ok": "1"})
			return
		}
		n, _ := strconv.Atoi(r.URL.Query().Get("diary"))
		if n <= 0 {
			n = 30
		}
		snap := m.snapshot(n)
		b.mu.Lock()
		snap["now"] = map[string]interface{}{"character": b.charName(), "role": b.charRole(), "class": className(b.Class),
			"level": b.BaseLvOr(), "online": b.Online, "map": b.MapName, "doing": b.State, "zeny": b.Zeny,
			"party": b.PartyName, "guild": b.P.Guild}
		b.mu.Unlock()
		snap["persona"] = b.P
		writeJSON(w, snap)
	})

	mux.HandleFunc("/api/spots", func(w http.ResponseWriter, r *http.Request) {
		lv, _ := strconv.Atoi(r.URL.Query().Get("level"))
		writeJSON(w, SpotsFor(lv, 0.5))
	})

	registerDashboard(mux, bots, start) // dashboard.go: /dashboard, /api/overview
	registerGuilds(mux, bots)           // guilds.go: /api/guilds/detail
	registerPartyDash(mux)              // partydash.go: /api/parties/detail
	registerMeter(mux, bots)            // meterapi.go: /api/meter (kills/min, zeny/h, time budget, cohorts, Neo)
	if !coreInstance() {                // the minds bus lives on the main instance only (instances.go)
		registerMinds(mux) // minds.go: /api/minds (LLM minds' thoughts + owner messages)
	}
	registerEconomy(mux)                        // economy.go: /api/economy (wants, offers, trades, prices)
	registerLookup(mux)                         // lookup.go: /api/lookup/item|monster|map (drop/spawn tables for the minds)
	registerMapSeen(mux)                        // seen.go: /api/mapseen (monsters in view per map; empty maps)
	registerFriends(mux)                        // friends.go: /api/friends (friendships, guild changes due to friends)
	registerLoyalty(mux)                        // loyalty.go: /api/guild-loyalty (loyalty, help requests, keep gifts)
	registerExchange(mux, bots)                 // exchange.go: /api/exchange, /api/chat-jobs, /api/chat-intent
	http.ListenAndServe(addr, coreForward(mux)) // instances.go: core-party requests go to the core instance
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
