package main

// Boss hunting (owner rules 2026-11-14, RULES.md 2: boss-only items are worth a guild's fortune, "this makes boss
// hunting a priority for strong party"; "boss hunt is only when the characters are completely built out with high
// level gears"). A guild leader's mind sets a boss hunt for its party (POST /api/boss-hunt, `legionctl boss-hunt`);
// the engine checks readiness, takes the party to the boss map (the rally at the nearest Kafra comes with moveTo),
// farms the map while it looks for the boss, fights it as a party and retreats with wings when the fight turns bad.
//
// What the guides say (iRO Wiki MVP pages, "MVP party" guides) and how it maps to the engine:
//   - party makeup: a tank that holds the boss (Swordman line, Monk, evasion Rogue/Assassin), a priest (Heal, Kyrie
//     Eleison, Blessing, Increase AGI; Lex Aeterna before big hits), damage dealers (archers/mages on the adds with
//     AoE, melee on the boss). bossReady requires a tank and a healer, 4+ members for an MVP, 3+ for a mini-boss.
//   - supplies: lots of HP potions, Fly Wings to dodge, a Butterfly Wing to leave. bossReady checks them.
//   - full build: owner rule - every member a 2nd job with its skill plan done, at a level near the boss, wearing its
//     build's current-band weapon and armor from buildgear.json (upper part of the band). Until then the party farms
//     and upgrades (the economy agent's gear goals).
//   - tactics: everyone targets the boss when it is in sight (tank first by walking speed, the rest assist); mages and
//     archers AoE the adds (aoe.go / weaponskill.go are party-aware); members flee only when nearly dead (Fly Wing on
//     the same map, then they come back); the party retreats when the healer dies, half the party is dead, or two
//     members are under 30% HP while the healer is out of SP.
//   - respawn: rAthena boss_monster spawns carry a respawn delay (noteBossSpawn); when the boss isn't up the party
//     keeps farming the map up to the respawn window + 20 min, then goes back.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// bossSpawns: boss mob id -> map -> respawn delay (ms), from npc/re/mobs boss_monster lines (data.go loadNPCs).
var bossSpawns = map[int]map[string]int{}

func noteBossSpawn(id int, m string, delayMs int) {
	if bossSpawns[id] == nil {
		bossSpawns[id] = map[string]int{}
	}
	if old, ok := bossSpawns[id][m]; !ok || delayMs < old {
		bossSpawns[id][m] = delayMs
	}
}

// bossTactics: per-boss notes from the guides (shown to the minds; the engine plays every boss the same way).
var bossTactics = map[string]string{
	"Eddga":            "fire tiger, cast-immune to Provoke tricks; water weapons/arrows, fire resist armor; tank holds, priest Kyrie the tank",
	"Orc Hero":         "earth, summons High Orcs: archers/mages clear the adds, melee on the boss; wind (Mandragora card) weapons",
	"Mistress":         "wind flyer, Jupitel Thunder/Storm Gust: earth weapons, priest heals; kill her Hornets with AoE",
	"Phreeoni":         "neutral brute, huge HP but weak hits: a patient party with Goblin/race cards; no ghost weapons needed",
	"Golden Thief Bug": "fire insect, reflects magic: physical damage only (Caramel card), mages stay on the Thief Bugs",
	"Osiris":           "undead, summons Ancient Mummies: holy damage, priest Heal/Turn Undead and Aspersio, Sanctuary on the tank",
	"Doppelganger":     "shadow demon, fast melee: Holy/Strouf card damage, two healers if possible, never fight it under-levelled",
	"Moonlight Flower": "fire demon, summons Nine Tails: water damage, tank holds while AoE clears the foxes",
	"Maya":             "earth insect queen, reflects magic: physical party, fire weapons, kill the summoned ants with Arrow Shower",
	"Drake":            "undead pirate, Water Ball: holy damage; Sunken Ship needs a boat",
	"Vocal":            "mini-boss cricket on prt_fild07: any party of lv30+",
	"Eclipse":          "mini-boss rabbit on prt_fild04: any party of lv30+",
	"Vagabond Wolf":    "mini-boss wolf in pay_fild areas, summons wolves: AoE the pack",
	"Dragon Fly":       "mini-boss fly, fast: ranged party members",
	"Mastering":        "mini-boss poring king: easy",
	"Toad":             "mini-boss frog: easy",
	"Deviling":         "dark angel-poring lv66, hits hard: holy damage",
	"Angeling":         "holy angel-poring lv77: shadow weapons, tank + priest",
	"Ghostring":        "ghost poring lv90: ghost/holy damage only (neutral misses), strong party",
}

