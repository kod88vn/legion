package main

// Guild leaders (owner, RULES.md 3b, 2026-11-13): "keep the guild leaders' accounts always online"; "guild leaders
// need to be smart enough to form the right party and level up fast. They should be on the leaderboard of the
// server since Haiku drives them."
//
//   - alwaysOnline: a leader (pickGuildLeaders, guilds.go) ignores the play schedule, never "calls it a day", doesn't
//     wait for an online slot, isn't kicked by human-party pins, and the run loop relogs it after any disconnect
//     (bot.go run). It plays its main character and skips the town hang-out after login (levelling first).
//   - leaderPartyManager: every minute an online leader that isn't leading a party gets a full tank / priest /
//     ranged party from its guild (in-game roster first, persona guild as fallback), within 10 levels, at the best
//     party spot of the strategy guide (strategy.go guideSpots) for the party's level range.
//   - levelBook + /api/leaderboard: the server's top characters (Legion bots, last known level kept in
//     data/levels.json so offline characters keep their place) and the rank of every guild leader.
//   - /api/guild-leaders: each leader's status, rank and party; POST /api/guild-leaders/{guildNN}/party forms the
//     leader's party (auto-picked members unless given) at a map (default: the guide's best spot).
// Core instance (instances.go) has no leaders: none of this runs there.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// alwaysOnline: guild leaders stay logged in at all times.
func (b *Bot) alwaysOnline() bool { return b.leaderVision() != nil }

// leaderRelogs counts how often each leader was logged back in after a session ended (account -> n).
var leaderRelogs sync.Map

func noteLeaderRelog(b *Bot, why string) {
	n := 1
	if v, ok := leaderRelogs.Load(b.P.Account); ok {
		n = v.(int) + 1
	}
	leaderRelogs.Store(b.P.Account, n)
	b.event("guild", "", fmt.Sprintf("guild leader disconnected (%s); logging back in", why))
}

// ---------------------------------------------------------------- level book / leaderboard

type lvEntry struct {
	Name    string `json:"name"`
	Player  string `json:"player"`
	Account string `json:"account"`
	Class   string `json:"class"`
	Level   int    `json:"level"`
	Job     int    `json:"job_level"`
	Exp     int64  `json:"exp"`
	Guild   string `json:"guild,omitempty"` // in-game guild name
	Seen    int64  `json:"seen"`            // unix time last seen online
}

var levelBook = struct {
	sync.Mutex
	m map[string]*lvEntry // char name -> entry
}{m: map[string]*lvEntry{}}

func levelBookPath() string { return filepath.Join(*dataDir, "levels.json") }

func loadLevelBook() {
	raw, err := os.ReadFile(levelBookPath())
	if err != nil {
		return
	}
	var m map[string]*lvEntry
	if json.Unmarshal(raw, &m) == nil {
		levelBook.Lock()
		for k, v := range m {
			levelBook.m[k] = v
		}
		levelBook.Unlock()
	}
}

func updateLevelBook(bots []*Bot, save bool) {
	now := time.Now().Unix()
	for _, b := range bots {
		b.mu.Lock()
		if b.Online && b.BaseLv > 0 && b.cur != nil {
			e := &lvEntry{Name: b.charName(), Player: b.P.Name, Account: b.P.Account, Class: className(b.Class),
				Level: b.BaseLv, Job: b.JobLv, Exp: b.BaseExp, Guild: b.GuildName, Seen: now}
			levelBook.Lock()
			levelBook.m[e.Name] = e
			levelBook.Unlock()
		}
		b.mu.Unlock()
	}
	if !save {
		return
	}
	levelBook.Lock()
	raw, err := json.Marshal(levelBook.m)
	levelBook.Unlock()
	if err == nil {
		tmp := levelBookPath() + ".tmp"
		if os.WriteFile(tmp, raw, 0644) == nil {
			os.Rename(tmp, levelBookPath())
		}
	}
}

