package main

// Guilds are not assigned: ten ambitious players (picked at startup) each carry one guild vision. They start at
// level 1 like everyone else, found their guild in-game once strong enough (Base 15+) and recruit the people they
// meet and party with. Players accept or decline by personality and how well the guild suits them. Only real
// members follow a guild's tactics: risk appetite, party preference, mentoring (high levels bait monsters for
// low-level guildmates), rest / hangout / selling habits and map variety. Unaffiliated players play their own way.
//   CZ_REQ_MAKE_GUILD 0x0165 (cid, name[24])   CZ_REQ_JOIN_GUILD 0x0168 (target aid, my aid, my cid)
//   ZC_REQ_JOIN_GUILD 0x016a (guild id, name[24])   CZ_JOIN_GUILD 0x016b (guild id, answer)
//   ZC_UPDATE_GDID    0x016c (guild id, emblem, mode, isMaster, interSID, name[24])

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type GuildTactics struct {
	Risk     float64 `json:"risk"`      // added to the member's own risk when picking maps (-0.3 .. +0.3)
	Party    float64 `json:"party"`     // multiplier on willingness to party (1 = personal sociability)
	Mentor   bool    `json:"mentor"`    // high-level members bait (tag once) so low-level guildmates get the kills
	Rest     float64 `json:"rest"`      // multiplier on the HP% at which members stop to rest (<1 = push on)
	Hangout  float64 `json:"hangout"`   // multiplier on town hangout time
	Sell     float64 `json:"sell"`      // chance to advertise surplus loot when idle in town
	Variety  float64 `json:"variety"`   // chance to pick any suitable map instead of the nearest
	MinParty int     `json:"min_party"` // preferred party size
}

type GuildDef struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Style    string       `json:"style"`
	Strategy string       `json:"strategy"`
	Tactics  GuildTactics `json:"tactics"`
}

var guildDefs = []*GuildDef{
	{"guild01", "Crimson Oath", "fiery, competitive",
		"Win the race: push to the hardest maps the party can survive, tank-led parties of four, minimal resting, first to 2nd job and the first to War of Emperium. Weak members are carried hard so the whole guild hits 99 first.",
		GuildTactics{Risk: 0.25, Party: 1.4, Mentor: true, Rest: 0.7, Hangout: 0.4, Sell: 0.2, Variety: 0.1, MinParty: 4}},
	{"guild02", "Silver Lanterns", "patient mentors",
		"Nobody levels alone: veterans adopt new players, bait monsters for them and keep a priest in every party. Safe maps, steady progress, lots of guidance in chat.",
		GuildTactics{Risk: -0.2, Party: 1.6, Mentor: true, Rest: 1.1, Hangout: 1.0, Sell: 0.3, Variety: 0.1, MinParty: 3}},
	{"guild03", "Moonlit Bazaar", "merchant guild",
		"Zeny first: farm maps with valuable drops, never waste loot, sell surplus in Prontera all evening. Merchants and blacksmiths are the backbone; everyone else feeds them materials.",
		GuildTactics{Risk: -0.05, Party: 0.8, Mentor: false, Rest: 1.0, Hangout: 1.3, Sell: 0.9, Variety: 0.3, MinParty: 2}},
	{"guild04", "Iron Wolves", "disciplined",
		"Textbook parties: one tank, one priest, two damage dealers, assigned by the leader. Dungeon runs on schedule, nobody solos in dungeons, nobody wanders off.",
		GuildTactics{Risk: 0.1, Party: 1.8, Mentor: true, Rest: 0.9, Hangout: 0.5, Sell: 0.2, Variety: 0.05, MinParty: 4}},
	{"guild05", "Starfall Wanderers", "explorers",
		"See everything: rotate between many fields and dungeons, small parties of friends, map knowledge over exp per hour.",
		GuildTactics{Risk: 0.05, Party: 1.0, Mentor: false, Rest: 1.0, Hangout: 0.7, Sell: 0.3, Variety: 0.7, MinParty: 2}},
	{"guild06", "Kitty Cafe", "laid-back, social",
		"Fun first: duo and trio with friends, long chats in town between hunts, help anyone who asks. Levels come on their own.",
		GuildTactics{Risk: -0.15, Party: 1.3, Mentor: true, Rest: 1.2, Hangout: 1.8, Sell: 0.5, Variety: 0.3, MinParty: 2}},
	{"guild07", "Obsidian Edge", "cold min-maxers",
		"Maximum exp per hour: optimal maps by level and element, potions over sitting, no idle time in town, sell loot in bulk and go straight back out.",
		GuildTactics{Risk: 0.2, Party: 1.1, Mentor: false, Rest: 0.6, Hangout: 0.2, Sell: 0.4, Variety: 0.0, MinParty: 3}},
	{"guild08", "Dawnbreakers", "earnest underdogs",
		"Grow together: the strongest members carry the weakest, every level-up is celebrated, recruit newcomers into parties straight away.",
		GuildTactics{Risk: -0.1, Party: 1.5, Mentor: true, Rest: 1.0, Hangout: 0.9, Sell: 0.3, Variety: 0.15, MinParty: 3}},
	{"guild09", "Thorned Rose", "proud roleplayers",
		"Grace and style: matching parties, elegant gear before raw stats, roleplay in town squares, hunt where it's scenic as much as where it's efficient.",
		GuildTactics{Risk: 0.0, Party: 1.2, Mentor: false, Rest: 1.1, Hangout: 1.4, Sell: 0.4, Variety: 0.5, MinParty: 3}},
	{"guild10", "Pudding Patrol", "goofy, chaotic",
		"Do whatever's fun right now: random maps, random parties, train Porings into submission, somehow still level up.",
		GuildTactics{Risk: 0.1, Party: 1.2, Mentor: false, Rest: 0.9, Hangout: 1.0, Sell: 0.4, Variety: 0.8, MinParty: 2}},
}

