package main

// guildduty_api.go: endpoints of guild direction, duty, rank and relations (redesign package G). Handlers take only
// the package's own locks (gduty, grank, grel) and copy what they return - never a bot's lock.
//
//   GET  /api/guild-direction                 every guild's directions in force (leader-set + autopilot)
//   GET  /api/guild-direction/{guild}         one guild (guild01..guild10, or starveil / human-guild)
//   POST /api/guild-direction/{guild}         {"kind":"stock","item":"Elunium","map":"","min_lv":0,"max_lv":0,
//                                              "classes":["hunter"],"note":"...","hours":24,"exclusive":false}
//                                             adds a leader direction (max 3; the oldest goes); {"clear":true} drops
//                                             the leader directions, {"remove":"<id>"} one of them
//   GET  /api/guild-duty[?guild=]             members' standing, today, last 7 days, block; kicked bots; counters
//   POST /api/guild-duty/{guild}/pardon       {"char":"Name"}  standing back to good
//   POST /api/guild-duty/{guild}/kick         {"char":"Name","why":"..."}  queue an expel (exemptions and pacing hold)
//   GET  /api/guild-rank[?hours=24]           rank and wealth now + hourly history
//   GET  /api/guild-relations                 the relations table (value, label, last event)
// The core instance (Ayla's party) runs no duty: it answers with empty data and "instance":"core".

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// registerGuildDuty: endpoints for both instances; the manager runs on the main instance only (guilds.go hook)
func registerGuildDuty(mux *http.ServeMux, bots []*Bot) {
	if !coreInstance() {
		go guildDutyManager(bots)
	}
	mux.HandleFunc("/api/guild-direction", guildDirAPI)
	mux.HandleFunc("/api/guild-direction/", guildDirAPI)
	mux.HandleFunc("/api/guild-duty", guildDutyAPI)
	mux.HandleFunc("/api/guild-duty/", guildDutyAPI)
	mux.HandleFunc("/api/guild-rank", guildRankAPI)
	mux.HandleFunc("/api/guild-relations", guildRelationsAPI)
}

func apiErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func instanceName() string {
	if coreInstance() {
		return "core"
	}
	return "main"
}

// dutyGuildParam: "guild03", "Crimson Oath", "starveil", "human-guild" -> guild id ("" unknown)
func dutyGuildParam(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	l := strings.ToLower(s)
	if l == "starveil" || l == humanGuildID || l == "human" {
		return humanGuildID
	}
	if g := guildByID(l); g != nil {
		return g.ID
	}
	if g := guildByName[l]; g != nil {
		return g.ID
	}
	return ""
}

func allGuildIDs() []string {
	ids := []string{}
	for _, g := range guildDefs {
		ids = append(ids, g.ID)
	}
	return append(ids, humanGuildID)
}

type dirView struct {
	Guild     string            `json:"guild"`
	Name      string            `json:"name"`
	Active    []*GuildDirection `json:"active"`
	Leader    []*GuildDirection `json:"leader_set"`
	Autopilot []*GuildDirection `json:"autopilot"`
}

func dirViewOf(id string, now time.Time) dirView {
	v := dirView{Guild: id, Name: dutyGuildName(id, humanGuildInfo().Guild)}
	gduty.Lock()
	defer gduty.Unlock()
	cp := func(ds []*GuildDirection) []*GuildDirection {
		out := []*GuildDirection{}
		for _, d := range ds {
			c := *d
			out = append(out, &c)
		}
		return out
	}
	v.Active = cp(activeDirsLocked(id, now))
	v.Leader = cp(gduty.dirs[id])
	v.Autopilot = cp(gduty.auto[id])
	return v
}

type dirPost struct {
	Kind      string   `json:"kind"`
	Item      string   `json:"item"`
	ItemID    int      `json:"item_id"`
	Map       string   `json:"map"`
	MinLv     int      `json:"min_lv"`
	MaxLv     int      `json:"max_lv"`
	Classes   []string `json:"classes"`
	Note      string   `json:"note"`
	Hours     float64  `json:"hours"`
	Exclusive bool     `json:"exclusive"`
	Clear     bool     `json:"clear"`
	Remove    string   `json:"remove"`
}