type rankRow struct {
	Rank     int    `json:"rank"`
	Name     string `json:"name"`
	Player   string `json:"player"`
	Class    string `json:"class"`
	Level    int    `json:"level"`
	Job      int    `json:"job_level"`
	Guild    string `json:"guild,omitempty"`
	Online   bool   `json:"online"`
	Leader   string `json:"leader_of,omitempty"` // guild id when this is a guild leader
	LastSeen string `json:"last_seen,omitempty"`
}

// leaderboard: every known character ranked by base level, then base exp (competition ranking: ties share a
// rank, the rank is 1 + how many characters are strictly ahead).
func leaderboard(bots []*Bot) []rankRow {
	online := map[string]bool{}
	leaderOf := map[string]string{} // char name -> guild id
	for _, b := range bots {
		b.mu.Lock()
		if b.Online {
			online[b.charName()] = true
		}
		b.mu.Unlock()
	}
	for _, g := range guildDefs {
		if l := guildLeader(g.ID); l != nil {
			l.mu.Lock()
			leaderOf[l.charName()] = g.ID
			l.mu.Unlock()
		}
	}
	levelBook.Lock()
	es := make([]lvEntry, 0, len(levelBook.m))
	for _, e := range levelBook.m {
		es = append(es, *e)
	}
	levelBook.Unlock()
	// leaders never seen online yet still get a row (level 1)
	for name := range leaderOf {
		found := false
		for _, e := range es {
			if e.Name == name {
				found = true
				break
			}
		}
		if !found {
			es = append(es, lvEntry{Name: name, Level: 1, Job: 1, Class: "Novice"})
		}
	}
	sort.Slice(es, func(i, j int) bool {
		if es[i].Level != es[j].Level {
			return es[i].Level > es[j].Level
		}
		if es[i].Exp != es[j].Exp {
			return es[i].Exp > es[j].Exp
		}
		return es[i].Name < es[j].Name
	})
	rows := make([]rankRow, len(es))
	for i, e := range es {
		r := i + 1
		if i > 0 && es[i-1].Level == e.Level && es[i-1].Exp == e.Exp {
			r = rows[i-1].Rank
		}
		rows[i] = rankRow{Rank: r, Name: e.Name, Player: e.Player, Class: e.Class, Level: e.Level, Job: e.Job,
			Guild: e.Guild, Online: online[e.Name], Leader: leaderOf[e.Name]}
		if e.Seen > 0 && !online[e.Name] {
			rows[i].LastSeen = time.Unix(e.Seen, 0).UTC().Format(time.RFC3339)
		}
	}
	return rows
}

// ---------------------------------------------------------------- leader parties

// guildMates: online characters free to join the leader's party: in the leader's in-game guild first, then players
// whose persona belongs to the guild (recruits-to-be). No vendors, no human-party members, nobody already partied.
func guildMates(l *Bot, g *GuildDef, bots []*Bot) []*Bot {
	l.mu.Lock()
	gname := l.GuildName
	l.mu.Unlock()
	if gname == "" {
		gname = g.Name
	}
	var in, persona []*Bot
	for _, b := range bots {
		if b == l || b.alwaysOnline() {
			continue
		}
		b.mu.Lock()
		ok := b.Online && b.pin == nil && b.vend == nil && b.charRole() != "vendor" && b.basicLv() >= 5
		member := strings.EqualFold(b.GuildName, gname)
		pg := b.P.Guild == g.ID && b.GuildName == ""
		b.mu.Unlock()
		if !ok || partyOf(b) != nil {
			continue
		}
		if !member {
			if v, ok := guildRoster.Load(b.P.Account); ok && strings.EqualFold(v.(string), gname) {
				member = true
			}
		}
		switch {
		case member:
			in = append(in, b)
		case pg:
			persona = append(persona, b)
		}
	}
	return append(in, persona...)
}