type bossInfo struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Level    int    `json:"level"`
	MVP      bool   `json:"mvp"`
	Map      string `json:"map"`
	Respawn  int    `json:"respawn_min"`
	MinLv    int    `json:"party_min_level"`
	Members  int    `json:"party_min_members"`
	Tactics  string `json:"tactics,omitempty"`
	Reach    bool   `json:"reachable"`
	MapNotes string `json:"map_note,omitempty"`
}

// bossReq: the levels/size a party needs (guides: MVPs with a full party a few levels under the boss; mini-bosses
// with a small party at its level)
func bossReq(lv int, mvp bool) (minLv, members int) {
	if mvp {
		return max(lv-10, 50), 4
	}
	return max(lv-5, 10), 3
}

// bossCatalog: every Boss-class monster with a reachable timed spawn (MVPs and mini-bosses), easiest first.
func bossCatalog() []bossInfo {
	lookupOnce.Do(loadLookup) // mobExtras (MVP flag)
	var out []bossInfo
	for id, maps := range bossSpawns {
		mb := mobs[id]
		if mb == nil || mb.Level <= 0 || mb.Champion {
			continue
		}
		ex := mobExtras[id]
		mvp := ex != nil && ex.Mvp
		best, bestD, reach := "", 0, false
		for m, d := range maps {
			r := mapHops("prontera", 156, 191, m) < 99 // from Prontera's centre (a real cell: RouteMaps needs one)
			if best == "" || (r && !reach) || (r == reach && d < bestD) {
				best, bestD, reach = m, d, r
			}
		}
		minLv, n := bossReq(mb.Level, mvp)
		out = append(out, bossInfo{ID: id, Name: mb.Name, Level: mb.Level, MVP: mvp, Map: best, Respawn: bestD / 60000,
			MinLv: minLv, Members: n, Tactics: bossTactics[mb.Name], Reach: reach})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return out[i].Level < out[j].Level
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func findBoss(q string) *bossInfo {
	q = strings.ToLower(strings.TrimSpace(q))
	id, _ := strconv.Atoi(q)
	var part *bossInfo
	for _, b := range bossCatalog() {
		b := b
		if b.ID == id || strings.ToLower(b.Name) == q {
			return &b
		}
		if part == nil && q != "" && strings.Contains(strings.ToLower(b.Name), q) {
			part = &b
		}
	}
	return part
}

// ---------------------------------------------------------------- hunts

type bossHunt struct {
	Guild    string    `json:"guild"`
	Leader   string    `json:"leader"`
	Boss     bossInfo  `json:"boss"`
	Phase    string    `json:"phase"` // travel, search, fight, done, retreat, cancelled, timeout
	Started  time.Time `json:"started"`
	Arrived  time.Time `json:"arrived,omitempty"`
	SeenAt   time.Time `json:"boss_seen_at,omitempty"`
	Ended    time.Time `json:"ended,omitempty"`
	Kills    int       `json:"kills"`
	Reason   string    `json:"reason,omitempty"`
	PrevMap  string    `json:"prev_map"`
	party    *Party
	lastSaid time.Time
}

var bossHunts = struct {
	sync.Mutex
	active   map[*Party]*bossHunt
	history  []*bossHunt
	cooldown map[string]time.Time // guild -> no new hunt before
}{active: map[*Party]*bossHunt{}, cooldown: map[string]time.Time{}}

// bossHuntsLive: number of entries in bossHunts.active (cheap check for the per-death hook in bot.go)
var bossHuntsLive atomic.Int32

func (h *bossHunt) live() bool {
	return h.Phase == "travel" || h.Phase == "search" || h.Phase == "fight"
}

func huntOf(p *Party) *bossHunt {
	if p == nil {
		return nil
	}
	bossHunts.Lock()
	defer bossHunts.Unlock()
	if h := bossHunts.active[p]; h != nil && h.live() {
		return h
	}
	return nil
}

// bossActive: the party is on a boss hunt (pickers must not move it, the refresh must not disband it).
func (p *Party) bossActive() bool { return huntOf(p) != nil }

// bossHuntTarget (b.mu may be held): the boss mob id this bot hunts right now (on the boss map), 0 = none.
func (b *Bot) bossHuntTarget() int {
	h := huntOf(partyOf(b))
	if h == nil || h.Phase == "travel" || b.MapName != h.Boss.Map {
		return 0
	}
	return h.Boss.ID
}

// bossKilled (from the 0x0080 handler, b.mu held): a boss died in sight of b.
func bossKilled(b *Bot, mob int) {
	p := partyOf(b)
	bossHunts.Lock()
	h := bossHunts.active[p]
	if h == nil || !h.live() || h.Boss.ID != mob || b.MapName != h.Boss.Map {
		bossHunts.Unlock()
		return
	}
	h.Kills++
	h.Phase, h.Reason, h.Ended = "done", "killed "+h.Boss.Name, time.Now()
	bossHunts.Unlock()
	go b.event("guild", "", fmt.Sprintf("BOSS DOWN: %s's party killed %s on %s", h.Leader, h.Boss.Name, h.Boss.Map))
	go b.remember("we killed %s on %s", h.Boss.Name, h.Boss.Map)
}

// ---------------------------------------------------------------- readiness

type bossMember struct {
	Name   string   `json:"name"`
	Class  string   `json:"class"`
	Level  int      `json:"level"`
	Role   string   `json:"role"`
	Issues []string `json:"issues,omitempty"`
}

type bossCheck struct {
	Ready   bool         `json:"ready"`
	Boss    bossInfo     `json:"boss"`
	Party   []bossMember `json:"party"`
	Missing []string     `json:"missing,omitempty"` // party-level problems
}

// topGear (b.mu held): the worn weapon and armor are on the build's buildgear table for the current level band and in
// its upper part (score at least 70% of the band's best for that slot). "" = ok, else what is missing.
func (b *Bot) topGear() string {
	p := b.plan()
	if p == nil {
		return "no build plan"
	}
	band := gearBandFor(p.ID, b.BaseLvOr())
	if band == nil {
		return "no gear table for the build"
	}
	var miss []string
	for _, s := range []struct {
		slot string
		loc  uint32
	}{{"weapon", 0x2}, {"armor", 0x10}} {
		list := band.Slots[s.slot]
		if len(list) == 0 {
			continue
		}
		best := 0
		for _, e := range list {
			best = max(best, e.Score)
		}
		worn := b.equipped[s.loc]
		ok := false
		if worn != nil {
			for _, e := range list {
				if e.ID == worn.ItemID && e.Score*10 >= best*7 {
					ok = true
				}
			}
		}
		if !ok {
			name := "nothing"
			if worn != nil {
				name = itemName(worn.ItemID)
			}
			miss = append(miss, fmt.Sprintf("%s %s is not top gear for lv%d-%d", s.slot, name, band.Min, band.Max))
		}
	}
	return strings.Join(miss, "; ")
}

func roleName(r int) string {
	switch r {
	case roleTank:
		return "tank"
	case roleSupport:
		return "support"
	case roleRanged:
		return "ranged"
	case roleMelee:
		return "melee"
	}
	return "none"
}

// bossReady: can party p take boss bi now? (locks each member's b.mu in turn; no other lock held)
func bossReady(p *Party, bi bossInfo) bossCheck {
	c := bossCheck{Boss: bi}
	parties.Lock()
	ms := append([]*Bot(nil), p.Members...)
	parties.Unlock()
	tank, healer, online := false, false, 0
	for _, m := range ms {
		s, ok := peek(m.P.Name)
		m.mu.Lock()
		bm := bossMember{Name: m.charName(), Class: className(m.Class), Level: m.BaseLvOr(), Role: roleName(partyRole(m))}
		if !ok || !s.Online || m.BaseLv <= 0 {
			bm.Issues = append(bm.Issues, "offline")
		} else {
			online++
		}
		if m.BaseLv < bi.MinLv {
			bm.Issues = append(bm.Issues, fmt.Sprintf("level %d < %d", m.BaseLv, bi.MinLv))
		}
		if family(m.Class) == m.Class || m.Class == 0 { // 1st job / novice: not a full build
			bm.Issues = append(bm.Issues, "still a 1st job")
		}
		if m.JobLv < 50 || m.SkillPts > 0 { // owner: "completely built out"
			bm.Issues = append(bm.Issues, fmt.Sprintf("skill build not finished (job %d/50)", m.JobLv))
		}
		if g := m.topGear(); g != "" {
			bm.Issues = append(bm.Issues, g)
		}
		pots := 15
		if !bi.MVP {
			pots = 8
		}
		if m.isHealer() {
			pots = 3 // healers heal themselves (RULES.md 2)
		}
		if n := m.potionCount(); n < pots {
			bm.Issues = append(bm.Issues, fmt.Sprintf("%d HP potions < %d", n, pots))
		}
		if m.countItem(itemButterflyWing)+m.countItem(itemFlyWing) == 0 {
			bm.Issues = append(bm.Issues, "no Fly/Butterfly Wing to escape")
		}
		if m.MaxWeight > 0 && m.Weight*100/m.MaxWeight >= 50 {
			bm.Issues = append(bm.Issues, "over 50% weight")
		}
		switch r := partyRole(m); {
		case r == roleTank:
			tank = true
		case m.isHealer():
			if si := m.skills[alHeal]; si != nil && si.Lv >= 5 {
				healer = true
			}
			if si := m.skills[73]; bi.MVP && (si == nil || si.Lv < 5) {
				bm.Issues = append(bm.Issues, "no Kyrie Eleison 5 for an MVP")
			}
		}
		m.mu.Unlock()
		c.Party = append(c.Party, bm)
	}
	if online < bi.Members {
		c.Missing = append(c.Missing, fmt.Sprintf("%d members online, %s needs %d", online, bi.Name, bi.Members))
	}
	if !tank {
		c.Missing = append(c.Missing, "no tank (Swordman line, Monk or evasion Thief)")
	}
	if !healer {
		c.Missing = append(c.Missing, "no healer with Heal 5+")
	}
	if !bi.Reach {
		c.Missing = append(c.Missing, bi.Map+" is not reachable on foot")
	}
	c.Ready = len(c.Missing) == 0
	for _, m := range c.Party {
		if len(m.Issues) > 0 {
			c.Ready = false
		}
	}
	return c
}

// ---------------------------------------------------------------- start / stop / manager

func startBossHunt(guild, boss string, force bool) (*bossHunt, *bossCheck, error) {
	l := guildLeader(guild)
	if l == nil {
		return nil, nil, fmt.Errorf("no leader for %s", guild)
	}
	p := partyOf(l)
	if p == nil || p.Leader != l || p.Human != "" {
		return nil, nil, fmt.Errorf("%s isn't leading a party (form one with leader-party first)", l.charName())
	}
	bi := findBoss(boss)
	if bi == nil {
		return nil, nil, fmt.Errorf("unknown boss %q (see GET /api/boss-hunt/bosses)", boss)
	}
	bossHunts.Lock()
	cd := bossHunts.cooldown[guild]
	busy := bossHunts.active[p] != nil && bossHunts.active[p].live()
	bossHunts.Unlock()
	if busy {
		return nil, nil, fmt.Errorf("%s's party is already on a boss hunt", l.charName())
	}
	if time.Now().Before(cd) {
		return nil, nil, fmt.Errorf("%s retreated from a boss recently: next hunt after %s", guild, cd.Format("15:04"))
	}
	c := bossReady(p, *bi)
	if !c.Ready && !force {
		return nil, &c, fmt.Errorf("party not ready for %s", bi.Name)
	}
	parties.Lock()
	prev := p.Map
	parties.Unlock()
	h := &bossHunt{Guild: guild, Leader: l.charName(), Boss: *bi, Phase: "travel", Started: time.Now(), PrevMap: prev, party: p}
	bossHunts.Lock()
	bossHunts.active[p] = h
	bossHuntsLive.Store(int32(len(bossHunts.active)))
	bossHunts.Unlock()
	l.mu.Lock()
	l.Goal, l.route, l.path = Goal{Mode: "hunt", Map: bi.Map, Set: "party-leader"}, nil, nil
	typing := l.P.Typing
	l.mu.Unlock()
	l.event("guild", "", fmt.Sprintf("boss hunt: %s's party goes for %s (lv%d) on %s", l.charName(), bi.Name, bi.Level, bi.Map))
	go l.partySay(styled(typing, pick("ok team, we're going for "+bi.Name+" on "+mapTalk(bi.Map)+". stock up, stay together",
		bi.Name+" hunt! meet at the kafra, tank in front, heal the tank")))
	p.moveTo(bi.Map)
	return h, &c, nil
}

// endBossHunt: the hunt is over; the party goes back to farming where it was.
func endBossHunt(h *bossHunt, phase, why string) {
	bossHunts.Lock()
	if !h.live() && phase != "done" {
		bossHunts.Unlock()
		return
	}
	if h.Ended.IsZero() || h.live() {
		h.Ended = time.Now()
	}
	h.Phase, h.Reason = phase, why
	delete(bossHunts.active, h.party)
	bossHuntsLive.Store(int32(len(bossHunts.active)))
	bossHunts.history = append(bossHunts.history, h)
	if len(bossHunts.history) > 50 {
		bossHunts.history = bossHunts.history[len(bossHunts.history)-50:]
	}
	if phase == "retreat" {
		bossHunts.cooldown[h.Guild] = time.Now().Add(30 * time.Minute)
	}
	bossHunts.Unlock()
	p := h.party
	l := p.Leader
	back := h.PrevMap
	if back == "" || back == h.Boss.Map || isTown(back) {
		parties.Lock()
		ms := append([]*Bot(nil), p.Members...)
		parties.Unlock()
		back = bestPartySpot(ms)
	}
	if l != nil {
		l.event("guild", "", fmt.Sprintf("boss hunt %s (%s): %s", phase, h.Boss.Name, why))
		if back != "" {
			l.mu.Lock()
			l.Goal, l.route, l.path = Goal{Mode: "hunt", Map: back, Set: "party-leader"}, nil, nil
			typing := l.P.Typing
			l.mu.Unlock()
			p.moveTo(back)
			sayPartyDest(l, typing, p, back, "back to farming") // partydest.go PT30: say where to be
		}
	}
}

// retreat: every member on the boss map leaves with a Butterfly Wing (Fly Wing if none), then the hunt ends.
func (h *bossHunt) retreat(why string) {
	parties.Lock()
	ms := append([]*Bot(nil), h.party.Members...)
	parties.Unlock()
	now := time.Now()
	for _, m := range ms {
		m.mu.Lock()
		if !m.Dead && m.MapName == h.Boss.Map {
			switch {
			case m.escItemOK(itemButterflyWing, now):
				m.useEscapeItem(itemButterflyWing, now, "boss hunt retreat: "+why)
			case m.escItemOK(itemFlyWing, now):
				m.useEscapeItem(itemFlyWing, now, "boss hunt retreat: "+why)
			}
			m.target, m.path = 0, nil
		}
		m.mu.Unlock()
	}
	if l := h.party.Leader; l != nil {
		typing := l.P.Typing
		go l.partySay(styled(typing, pick("too much, everyone out! wing out now", "retreat! we'll come back stronger")))
	}
	endBossHunt(h, "retreat", why)
}

// bossTick: one look at a live hunt (no lock held).
func (h *bossHunt) tick(now time.Time) {
	parties.Lock()
	ms := append([]*Bot(nil), h.party.Members...)
	alive := parties.byName[h.party.Name] == h.party || h.party.Leader != nil
	parties.Unlock()
	if !alive || len(ms) < 2 {
		endBossHunt(h, "cancelled", "the party broke up")
		return
	}
	onMap, dead, low, n := 0, 0, 0, len(ms)
	healerDead, healerDry, seen := false, true, false
	for _, m := range ms {
		m.mu.Lock()
		if m.MapName == h.Boss.Map {
			onMap++
			if m.Dead {
				dead++
			} else if m.hpFrac() < 0.3 {
				low++
			}
			for _, e := range m.ents {
				if e.Type == objMob && e.Class == h.Boss.ID {
					seen = true
				}
			}
		}
		if m.isHealer() {
			if m.Dead && m.MapName == h.Boss.Map {
				healerDead = true
			}
			if m.MaxSP > 0 && m.SP*100/m.MaxSP >= 20 {
				healerDry = false
			}
		}
		m.mu.Unlock()
	}
	bossHunts.Lock()
	phase := h.Phase
	if phase == "travel" && h.party.Leader != nil {
		if s, ok := peek(h.party.Leader.P.Name); ok && s.Map == h.Boss.Map {
			h.Phase, h.Arrived, phase = "search", now, "search"
		}
	}
	if seen && phase != "fight" {
		h.Phase, h.SeenAt, phase = "fight", now, "fight"
	}
	if seen {
		h.SeenAt = now
	}
	started, arrived, seenAt := h.Started, h.Arrived, h.SeenAt
	bossHunts.Unlock()
	switch {
	case phase == "travel" && now.Sub(started) > 25*time.Minute:
		endBossHunt(h, "timeout", "could not reach "+h.Boss.Map)
	case phase != "travel" && (healerDead && onMap > 0):
		h.retreat("the healer died")
	case phase != "travel" && dead*2 >= n:
		h.retreat(fmt.Sprintf("%d of %d members dead", dead, n))
	case phase == "fight" && low >= 2 && healerDry:
		h.retreat("two members under 30% HP and the healer is out of SP")
	case phase == "fight" && !seenAt.IsZero() && now.Sub(seenAt) > 4*time.Minute:
		h.Phase = "search" // lost it (it wandered off / someone else killed it): keep looking
	case phase == "search" && now.Sub(arrived) > time.Duration(h.Boss.Respawn+20)*time.Minute:
		endBossHunt(h, "timeout", h.Boss.Name+" did not show up")
	}
}

func bossHuntManager(stop <-chan struct{}) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			bossHunts.Lock()
			var hs []*bossHunt
			var done []*bossHunt
			for _, h := range bossHunts.active {
				if h.live() {
					hs = append(hs, h)
				} else if h.Phase == "done" && now.Sub(h.Ended) > 30*time.Second {
					done = append(done, h) // 30 s to loot, then back to farming
				}
			}
			bossHunts.Unlock()
			for _, h := range hs {
				h.tick(now)
			}
			for _, h := range done {
				endBossHunt(h, "done", h.Reason)
			}
		}
	}
}