// newLeaderDirection: validates a POST body into a direction (pure apart from the item / map tables)
func newLeaderDirection(guild string, p dirPost, now time.Time) (*GuildDirection, error) {
	k := strings.ToLower(strings.TrimSpace(p.Kind))
	if !validDirKind(k) {
		return nil, fmt.Errorf("kind must be one of %s", strings.Join(dirKinds, ", "))
	}
	d := &GuildDirection{Guild: guild, Kind: k, Map: strings.ToLower(strings.TrimSpace(p.Map)), MinLv: p.MinLv, MaxLv: p.MaxLv,
		Classes: p.Classes, Note: p.Note, SetBy: "leader", Set: now, Exclusive: p.Exclusive}
	if p.ItemID > 0 {
		d.ItemID = p.ItemID
	} else if p.Item != "" {
		hits := findItems(p.Item, 1)
		if len(hits) == 0 {
			return nil, fmt.Errorf("unknown item %q", p.Item)
		}
		d.ItemID = hits[0].ID
	}
	if d.ItemID > 0 {
		if items[d.ItemID] == nil {
			return nil, fmt.Errorf("unknown item id %d", d.ItemID)
		}
		d.Item = itemName(d.ItemID)
	}
	if d.Map != "" && maps[d.Map] == nil {
		return nil, fmt.Errorf("unknown map %q", d.Map)
	}
	if (k == dirStock || k == dirGear) && d.ItemID == 0 {
		return nil, fmt.Errorf("%s needs an item", k)
	}
	if k == dirCarry && d.Map == "" {
		return nil, fmt.Errorf("carry needs a map")
	}
	if d.MaxLv > 0 && d.MinLv > d.MaxLv {
		return nil, fmt.Errorf("min_lv > max_lv")
	}
	h := p.Hours
	if h <= 0 {
		h = guildDirLeaderTTL.Hours()
	}
	d.Expires = now.Add(time.Duration(h * float64(time.Hour)))
	d.ID = fmt.Sprintf("%s-L%d", guild, now.UnixNano()%1000000)
	return d, nil
}

func guildDirAPI(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/guild-direction"), "/")
	if rest == "" {
		if r.Method != http.MethodGet {
			apiErr(w, 405, "POST to /api/guild-direction/{guild}")
			return
		}
		out := []dirView{}
		for _, id := range allGuildIDs() {
			out = append(out, dirViewOf(id, now))
		}
		writeJSON(w, map[string]any{"instance": instanceName(), "duty_on": dutyOn(), "guilds": out})
		return
	}
	id := dutyGuildParam(rest)
	if id == "" {
		apiErr(w, 404, "unknown guild "+rest)
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, dirViewOf(id, now))
		return
	}
	if r.Method != http.MethodPost {
		apiErr(w, 405, "GET or POST")
		return
	}
	if coreInstance() {
		apiErr(w, 409, "guild directions live on the main instance (:8090)")
		return
	}
	var p dirPost
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		apiErr(w, 400, "bad JSON: "+err.Error())
		return
	}
	gduty.Lock()
	switch {
	case p.Clear:
		gduty.dirs[id] = nil
		gduty.dirty = true
		gduty.Unlock()
		writeJSON(w, dirViewOf(id, now))
		return
	case p.Remove != "":
		keep := gduty.dirs[id][:0]
		for _, d := range gduty.dirs[id] {
			if d.ID != p.Remove {
				keep = append(keep, d)
			}
		}
		gduty.dirs[id] = keep
		gduty.dirty = true
		gduty.Unlock()
		writeJSON(w, dirViewOf(id, now))
		return
	}
	gduty.Unlock()
	d, err := newLeaderDirection(id, p, now)
	if err != nil {
		apiErr(w, 400, err.Error())
		return
	}
	gduty.Lock()
	ds := append(gduty.dirs[id], d)
	if len(ds) > guildDirMax {
		ds = ds[len(ds)-guildDirMax:]
	}
	gduty.dirs[id] = ds
	gduty.dirty = true
	dutyCount("leader directions set")
	gduty.Unlock()
	go dutyAnnounce(id, d)
	writeJSON(w, dirViewOf(id, now))
}

// dutyAnnounce: the guild leader bot tells the guild about a new direction (bot guilds; Ayla speaks for herself)
func dutyAnnounce(guild string, d *GuildDirection) {
	l := guildLeader(guild)
	if l == nil || guild == humanGuildID {
		return
	}
	what := d.Kind
	switch {
	case d.Item != "" && d.Map != "":
		what = d.Item + " at " + mapTalk(d.Map)
	case d.Item != "":
		what = d.Item
	case d.Map != "":
		what = d.Kind + " at " + mapTalk(d.Map)
	}
	time.Sleep(time.Duration(2+crand(6)) * time.Second)
	l.mu.Lock()
	if l.Online && l.GuildName != "" {
		line := styled(l.P.Typing, pick("guild focus for today: "+what+". do ur 2h when u can",
			"new guild plan: "+what+", help out when u can pls", "everyone: "+what+" is what we need rn"))
		if !dutyChatDirection(l, line, d) { // guildchat.go
			l.guildSay(line)
		}
	}
	l.mu.Unlock()
}