// pickLeaderParty: tank, support, ranged, then a 2nd ranged / melee, each the closest in level to the leader
// (within 10 levels so even EXP share works); the leader fills its own role.
func pickLeaderParty(l *Bot, cands []*Bot) []*Bot {
	l.mu.Lock()
	lv, lrole := l.BaseLvOr(), partyRole(l)
	l.mu.Unlock()
	used := map[*Bot]bool{}
	pick := func(role int) *Bot {
		var best *Bot
		bd := 11
		for _, c := range cands {
			if used[c] {
				continue
			}
			c.mu.Lock()
			r, d := partyRole(c), abs(c.BaseLvOr()-lv)
			c.mu.Unlock()
			if r == role && d < bd {
				best, bd = c, d
			}
		}
		return best
	}
	want := []int{roleTank, roleSupport, roleRanged, roleRanged, roleMelee, roleTank}
	need := map[int]bool{lrole: true} // the leader covers its own role once
	var out []*Bot
	for _, role := range want {
		if len(out) >= 3 {
			break
		}
		if need[role] {
			delete(need, role)
			continue
		}
		if c := pick(role); c != nil {
			used[c] = true
			out = append(out, c)
		}
	}
	return out
}

// bestPartySpot: the guide spot with the most EXP the whole party can handle - the highest-level spot whose range
// covers every member, party spots first, AoE-friendly mobby spots when there's a mage.
func bestPartySpot(members []*Bot) string {
	if len(members) == 0 {
		return ""
	}
	// QA 2026-11-14 (guild06-10 minds): leaders' parties travelled to targets they never reached, bouncing between
	// Prontera/Payon/Morroc; beginner leaders (lv1-14) went to Payon while their party stood on prt_fild01. Now: levels
	// are EFFECTIVE levels (gearpower.go: starter gear drops bands), the spot must be reachable and close to the
	// leader (hops cost score), beginners stay within 3 maps of their home town, and the pick is kept for 20 minutes
	// while it still fits (no re-pick every leaderPartyManager minute).
	l := members[0]
	minLv, maxLv, aoe, allRanged, fitLv, minBase := 999, 0, false, true, 999, 999
	l.mu.Lock()
	from, fx, fy, lname := l.MapName, l.X, l.Y, l.P.Name
	l.mu.Unlock()
	for _, m := range members {
		m.mu.Lock()
		if m.BaseLv <= 0 { // level not known yet (logging in): the persona's lv1 default would drag the pick to Novice fields
			m.mu.Unlock()
			continue
		}
		lv, cls, r, tl, base := m.powerLv(), m.Class, partyRole(m), m.targetMaxLv(), m.BaseLvOr()
		if fragileClass(cls) {
			lv = max(1, lv-partyFragileLv) // ai.go: low HP, dies first in a crowd
		}
		m.mu.Unlock()
		minLv, maxLv, fitLv, minBase = min(minLv, lv), max(maxLv, lv), min(fitLv, tl), min(minBase, base)
		aoe = aoe || aoeFamily(cls)
		allRanged = allRanged && (r == roleRanged || r == roleSupport)
	}
	if b := partyStrength(members); b > 0 && minLv < 999 { // lfg.go: a composed party fights above its weakest member
		minLv, fitLv = min(minLv+b, max(minLv, minBase)), fitLv+b
	}
	if minLv < 999 && gearPerfOn() { // gearscore.go: the party's measured step (deaths down, easy farming up)
		o := partyPerfOffset(members)
		minLv, fitLv = max(1, minLv+o), max(1, fitLv+o)
	}
	now := time.Now()
	fits := func(g GuideSpot) bool {
		if bad, _ := leaderAvoids(lname, g.Map, now); bad {
			return false // party.go leaderMem: this leader's parties died / kept fleeing there lately (QA guild07)
		}
		return maps[g.Map] != nil && g.Min <= minLv+2 && maxLv <= g.Max+3 && (!g.Ranged || allRanged) && fitShare(g.Map, fitLv) >= minFitShare &&
			recentDeathsFor(g.Map, minBase) < 4 && !swarmVeto(g.Map, minLv) && !mapReadsEmpty(g.Map) && !bossRecent(g.Map, minLv) && // swarm.go, seen.go, bossnote.go
			!partyMapTooHard(g.Map, minLv) && !partyMapDeadly(g.Map) // ai.go: monsters (and their aggression) not over the weakest member
	}
	partySpotPick.Lock()
	prev := partySpotPick.m[lname]
	partySpotPick.Unlock()
	if prev.m != "" && time.Since(prev.at) < 20*time.Minute {
		for _, g := range guideSpots {
			if g.Map == prev.m && fits(g) && !mapCrowded(prev.m) { // crowd.go: a full map is given up
				return prev.m
			}
		}
	}
	beginner := minBase < beginnerLv()
	best, bs := "", -1e9
	// crowd.go: a map with room for the party first (the party's own map: not crowded), then any fitting one
	for pass := 0; pass < 2 && best == ""; pass++ {
		for _, g := range guideSpots {
			if !fits(g) {
				continue // the weakest member's target filter must accept most monsters there
			}
			if pass == 0 && (g.Map == from && mapCrowded(g.Map) || g.Map != from && !mapHasRoom(g.Map, len(members))) {
				continue
			}
			h := 0
			if from != "" && g.Map != from {
				h = mapHops(from, fx, fy, g.Map)
			}
			if h > 8 {
				continue // unreachable or across the world
			}
			l.mu.Lock()
			nh := l.nearHome(g.Map)
			l.mu.Unlock()
			if beginner && nh > 3 {
				continue // beginners level around their home town (owner rule)
			}
			s := float64(g.Min) - 1.5*float64(h)
			if mapExpPoor(g.Map, minBase) {
				s -= 15 // ai.go: little exp for the weakest member's base level - only when nothing better fits
			}
			if _, gone := leaderAvoids(lname, g.Map, now); gone {
				s -= 20 // this leader's party left it < 30 min ago: not straight back (re-forming used to forget it)
			}
			if g.Party && len(members) >= 3 {
				s += 6
			}
			if g.Mobby {
				if aoe {
					s += 3
				} else {
					s -= 4
				}
			}
			if s > bs {
				best, bs = g.Map, s
			}
		}
	}
	if best != "" {
		partySpotPick.Lock()
		partySpotPick.m[lname] = spotPick{best, time.Now()}
		partySpotPick.Unlock()
	}
	return best
}