var neutralTactics = GuildTactics{Risk: 0, Party: 1, Rest: 1, Hangout: 1, Sell: 0.3, Variety: 0.2, MinParty: 2}

var guildByName = func() map[string]*GuildDef {
	m := map[string]*GuildDef{}
	for _, g := range guildDefs {
		m[strings.ToLower(g.Name)] = g
	}
	return m
}()

var guildLeaders = struct {
	sync.Mutex
	byDef map[string]*Bot    // guild id -> leader
	ofBot map[*Bot]*GuildDef // leader -> its vision
}{byDef: map[string]*Bot{}, ofBot: map[*Bot]*GuildDef{}}

// guildRoster: which guild each Legion account's character is in (learned from the server, persisted)
var guildRoster sync.Map // account -> guild name

func ambition(p *Persona) float64 {
	a := p.Skill + 0.6*p.Aggression + 0.5*p.Patience + 0.4*p.Sociability + 0.3*p.Loyalty
	switch p.Archetype {
	case "hardcore", "grinder":
		a += 0.6
	case "socialite", "helper":
		a += 0.3
	}
	return a
}

func pickGuildLeaders(bots []*Bot) {
	// One leader per guild, stable across restarts: data/guild_leaders.json (guild id -> persona account) keeps the
	// picks; the in-game guild master (0x016c, noteGuildMaster) always wins over it. Only guilds without a saved
	// leader get a new pick: the most ambitious free persona, preferring the guild's own members (persona guild) so
	// the leader is in its guild's roster.
	byAcc := map[string]*Bot{}
	for _, b := range bots {
		byAcc[b.P.Account] = b
	}
	saved := map[string]string{}
	if raw, err := os.ReadFile(filepath.Join(*dataDir, "guild_leaders.json")); err == nil {
		json.Unmarshal(raw, &saved)
	}
	used := map[*Bot]bool{}
	chosen := map[string]*Bot{}
	for _, g := range guildDefs {
		if b := byAcc[saved[g.ID]]; b != nil && len(b.P.Chars) > 0 && !seedKeep[b.P.AccountID] && !used[b] {
			chosen[g.ID], used[b] = b, true
		}
	}
	cands := make([]*Bot, 0, len(bots))
	for _, b := range bots {
		if b.pin == nil && len(b.P.Chars) > 0 && !seedKeep[b.P.AccountID] && !used[b] {
			cands = append(cands, b)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return ambition(cands[i].P) > ambition(cands[j].P) })
	for _, g := range guildDefs {
		if chosen[g.ID] != nil {
			continue
		}
		var l *Bot
		for _, c := range cands {
			if !used[c] && c.P.Guild == g.ID {
				l = c
				break
			}
		}
		for _, c := range cands {
			if l == nil && !used[c] {
				l = c
			}
		}
		if l != nil {
			chosen[g.ID], used[l] = l, true
		}
	}
	guildLeaders.Lock()
	for _, g := range guildDefs {
		if l := chosen[g.ID]; l != nil {
			guildLeaders.byDef[g.ID] = l
			guildLeaders.ofBot[l] = g
			leaderTraits(l.P)
		}
	}
	guildLeaders.Unlock()
	for _, g := range guildDefs {
		if l := chosen[g.ID]; l != nil {
			memoryOf(l.P).ensureGoal("found the guild " + g.Name + " and grow it. Vision: " + g.Strategy)
		}
	}
	saveGuildLeaders()
	loadGuildRoster()
}