type dutyMemberView struct {
	Char        string     `json:"char"`
	Guild       string     `json:"guild"`
	Standing    string     `json:"standing"`
	ShortDays   int        `json:"short_days"`
	SinceWarn   int        `json:"short_since_warn,omitempty"`
	Exempt      string     `json:"exempt,omitempty"`
	TodayMin    int        `json:"today_duty_min"`
	TodayOnline int        `json:"today_online_min"`
	Owed        int        `json:"today_owed_min"`
	WarnPending bool       `json:"warn_pending,omitempty"`
	WarnedAt    *time.Time `json:"warned_at,omitempty"`
	KickPending bool       `json:"kick_pending,omitempty"`
	KickAt      *time.Time `json:"kick_at,omitempty"`
	KickWhy     string     `json:"kick_why,omitempty"`
	Block       *dutyBlock `json:"block,omitempty"`
	LastFit     string     `json:"last_plan,omitempty"`
	Days        []dutyDay  `json:"days"`
	Seen        time.Time  `json:"seen"`
}

func guildDutyAPI(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/guild-duty"), "/")
	if rest != "" {
		guildDutyPost(w, r, rest, now)
		return
	}
	filter := dutyGuildParam(r.URL.Query().Get("guild"))
	if r.URL.Query().Get("guild") != "" && filter == "" {
		apiErr(w, 404, "unknown guild")
		return
	}
	tn := tune()
	date := dutyDate(now)
	gduty.Lock()
	var ms []dutyMemberView
	stand := map[string]map[string]int{}
	for _, m := range gduty.members {
		if filter != "" && m.Guild != filter {
			continue
		}
		v := dutyMemberView{Char: m.Char, Guild: m.Guild, Standing: m.Standing, ShortDays: m.Short, SinceWarn: m.SinceWarn,
			Exempt: m.Exempt, WarnPending: m.WarnPending, KickPending: m.KickPending, KickWhy: m.KickWhy, LastFit: m.LastFit,
			Seen: m.Seen}
		if !m.WarnedAt.IsZero() {
			t := m.WarnedAt
			v.WarnedAt = &t
		}
		if m.KickPending {
			t := m.KickAt
			v.KickAt = &t
		}
		if m.Block != nil {
			c := *m.Block
			v.Block = &c
		}
		for _, d := range m.Days {
			v.Days = append(v.Days, *d)
		}
		if d := m.today(date); d != nil {
			v.TodayMin, v.TodayOnline, v.Owed = d.DutyMin, d.OnlineMin, dutyOwed(d.OnlineMin, tn.GuildDutyMinPerDay)
		}
		if stand[m.Guild] == nil {
			stand[m.Guild] = map[string]int{}
		}
		stand[m.Guild][m.Standing]++
		ms = append(ms, v)
	}
	var kicked []dutyKicked
	for _, k := range gduty.kicked {
		if filter == "" || k.From == filter {
			kicked = append(kicked, *k)
		}
	}
	counters := map[string]int{}
	for k, v := range gduty.counters {
		counters[k] = v
	}
	kicksHour := len(gduty.kicks)
	gduty.Unlock()
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].Guild != ms[j].Guild {
			return ms[i].Guild < ms[j].Guild
		}
		return ms[i].Char < ms[j].Char
	})
	sort.Slice(kicked, func(i, j int) bool { return kicked[i].At.After(kicked[j].At) })
	strict := map[string]any{}
	for _, id := range allGuildIDs() {
		s := guildStrictness(id)
		wa, ka := dutyThresholds(tn.GuildDutyWarnDays, tn.GuildDutyKickDays, s)
		strict[id] = map[string]any{"strictness": s, "warn_after_short_days": wa, "kick_after_warned_short_days": ka}
	}
	writeJSON(w, map[string]any{"instance": instanceName(), "duty_on": dutyOn(), "date": date,
		"config": map[string]any{"min_per_day": tn.GuildDutyMinPerDay, "played_day_min": dutyPlayedMin,
			"warn_days": tn.GuildDutyWarnDays, "kick_days": tn.GuildDutyKickDays, "kicks_per_hour": tn.GuildKicksPerHour,
			"kick_cooldown_h": tn.GuildKickCooldownH, "per_guild": strict},
		"standings": stand, "members": ms, "kicked": kicked, "kicks_last_hour": kicksHour, "counters": counters})
}