// partySpotPick: the last spot bestPartySpot chose per leader (kept 20 min while it fits)
type spotPick struct {
	m  string
	at time.Time
}

var partySpotPick = struct {
	sync.Mutex
	m map[string]spotPick
}{m: map[string]spotPick{}}

// formLeaderParty forms the leader's party (members auto-picked when nil) at mapName (best spot when empty).
func formLeaderParty(l *Bot, g *GuildDef, bots []*Bot, members []*Bot, mapName string) (*Party, string, error) {
	if inHumanParty(l) {
		return nil, "", fmt.Errorf("%s is in a human's party", l.charName())
	}
	if members == nil {
		members = pickLeaderParty(l, guildMates(l, g, bots))
	}
	if len(members) == 0 {
		return nil, "", fmt.Errorf("no free guild members within 10 levels of %s online", l.charName())
	}
	if mapName == "" {
		mapName = bestPartySpot(append([]*Bot{l}, members...))
	}
	if mapName == "" || isTown(mapName) {
		return nil, "", fmt.Errorf("no hunting spot found for the party")
	}
	if p := partyOf(l); p != nil {
		if p.Human != "" {
			return nil, "", fmt.Errorf("%s is in a human's party", l.charName())
		}
		disband(p)
	}
	for _, m := range members {
		if p := partyOf(m); p != nil && p.Human == "" {
			disband(p)
		}
	}
	time.Sleep(500 * time.Millisecond)
	p := formParty(l, members, mapName)
	if p == nil {
		return nil, mapName, fmt.Errorf("could not form the party")
	}
	return p, mapName, nil
}