// leaderTraits: more ambition for a guild leader: grind more, idle less, lead
func leaderTraits(p *Persona) {
	p.AFKChance = 0
	p.Patience = maxf(p.Patience, 0.8)
	p.Sociability = maxf(p.Sociability, 0.8)
	p.Aggression = minf(1, p.Aggression+0.15)
	p.Risk = minf(1, p.Risk+0.1)
}

func saveGuildLeaders() {
	m := map[string]string{}
	guildLeaders.Lock()
	for id, l := range guildLeaders.byDef {
		m[id] = l.P.Account
	}
	guildLeaders.Unlock()
	if raw, err := json.MarshalIndent(m, "", " "); err == nil {
		os.WriteFile(filepath.Join(*dataDir, "guild_leaders.json"), raw, 0644)
	}
}

// noteGuildMaster (0x016c, main instance): the in-game master of one of our guilds is that guild's leader. If the
// engine had another leader, the two swap visions (so every guild still has exactly one leader); persisted.
func noteGuildMaster(b *Bot, guildName string) {
	g := guildByName[strings.ToLower(guildName)]
	if g == nil || coreInstance() {
		return
	}
	guildLeaders.Lock()
	old := guildLeaders.byDef[g.ID]
	if old == b {
		guildLeaders.Unlock()
		return
	}
	prev := guildLeaders.ofBot[b] // the guild b led until now (nil if none)
	delete(guildLeaders.ofBot, b)
	guildLeaders.byDef[g.ID], guildLeaders.ofBot[b] = b, g
	if old != nil {
		delete(guildLeaders.ofBot, old)
		if prev != nil {
			guildLeaders.byDef[prev.ID], guildLeaders.ofBot[old] = old, prev
		}
	} else if prev != nil {
		delete(guildLeaders.byDef, prev.ID) // re-picked at the next start; rare (a leader mastering a foreign guild)
	}
	guildLeaders.Unlock()
	leaderTraits(b.P)
	oldName := "nobody"
	if old != nil {
		oldName = old.P.Name
	}
	events.add(Event{T: time.Now(), Bot: b.charName(), Player: b.P.Name, Kind: "info",
		Text: "guild leader of " + g.ID + " is now the in-game master " + b.charName() + " (was " + oldName + ")"})
	go saveGuildLeaders()
}

// loadGuildRoster reads guild_roster.json (both instances; only the main one writes it).
func loadGuildRoster() {
	if raw, err := os.ReadFile(filepath.Join(*dataDir, "guild_roster.json")); err == nil {
		var m map[string]string
		if json.Unmarshal(raw, &m) == nil {
			for k, v := range m {
				guildRoster.Store(k, v)
			}
		}
	}
}