func guildDutyPost(w http.ResponseWriter, r *http.Request, rest string, now time.Time) {
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || r.Method != http.MethodPost {
		apiErr(w, 404, "POST /api/guild-duty/{guild}/pardon|kick")
		return
	}
	id := dutyGuildParam(parts[0])
	if id == "" {
		apiErr(w, 404, "unknown guild "+parts[0])
		return
	}
	if coreInstance() {
		apiErr(w, 409, "guild duty lives on the main instance (:8090)")
		return
	}
	var p struct {
		Char string `json:"char"`
		Why  string `json:"why"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.Char == "" {
		apiErr(w, 400, `body {"char":"Name"}`)
		return
	}
	gduty.Lock()
	var m *dutyMember
	for _, x := range gduty.members {
		if x.Guild == id && strings.EqualFold(x.Char, p.Char) {
			m = x
			break
		}
	}
	if m == nil {
		gduty.Unlock()
		apiErr(w, 404, "no duty record for "+p.Char+" in "+id)
		return
	}
	switch parts[1] {
	case "pardon":
		m.dutyStanding = dutyStanding{Standing: standGood}
		if m.Exempt != "" {
			m.Standing = standExempt
		}
		m.WarnPending, m.KickPending, m.KickSentAt, m.KickTries = false, false, time.Time{}, 0
		m.Pardons++
		dutyCount("pardons")
		gduty.dirty = true
		v := m.dutyStanding
		gduty.Unlock()
		writeJSON(w, map[string]any{"ok": true, "char": m.Char, "standing": v})
	case "kick":
		switch {
		case m.Exempt != "":
			gduty.Unlock()
			apiErr(w, 409, m.Char+" is never kicked: "+m.Exempt)
			return
		case id == humanGuildID:
			gduty.Unlock()
			apiErr(w, 409, "Starveil's master is Ayla (OpenKore): the engine cannot expel from it")
			return
		}
		m.Standing, m.KickPending, m.KickAt, m.KickSentAt, m.KickTries = standKicked, true, now, time.Time{}, 0
		m.KickWhy = "leader's decision"
		if p.Why != "" {
			m.KickWhy = p.Why
		}
		dutyCount("kicks requested by the leader")
		gduty.dirty = true
		gduty.Unlock()
		writeJSON(w, map[string]any{"ok": true, "queued": true, "char": m.Char,
			"note":    "expelled by the leader bot while both are online, within the kick pacing (guild_kicks_per_hour, 2 h per guild)",
			"duty_on": dutyOn()})
	default:
		gduty.Unlock()
		apiErr(w, 404, "pardon or kick")
	}
}

func guildRankAPI(w http.ResponseWriter, r *http.Request) {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 {
		hours = 24
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	grank.Lock()
	var cur []guildRankRow
	for _, x := range grank.cur {
		cur = append(cur, *x)
	}
	hist := map[string][]guildRankRow{}
	for id, h := range grank.hist {
		for _, x := range h {
			if x.T.After(since) {
				hist[id] = append(hist[id], *x)
			}
		}
	}
	grank.Unlock()
	sort.Slice(cur, func(i, j int) bool { return cur[i].Place < cur[j].Place })
	writeJSON(w, map[string]any{"instance": instanceName(), "rank": cur, "history": hist,
		"how": "score = guild level x10 + avg of the 10 best members' levels + leaderboard points ((101-place)/10 per top-100 member); wealth = shop stock market value + leader/vendor zeny"})
}

func guildRelationsAPI(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	type rv struct {
		guildRelation
		Label  string  `json:"label"`
		NameA  string  `json:"name_a"`
		NameB  string  `json:"name_b"`
		NowVal float64 `json:"-"`
	}
	starveil := humanGuildInfo().Guild
	grel.Lock()
	var out []rv
	for _, x := range grel.m {
		v := relDrift(x.V, now.Sub(x.Drift).Hours()/24)
		c := rv{guildRelation: *x, NowVal: v}
		c.V = float64(int(v*1000)) / 1000
		out = append(out, c)
	}
	grel.Unlock()
	for i := range out {
		out[i].Label = relLabel(out[i].NowVal)
		out[i].NameA, out[i].NameB = dutyGuildName(out[i].A, starveil), dutyGuildName(out[i].B, starveil)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].V < out[j].V })
	writeJSON(w, map[string]any{"instance": instanceName(), "relations": out,
		"scale":  "-1 rival .. +1 ally; unlisted pairs are neutral (0); fades 5%/day toward 0",
		"events": map[string]float64{"poach": relPoach, "fair_trade": relFairTrade, "gift": relGift}})
}