// leaderPartyManager: an online leader always levels in a full party from its guild.
func leaderPartyManager(bots []*Bot, stop <-chan struct{}) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		for _, g := range guildDefs {
			l := guildLeader(g.ID)
			if l == nil {
				continue
			}
			l.mu.Lock()
			ready := l.Online && l.pin == nil && l.basicLv() >= 7 && l.trip == nil && !l.Dead
			l.mu.Unlock()
			if !ready {
				continue
			}
			p := partyOf(l)
			if goalPartiesOn() {
				// lfg.go: the leader's party forms around the leader's goal and ends with it; LFM tops it up
				if p == nil {
					leaderGoalParty(l, g, bots)
				}
				continue
			}
			if p != nil && (p.Leader == l || p.Human != "") && len(p.Members) >= 3 {
				continue // leading a party already (or with a human)
			}
			ms := pickLeaderParty(l, guildMates(l, g, bots))
			// a party of 2 is only worth leaving another party / a smaller one for if it brings a support or a tank
			if len(ms) < 2 && p != nil {
				continue
			}
			if len(ms) == 0 {
				continue
			}
			// re-forming keeps the party's map while it fits (formParty re-fits it from the leader's position otherwise):
			// a top-up must not send the party somewhere new
			keep := ""
			if p != nil && p.Leader == l && p.Map != "" && !isTown(p.Map) {
				keep = p.Map
			}
			if _, m, err := formLeaderParty(l, g, bots, ms, keep); err == nil {
				l.event("guild", "", fmt.Sprintf("guild leader formed a %d-member guild party for %s", len(ms)+1, m))
			}
		}
	}
}

// leaderGoalParty (lfg.go need parties): the guild leader picks its own goal's map (an item goal's drop map when safe
// for the group, else the best spot for the group) and recruits guild mates whose goals that map serves (flexible
// zeny/level goals, healers, or the same drop). Announced in guild chat. No mates: the leader farms and posts LFM
// in #global through lfg.go like any other farmer.
func leaderGoalParty(l *Bot, g *GuildDef, bots []*Bot) {
	l.mu.Lock()
	fg := l.fgoal
	typing := l.P.Typing
	l.mu.Unlock()
	if fg == nil {
		return
	}
	mates := guildMates(l, g, bots)
	compat := func(m string) []*Bot {
		var out []*Bot
		for _, c := range mates {
			c.mu.Lock()
			cg := c.fgoal
			c.mu.Unlock()
			if cg == nil || cg.serves(m) {
				out = append(out, c)
			}
		}
		return out
	}
	var ms []*Bot
	m, anchor := "", ""
	if fg.mapped() {
		for _, fm := range fg.Maps {
			if c := pickLeaderParty(l, compat(fm)); len(c) > 0 && goalMapSafeItem(append([]*Bot{l}, c...), fm, fg.farmItem()) {
				ms, m, anchor = c, fm, fm
				break
			}
		}
	}
	if m == "" {
		var flex []*Bot
		for _, c := range mates {
			c.mu.Lock()
			cg := c.fgoal
			c.mu.Unlock()
			if cg == nil || !cg.fixed() {
				flex = append(flex, c)
			}
		}
		ms = pickLeaderParty(l, flex)
		if len(ms) == 0 {
			return
		}
		m = bestPartySpot(append([]*Bot{l}, ms...))
	}
	if m == "" || isTown(m) || len(ms) == 0 {
		return
	}
	purpose := fg.desc()
	if anchor == "" && fg.mapped() {
		purpose = "lvling" // the group's spot, not the leader's drop map
	}
	l.guildSay(styled(typing, pick("guild pt for "+purpose+" at "+mapTalk(m)+", who's in?", "forming a guild party, "+mapTalk(m)+", "+purpose,
		"party up guys, we go "+mapTalk(m))))
	p := formPartyOpt(l, ms, m, anchor == "", func(p *Party) {
		p.Need, p.Purpose, p.Anchor = true, purpose+" @ "+m, anchor
		if anchor != "" {
			p.AnchorItem = fg.farmItem()
		}
	})
	if p != nil {
		lfgStats.Lock()
		lfgStats.formed++
		lfgStats.joins += len(ms)
		lfgStats.Unlock()
		l.mu.Lock()
		l.event("guild", "", fmt.Sprintf("guild leader formed a %d-member party for %s (%s)", len(ms)+1, m, purpose))
		l.mu.Unlock()
	}
}