func saveGuildRoster() {
	m := map[string]string{}
	guildRoster.Range(func(k, v interface{}) bool { m[k.(string)] = v.(string); return true })
	if raw, err := json.Marshal(m); err == nil {
		os.WriteFile(filepath.Join(*dataDir, "guild_roster.json"), raw, 0644)
	}
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// leaderVision: the guild this character is destined to found (nil for everyone else)
func (b *Bot) leaderVision() *GuildDef {
	guildLeaders.Lock()
	defer guildLeaders.Unlock()
	return guildLeaders.ofBot[b]
}

func guildLeader(id string) *Bot {
	guildLeaders.Lock()
	defer guildLeaders.Unlock()
	return guildLeaders.byDef[id]
}

// guild: whose tactics this character follows - its own vision for a leader, the guild it actually joined
// otherwise, nil when unaffiliated
func (b *Bot) guild() *GuildDef {
	if g := b.leaderVision(); g != nil {
		return g
	}
	if b.GuildName != "" {
		return guildByName[strings.ToLower(b.GuildName)]
	}
	return nil
}

func (b *Bot) tactics() GuildTactics {
	if g := b.guild(); g != nil {
		return g.Tactics
	}
	return neutralTactics
}

// guildThink (under b.mu): answer invitations; leaders found their guild and recruit.
func (b *Bot) guildThink(now time.Time) {
	if b.guildInvite != 0 {
		gid, name := b.guildInvite, b.guildInviteName
		b.guildInvite = 0
		ans := uint32(0)
		if acc, ok := b.dutyGuildAnswer(name); ok { // guildduty.go: a kicked bot's cool-down / the guild it chose
			if acc {
				ans = 1
			}
		} else if acc, ok := b.humanGuildAnswer(name); ok { // humanguild.go: Ayla's guild / her party friends
			if acc {
				ans = 1
			}
		} else if b.GuildID == 0 && b.friendMoveWants(name) { // friends.go: following its friends to their guild
			ans = 1
		} else if b.GuildID == 0 && b.leaderVision() == nil && b.rng.Float64() < b.guildFit(name) {
			ans = 1
		} else if g := guildByID(b.P.Guild); b.GuildID == 0 && g != nil && g.Name == name {
			ans = 1 // economy run 8: its own (persona) guild - the guild alchemist / trader always joins
		}
		if ans == 1 {
			b.guildJoinGID, b.guildJoinName = gid, name // confirmed by 0x016c or our 0x0a30 name update (bot.go)
		}
		go b.send(newPkt(0x016b).u32(gid).u32(ans).b)
	}
	b.guildAdminThink(now) // guildtax.go: EXP tax, guild skills, guild info
	v := b.leaderVision()
	if v == nil || now.Before(b.guildActAt) {
		return
	}
	b.guildActAt = now.Add(time.Duration(40+b.rng.Intn(50)) * time.Second)
	if b.GuildID == 0 {
		if b.BaseLvOr() >= 15 {
			go b.send(newPkt(0x0165).u32(b.cid).str(v.Name, 24).b)
			b.event("guild", "", "founding the guild "+v.Name)
		}
		return
	}
	if !b.guildMaster {
		return
	}
	// recruit: party members first, then players on screen who aren't in a guild yet and are near our level
	var pick *Bot
	// economy run 8: the guild's key roles first, wherever they are - its alchemist (brewer) and its trader (guild
	// stock). Both guild alchemists were outside any in-game guild (guild_id 0) while the guild's 16 slots (guild
	// level 1) were filled with whoever stood on the leader's screen.
	for _, name := range []string{guildAlchemistChar2(v.ID), guildTrader(v.ID)} {
		if o := botByCharName(name); name != "" && o != nil && o != b && o.Online && o.GuildID == 0 && o.leaderVision() == nil &&
			now.After(b.buffAt["guildkey:"+name]) {
			b.buffAt["guildkey:"+name] = now.Add(10 * time.Minute) // a full guild refuses: retry every 10 min
			pick = o
			b.event("guild", name, "inviting "+name+" (guild key role) to "+v.Name)
			break
		}
	}
	if pick == nil {
		if f := b.friendRecruit(); f != nil { // friends.go: the leader's own friends first
			pick = f
			b.event("guild", f.charName(), "inviting my friend "+f.charName()+" to "+v.Name)
		}
	}
	if p := partyOf(b); p != nil && pick == nil {
		for _, m := range p.Members {
			if m != b && m.GuildID == 0 && m.Online && m.leaderVision() == nil {
				pick = m
				break
			}
		}
	}
	if pick == nil {
		lv := b.BaseLvOr()
		for _, e := range b.ents {
			if e.Type != objPC {
				continue
			}
			o := botByCharName(e.Name)
			if o == nil || o == b || o.GuildID != 0 || o.leaderVision() != nil || abs(o.BaseLvOr()-lv) > 20 {
				continue
			}
			pick = o
			if b.rng.Float64() < 0.5 {
				break
			}
		}
	}
	if pick != nil {
		go b.send(newPkt(0x0168).u32(pick.aid).u32(b.aid).u32(b.cid).b)
	}
}

// guildFit: chance that this player accepts an invitation, from personality and how well the guild suits them
func (b *Bot) guildFit(name string) float64 {
	g := guildByName[strings.ToLower(name)]
	f := guildFitBase(b)
	if g == nil {
		return f
	}
	switch {
	case b.P.Archetype == "merchant" && g.ID == "guild03",
		(b.P.Archetype == "hardcore" || b.P.Archetype == "grinder") && (g.ID == "guild01" || g.ID == "guild07" || g.ID == "guild04"),
		(b.P.Archetype == "newbie" || b.P.Archetype == "helper") && (g.ID == "guild02" || g.ID == "guild08"),
		b.P.Archetype == "explorer" && g.ID == "guild05",
		(b.P.Archetype == "socialite" || b.P.Archetype == "casual") && (g.ID == "guild06" || g.ID == "guild09" || g.ID == "guild10"):
		f += 0.3
	}
	return minf(0.95, f)
}

var botNames sync.Map // character name -> *Bot

func botByCharName(n string) *Bot {
	if v, ok := botNames.Load(n); ok {
		return v.(*Bot)
	}
	return nil
}

// registerGuilds: GET /api/guilds/detail (dashboard + guild minds).
func registerGuilds(mux *http.ServeMux, bots []*Bot) {
	for n, b := range charIndexBy(bots, func(s string) string { return s }) { // mains win, as in the API (api.go charIndex)
		botNames.Store(n, b)
	}
	if coreInstance() {
		loadGuildRoster() // the core instance (instances.go) picks no guild leaders and doesn't write the roster
	} else {
		pickGuildLeaders(bots)
		go func() {
			for range time.Tick(time.Minute) {
				saveGuildRoster()
			}
		}()
	}
	registerBossHunt(mux)           // bosshunt.go: /api/boss-hunt (guild boss hunts), /api/boss-hunt/bosses
	registerGuildLeaders(mux, bots) // guildlead.go: /api/leaderboard, /api/guild-leaders, leader parties
	registerHumanGuild(mux, bots)   // humanguild.go: /api/guild-recruits (Ayla's guild)
	registerGuildDuty(mux, bots)    // guildduty_api.go: guild directions, duty, rank, relations
	mux.HandleFunc("/api/guilds/detail", func(w http.ResponseWriter, r *http.Request) {
		type member struct {
			Name  string `json:"name"`
			Level int    `json:"level"`
			Class string `json:"class"`
			Map   string `json:"map,omitempty"`
		}
		type leader struct {
			member
			Online  bool   `json:"online"`
			Player  string `json:"player"`
			Persona string `json:"persona"`
			// additive (guildlead.go): leaders are always online; their state, server rank, relogs and party
			AlwaysOnline bool   `json:"always_online"`
			State        string `json:"state,omitempty"`
			Rank         int    `json:"rank,omitempty"`
			RankedOf     int    `json:"ranked_of,omitempty"`
			Relogs       int    `json:"relogs"`
			Party        string `json:"party,omitempty"`
			PartyMap     string `json:"party_map,omitempty"`
			PartySize    int    `json:"party_size,omitempty"`
		}
		type out struct {
			*GuildDef
			Founded   bool           `json:"founded"`
			Leader    leader         `json:"leader"`
			Members   int            `json:"members"`
			Online    int            `json:"online"`
			AvgLevel  float64        `json:"avg_level"`
			Top       []member       `json:"top"`
			Parties   int            `json:"parties"`
			Maps      [][]any        `json:"maps"`
			Kills     int            `json:"kills_session"`
			Deaths    int            `json:"deaths_session"`
			JobCounts map[string]int `json:"jobs"`
		}
		lsum := leaderSummary(bots)
		res := []*out{}
		idx := map[string]*out{}
		for _, g := range guildDefs {
			o := &out{GuildDef: g, JobCounts: map[string]int{}}
			res = append(res, o)
			idx[g.ID] = o
		}
		maps := map[string]map[string]int{}
		lvSum := map[string]int{}
		for _, b := range bots {
			var o *out
			if v, ok := guildRoster.Load(b.P.Account); ok {
				if g := guildByName[strings.ToLower(v.(string))]; g != nil {
					o = idx[g.ID]
				}
			}
			if o == nil {
				continue
			}
			o.Members++
			b.mu.Lock()
			if b.Online {
				o.Online++
				lv := b.BaseLvOr()
				lvSum[o.ID] += lv
				o.Top = append(o.Top, member{b.charName(), lv, className(b.Class), b.MapName})
				o.JobCounts[className(b.Class)]++
				if maps[o.ID] == nil {
					maps[o.ID] = map[string]int{}
				}
				maps[o.ID][b.MapName]++
				o.Kills += b.Kills
				o.Deaths += b.Deaths
			}
			b.mu.Unlock()
		}
		parties.Lock()
		for _, p := range parties.byName {
			if p.Leader != nil {
				if g := p.Leader.guild(); g != nil {
					idx[g.ID].Parties++
				}
			}
		}
		parties.Unlock()
		for _, o := range res {
			if o.Online > 0 {
				o.AvgLevel = float64(lvSum[o.ID]) / float64(o.Online)
			}
			sort.Slice(o.Top, func(i, j int) bool { return o.Top[i].Level > o.Top[j].Level })
			if len(o.Top) > 8 {
				o.Top = o.Top[:8]
			}
			for m, n := range maps[o.ID] {
				o.Maps = append(o.Maps, []any{m, n})
			}
			sort.Slice(o.Maps, func(i, j int) bool { return o.Maps[i][1].(int) > o.Maps[j][1].(int) })
			if len(o.Maps) > 6 {
				o.Maps = o.Maps[:6]
			}
			if l := guildLeader(o.ID); l != nil {
				l.mu.Lock()
				o.Founded = l.GuildID != 0 && l.guildMaster
				o.Leader = leader{member{l.charName(), l.BaseLvOr(), className(l.Class), l.MapName}, l.Online, l.P.Name,
					l.P.Archetype + ", " + strings.TrimSpace(l.P.Typing), true, l.State, 0, 0, 0, "", "", 0}
				l.mu.Unlock()
				if s, ok := lsum[o.ID]; ok {
					o.Leader.Rank, o.Leader.RankedOf, o.Leader.Relogs = s.Rank, s.Ranked, s.Relogs
					o.Leader.Party, o.Leader.PartyMap, o.Leader.PartySize = s.Party, s.PartyMap, len(s.PartyMembers)
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	})
}

// noteGuildJoined (b.mu held): the server confirmed our membership by a name update (bot.go 0x0a30) - the same
// bookkeeping as a 0x016c for a plain member.
func (b *Bot) noteGuildJoined(gid uint32, name string) {
	b.GuildID, b.GuildName, b.guildMaster = gid, name, false
	b.guildJoinGID, b.guildJoinName = 0, ""
	guildRoster.Store(b.P.Account, name)
	b.event("guild", "", "joined the guild "+name)
	b.remember("joined the guild %s", name)
	b.humanGuildJoined(name) // humanguild.go: hello in Ayla's guild chat
}

// guildAlchemistChar2: the guild's alchemist character name ("" none) - alchemy.go guildAlchemistOf by guild id
func guildAlchemistChar2(guild string) string {
	if a := guildAlchemistOf(guild); a != nil {
		return a.Char
	}
	return ""
}