// ---------------------------------------------------------------- API

func registerBossHunt(mux *http.ServeMux) {
	if coreInstance() {
		return
	}
	go bossHuntManager(make(chan struct{}))
	mux.HandleFunc("/api/boss-hunt/bosses", func(w http.ResponseWriter, r *http.Request) {
		cat := bossCatalog()
		g := r.URL.Query().Get("guild")
		if g == "" {
			writeJSON(w, cat)
			return
		}
		l := guildLeader(g)
		var p *Party
		if l != nil {
			p = partyOf(l)
		}
		var rows []map[string]interface{}
		for _, bi := range cat {
			if !bi.Reach {
				continue
			}
			row := map[string]interface{}{"boss": bi}
			if p != nil {
				c := bossReady(p, bi)
				row["ready"], row["missing"] = c.Ready, c.Missing
				n := 0
				for _, m := range c.Party {
					n += len(m.Issues)
				}
				row["member_issues"] = n
			}
			rows = append(rows, row)
		}
		writeJSON(w, rows)
	})
	mux.HandleFunc("/api/boss-hunt", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var req struct {
				Guild string `json:"guild"`
				Boss  string `json:"boss"`
				Check bool   `json:"check"` // readiness only
				Force bool   `json:"force"` // owner override: go even if not ready
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			if req.Check {
				l := guildLeader(req.Guild)
				bi := findBoss(req.Boss)
				if l == nil || bi == nil || partyOf(l) == nil {
					http.Error(w, "need a guild whose leader leads a party, and a known boss", 400)
					return
				}
				writeJSON(w, bossReady(partyOf(l), *bi))
				return
			}
			h, c, err := startBossHunt(req.Guild, req.Boss, req.Force)
			if err != nil {
				writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error(), "check": c})
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true, "hunt": h, "check": c})
		case http.MethodDelete:
			g := r.URL.Query().Get("guild")
			bossHunts.Lock()
			var h *bossHunt
			for _, x := range bossHunts.active {
				if x.Guild == g && x.live() {
					h = x
				}
			}
			bossHunts.Unlock()
			if h == nil {
				http.Error(w, "no live hunt for "+g, 404)
				return
			}
			endBossHunt(h, "cancelled", "called off by the guild leader")
			writeJSON(w, map[string]interface{}{"ok": true})
		default:
			bossHunts.Lock()
			var act []*bossHunt
			for _, h := range bossHunts.active {
				act = append(act, h)
			}
			hist := append([]*bossHunt(nil), bossHunts.history...)
			bossHunts.Unlock()
			writeJSON(w, map[string]interface{}{"active": act, "history": hist})
		}
	})
}

// huntOfLocked: huntOf for callers that hold parties.Mutex (huntOf takes only bossHunts' lock).
func huntOfLocked(p *Party) *bossHunt { return huntOf(p) }

// bossEscape (b.mu held, from escape): on a boss hunt a member doesn't run from the fight on "outmatched" - the boss
// is far above everyone by design. It keeps fighting (survive drinks potions) and only above 25% HP; under it a Fly
// Wing takes it off on the same map and the party regroup brings it back. handled=false: escape's normal rules.
func (b *Bot) bossEscape(hp float64, now time.Time) (handled, fled bool) {
	if b.bossHuntTarget() == 0 {
		return false, false
	}
	if hp >= 0.25 {
		return true, false
	}
	if b.escItemOK(itemFlyWing, now) {
		b.escapeAt = now.Add(4 * time.Second)
		b.target, b.path = 0, nil
		b.buffAt["flywing"] = now
		b.useEscapeItem(itemFlyWing, now, "boss hunt: low HP")
		b.setState("teleported away from the boss (low HP)")
		return true, true
	}
	return false, false
}