// ---------------------------------------------------------------- API

type leaderStatus struct {
	Guild        string   `json:"guild"`
	GuildName    string   `json:"guild_name"`
	Name         string   `json:"name"`
	Player       string   `json:"player"`
	AlwaysOnline bool     `json:"always_online"`
	Online       bool     `json:"online"`
	State        string   `json:"state"`
	Map          string   `json:"map,omitempty"`
	Class        string   `json:"class"`
	Level        int      `json:"level"`
	JobLevel     int      `json:"job_level"`
	HP           int      `json:"hp"`
	MaxHP        int      `json:"max_hp"`
	SP           int      `json:"sp"`
	MaxSP        int      `json:"max_sp"`
	Zeny         int      `json:"zeny"`
	Founded      bool     `json:"founded"`
	Rank         int      `json:"rank"`
	Ranked       int      `json:"ranked_of"`
	Relogs       int      `json:"relogs"`
	LastError    string   `json:"last_error,omitempty"`
	Party        string   `json:"party,omitempty"`
	PartyMap     string   `json:"party_map,omitempty"`
	PartyMembers []string `json:"party_members,omitempty"`
}

func leaderStatuses(bots []*Bot) []leaderStatus {
	board := leaderboard(bots)
	rank := map[string]int{}
	for _, r := range board {
		rank[r.Name] = r.Rank
	}
	var out []leaderStatus
	for _, g := range guildDefs {
		l := guildLeader(g.ID)
		if l == nil {
			continue
		}
		l.mu.Lock()
		s := leaderStatus{Guild: g.ID, GuildName: g.Name, Name: l.charName(), Player: l.P.Name, AlwaysOnline: true,
			Online: l.Online, State: l.State, Map: l.MapName, Class: className(l.Class), Level: l.BaseLvOr(),
			JobLevel: l.JobLv, HP: l.HP, MaxHP: l.MaxHP, SP: l.SP, MaxSP: l.MaxSP, Zeny: l.Zeny,
			Founded: l.GuildID != 0 && l.guildMaster, LastError: l.LastErr, Ranked: len(board)}
		l.mu.Unlock()
		s.Rank = rank[s.Name]
		if v, ok := leaderRelogs.Load(l.P.Account); ok {
			s.Relogs = v.(int)
		}
		if p := partyOf(l); p != nil {
			s.Party, s.PartyMap = p.Name, p.Map
			for _, m := range p.Members {
				m.mu.Lock()
				s.PartyMembers = append(s.PartyMembers, fmt.Sprintf("%s (%s %d)", m.charName(), className(m.Class), m.BaseLvOr()))
				m.mu.Unlock()
			}
		}
		out = append(out, s)
	}
	return out
}

func leaderSummary(bots []*Bot) map[string]leaderStatus {
	m := map[string]leaderStatus{}
	for _, s := range leaderStatuses(bots) {
		m[s.Guild] = s
	}
	return m
}

// registerGuildLeaders: /api/leaderboard, /api/guild-leaders (main instance; called from registerGuilds).
func registerGuildLeaders(mux *http.ServeMux, bots []*Bot) {
	if !coreInstance() {
		loadLevelBook()
		go func() {
			n := 0
			for range time.Tick(time.Minute) {
				n++
				updateLevelBook(bots, n%5 == 0)
			}
		}()
		stop := make(chan struct{}) // lives as long as the process
		go leaderPartyManager(bots, stop)
	}
	// GET /api/leaderboard[?limit=N]: top characters + where each guild leader ranks
	mux.HandleFunc("/api/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		updateLevelBook(bots, false)
		board := leaderboard(bots)
		limit := 50
		fmt.Sscan(r.URL.Query().Get("limit"), &limit)
		top := board
		if limit > 0 && len(top) > limit {
			top = top[:limit]
		}
		var leaders []rankRow
		for _, row := range board {
			if row.Leader != "" {
				leaders = append(leaders, row)
			}
		}
		writeJSON(w, map[string]interface{}{"ranked": len(board), "top": top, "leaders": leaders,
			"note": "Legion characters by base level then base exp; last known level kept for offline characters"})
	})
	// GET /api/guild-leaders: every leader's status; POST /api/guild-leaders/{guildNN}/party {"map":"", "members":[]}
	mux.HandleFunc("/api/guild-leaders", func(w http.ResponseWriter, r *http.Request) {
		updateLevelBook(bots, false)
		writeJSON(w, leaderStatuses(bots))
	})
	byName := map[string]*Bot{}
	for _, b := range bots {
		for _, c := range b.P.Chars {
			byName[strings.ToLower(c.Name)] = b
		}
	}
	mux.HandleFunc("/api/guild-leaders/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/guild-leaders/"), "/")
		var g *GuildDef
		for _, d := range guildDefs {
			if strings.EqualFold(d.ID, parts[0]) {
				g = d
			}
		}
		l := guildLeader(parts[0])
		if g == nil || l == nil {
			http.Error(w, "no such guild leader", 404)
			return
		}
		if len(parts) == 1 || parts[1] == "" {
			updateLevelBook(bots, false)
			// + the guild members free to party right now (online, no party), for picking the leader's party
			type mate struct {
				Name  string `json:"name"`
				Class string `json:"class"`
				Role  string `json:"role"`
				Level int    `json:"level"`
				Map   string `json:"map"`
			}
			roles := map[int]string{roleTank: "tank", roleRanged: "ranged", roleSupport: "support", roleMelee: "melee", roleNone: "novice"}
			mates := []mate{}
			for _, m := range guildMates(l, g, bots) {
				m.mu.Lock()
				mates = append(mates, mate{m.charName(), className(m.Class), roles[partyRole(m)], m.BaseLvOr(), m.MapName})
				m.mu.Unlock()
			}
			sort.Slice(mates, func(i, j int) bool { return mates[i].Level > mates[j].Level })
			ms := pickLeaderParty(l, guildMates(l, g, bots))
			sug := []string{}
			for _, m := range ms {
				sug = append(sug, m.charName())
			}
			writeJSON(w, map[string]interface{}{"leader": leaderSummary(bots)[g.ID], "free_mates": mates,
				"suggested_party": sug, "suggested_map": bestPartySpot(append([]*Bot{l}, ms...))})
			return
		}
		if parts[1] != "party" || r.Method != http.MethodPost {
			http.Error(w, "GET /api/guild-leaders/{guild} or POST /api/guild-leaders/{guild}/party", 400)
			return
		}
		var body struct {
			Map     string   `json:"map"`
			Members []string `json:"members"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		var ms []*Bot
		for _, n := range body.Members {
			m := byName[strings.ToLower(n)]
			if m == nil {
				http.Error(w, "no such character: "+n, 404)
				return
			}
			if inHumanParty(m) {
				http.Error(w, m.charName()+" is in "+humanOf(m)+"'s party and can't join another one", 409)
				return
			}
			if m != l {
				ms = append(ms, m)
			}
		}
		p, m, err := formLeaderParty(l, g, bots, ms, body.Map)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		var names []string
		for _, b := range p.Members {
			names = append(names, b.charName())
		}
		writeJSON(w, map[string]interface{}{"ok": 1, "party": p.Name, "map": m, "members": names})
	})
}
