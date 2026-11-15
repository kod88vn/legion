package main

// Friendship system (owner rule 2026-11-14, RULES.md section 3 "Friendship system": "need a friendship system. bots
// that spend time together grow in friendship and more likely to continue to party together or be in the same guild.
// Bots can change guild because of their friends. This is a separate system we need to track.")
//
// Its own module with its own data: data/friends.json (main instance) / data/friends_core.json (core instance), each
// rotated to .bak1..3 every hour. Each instance scores the pairs its own bots are part of and reads the other
// instance's file as "peer" pairs (merged by max), so the main API shows both.
//
//   Score per pair of characters, 0-100 (raw score + the minds' manual memory relations as a bonus):
//     partied together on the same map   +0.30 a minute       both killing in that party   +0.10 a minute
//     heal from the other                +0.30 (1/min)        party/guild chat or whisper  +0.20 / +0.05 (1 per 2 min)
//     trade / zeny gift between them     +1.5 each side       mail with zeny               +4
//     same guild, same map, not partied  +0.05 a minute
//   Gains shrink as the score grows (pts * (1 - score/110)). Without contact for 6 h the score fades with a 7-day
//   half-life; pairs under 1 point and silent for 3 days are dropped.
//   Memory relations (legionctl remember --person X --relation ...): friend +10, close/best friend +20, rival -15,
//   enemy -30 (each side's note counts half).
//   Tiers: acquaintance >= 10, friend >= 35, close friend >= 65.
//
//   Effects:
//     - FriendScore(a, b) / bestFriendsOnline(b, n) / friendPartyBonus(c, ms) / friendsFirst(leader, cands): for the
//       goal parties (lfg.go, owned by another engineer) to prefer friends when goals are compatible.
//     - Legion guild leaders invite their unguilded friends first (guilds.go guildThink).
//     - Friends greet each other on login (party chat / guild chat / whisper) and the friend answers; friends in one
//       party chat now and then (banter).
//     - Guild change (main instance only): a bot whose close friends mostly sit in another Legion guild may, with a
//       low chance per day (~12% on a day it qualifies, at most 3 moves a day server-wide, once a week per bot), say
//       goodbye in guild chat, leave (CZ_REQ_LEAVE_GUILD 0x0159) and get invited by that guild's master bot
//       (0x0168); it accepts (guilds.go friendMoveWants) and says hello. Never guild masters / guild leaders, never
//       Ayla's party or Starveil members, never a guild's alchemist or trader.
//     - Memory diary: "X and I have become good friends" when a pair reaches friend / close friend.
//   Seeded lightly the first time (no file): current party mates 18, Ayla's party 30, guild mates 4.
//
// GET /api/friends[?n=30][&bot=Name]: tier counts, top pairs, one character's friends, guild changes, counters.

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	friendTierAcq      = 10.0
	friendTierFriend   = 35.0
	friendTierClose    = 65.0
	friendHalfLife     = 7 * 24 * time.Hour
	friendDecayAfter   = 6 * time.Hour
	friendMoveDayP     = 0.12 // chance per qualifying day that a bot follows its close friends to their guild
	friendMovesPerDay  = 3    // server-wide cap
	friendMoveCooldown = 7 * 24 * time.Hour
)

// friend event kinds and their points
const (
	fkParty     = iota // a minute partied together on the same map
	fkFight            // a minute both killing in that party
	fkHeal             // a heal from the other
	fkChat             // party chat / whisper heard
	fkGuildChat        // guild chat heard
	fkTrade            // a trade window completed between them (each side counts once)
	fkMail             // mail with zeny
	fkGuild            // a minute in the same guild on the same map, not partied
)

var friendPoints = [...]float64{fkParty: 0.30, fkFight: 0.10, fkHeal: 0.30, fkChat: 0.20, fkGuildChat: 0.05,
	fkTrade: 1.5, fkMail: 4, fkGuild: 0.05}

type friendPair struct {
	A        string    `json:"a"`
	B        string    `json:"b"`
	Score    float64   `json:"score"` // raw (without the memory bonus)
	PartyMin float64   `json:"partied_min"`
	Fights   int       `json:"fight_min"`
	Heals    int       `json:"heals"`
	Chats    int       `json:"chats"`
	Trades   int       `json:"trades"`
	GuildMin float64   `json:"guild_min"`
	First    time.Time `json:"first"`
	Last     time.Time `json:"last"`
	Seeded   bool      `json:"seeded,omitempty"`
	decayAt  time.Time
	rate     map[int]time.Time // per kind: last counted (rate limits)
}

type friendMove struct {
	Bot      string    `json:"bot"`
	Account  string    `json:"account"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	Friends  []string  `json:"friends"`
	Stage    string    `json:"stage"` // leaving, invite, done, failed
	Why      string    `json:"why,omitempty"`
	Started  time.Time `json:"started"`
	At       time.Time `json:"at"` // last stage change
	Invites  int       `json:"invites"`
	inviteAt time.Time
}

var friends = struct {
	sync.Mutex
	pairs    map[string]*friendPair
	peer     map[string]*friendPair // the other instance's file
	bonus    map[string]float64     // "a|b" -> memory relation bonus a feels for b
	moves    []*friendMove
	counters map[string]int
	seeded   bool
	loadedN  int
	dirty    bool
	backupAt time.Time
	promos   []friendPromo // tier crossings waiting for diary entries
}{pairs: map[string]*friendPair{}, peer: map[string]*friendPair{}, bonus: map[string]float64{}, counters: map[string]int{}}

type friendPromo struct{ a, b, tier string }

func friendKey(a, b string) (string, string, string) {
	if b < a {
		a, b = b, a
	}
	return a + "|" + b, a, b
}

func friendTier(s float64) string {
	switch {
	case s >= friendTierClose:
		return "close friend"
	case s >= friendTierFriend:
		return "friend"
	case s >= friendTierAcq:
		return "acquaintance"
	}
	return ""
}

func friendFile() string {
	if coreInstance() {
		return filepath.Join(*dataDir, "friends_core.json")
	}
	return filepath.Join(*dataDir, "friends.json")
}

func friendPeerFile() string {
	if coreInstance() {
		return filepath.Join(*dataDir, "friends.json")
	}
	return filepath.Join(*dataDir, "friends_core.json")
}

// friendAddLocked (friends.Mutex held): points for one pair.
func friendAddLocked(a, b string, kind int, mult float64, now time.Time) {
	if a == "" || b == "" || strings.EqualFold(a, b) || !memoryNameOK(a) || !memoryNameOK(b) {
		return
	}
	k, a, b := friendKey(a, b)
	p := friends.pairs[k]
	if p == nil {
		p = &friendPair{A: a, B: b, First: now}
		friends.pairs[k] = p
	}
	before := friendTier(p.Score)
	pts := friendPoints[kind] * mult
	p.Score = math.Min(100, p.Score+pts*(1-p.Score/110))
	p.Last, p.decayAt = now, now
	switch kind {
	case fkParty:
		p.PartyMin += mult
	case fkFight:
		p.Fights++
	case fkHeal:
		p.Heals++
	case fkChat, fkGuildChat:
		p.Chats++
	case fkTrade, fkMail:
		p.Trades++
	case fkGuild:
		p.GuildMin += mult
	}
	if after := friendTier(p.Score); after != before && (after == "friend" || after == "close friend") {
		friends.promos = append(friends.promos, friendPromo{a, b, after})
		friends.counters["new "+after]++
	}
	friends.dirty = true
}

// friendNote: one event between two characters (rate-limited per pair and kind for chat/heal).
func friendNote(a, b string, kind int) {
	if a == "" || b == "" || strings.EqualFold(a, b) {
		return
	}
	now := time.Now()
	defer loyaltyFromEvent(a, b, kind) // loyalty.go (runs after the unlock below: never under friends.Mutex)
	friends.Lock()
	defer friends.Unlock()
	if gap := friendRateGap(kind); gap > 0 {
		k, _, _ := friendKey(a, b)
		p := friends.pairs[k]
		if p != nil && p.rate != nil && now.Sub(p.rate[kind]) < gap {
			return
		}
		friendAddLocked(a, b, kind, 1, now)
		if p = friends.pairs[k]; p != nil {
			if p.rate == nil {
				p.rate = map[int]time.Time{}
			}
			p.rate[kind] = now
		}
		return
	}
	friendAddLocked(a, b, kind, 1, now)
}

func friendRateGap(kind int) time.Duration {
	switch kind {
	case fkHeal:
		return time.Minute
	case fkChat, fkGuildChat:
		return 2 * time.Minute
	}
	return 0
}

// friendHeard (b.mu held): b heard "Name : text" on party/guild chat.
func friendHeard(b *Bot, line string, guild bool) {
	i := strings.Index(line, " : ")
	if i <= 0 {
		return
	}
	who, me := line[:i], b.charName()
	if who == me {
		return
	}
	k := fkChat
	if guild {
		k = fkGuildChat
	}
	go friendNote(me, who, k)
}

// friendBaseLocked: the raw score (own or the peer instance's, whichever is higher).
func friendBaseLocked(k string) float64 {
	s := 0.0
	if p := friends.pairs[k]; p != nil {
		s = p.Score
	}
	if p := friends.peer[k]; p != nil && p.Score > s {
		s = p.Score
	}
	return s
}

func friendEffLocked(a, b string) float64 {
	k, _, _ := friendKey(a, b)
	s := friendBaseLocked(k) + (friends.bonus[a+"|"+b]+friends.bonus[b+"|"+a])/2
	return math.Max(0, math.Min(100, s))
}

// FriendScore: how close two characters are, 0-100 (0 = strangers). Safe to call with any b.mu held.
func FriendScore(a, b string) float64 {
	if a == "" || b == "" || a == b {
		return 0
	}
	friends.Lock()
	defer friends.Unlock()
	return friendEffLocked(a, b)
}

// friendsOf: everyone a character knows with an effective score >= min, best first (name -> score).
func friendsOf(name string, min float64) []nameScore {
	friends.Lock()
	defer friends.Unlock()
	seen := map[string]bool{}
	var out []nameScore
	for _, src := range []map[string]*friendPair{friends.pairs, friends.peer} {
		for _, p := range src {
			o := ""
			switch name {
			case p.A:
				o = p.B
			case p.B:
				o = p.A
			default:
				continue
			}
			if seen[o] {
				continue
			}
			seen[o] = true
			if s := friendEffLocked(name, o); s >= min {
				out = append(out, nameScore{o, s})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

type nameScore struct {
	Name  string  `json:"name"`
	Score float64 `json:"score"`
}

// bestFriendsOnline: up to n online bots of this instance that b knows (score >= acquaintance), best first.
// Reads other bots' Online without their locks (a bool snapshot, as guildThink does). Safe with b.mu held.
func bestFriendsOnline(b *Bot, n int) []*Bot {
	var out []*Bot
	for _, f := range friendsOf(b.charName(), friendTierAcq) {
		if o := botByCharName(f.Name); o != nil && o != b && o.Online && o.charName() == f.Name {
			out = append(out, o)
			if len(out) >= n {
				break
			}
		}
	}
	return out
}

// friendPartyBonus: how much candidate c's friends in a party (ms) make it want to join, 0..40 - for lfgScore.
func friendPartyBonus(c *Bot, ms []*Bot) int {
	me := c.charName()
	t := 0.0
	for _, m := range ms {
		if m != nil && m != c {
			t += FriendScore(me, m.charName())
		}
	}
	return int(math.Min(40, t/3))
}

// friendsFirst: reorders cands so the leader's friends come first (stable otherwise) - "bots invite friends first".
func friendsFirst(leader *Bot, cands []*Bot) {
	me := leader.charName()
	sc := make(map[*Bot]float64, len(cands))
	for _, c := range cands {
		sc[c] = FriendScore(me, c.charName())
	}
	sort.SliceStable(cands, func(i, j int) bool { return sc[cands[i]] >= friendTierAcq && sc[cands[i]] > sc[cands[j]] })
}

// ---------------------------------------------------------------- guild moves

// friendMoveWants (b.mu held): b is moving to guild name to follow its friends - accept that invitation.
func (b *Bot) friendMoveWants(guild string) bool {
	friends.Lock()
	defer friends.Unlock()
	for _, m := range friends.moves {
		if m.Account == b.P.Account && m.Stage == "invite" && strings.EqualFold(m.To, guild) {
			return true
		}
	}
	return false
}

// friendRecruit (leader's b.mu held): an online, unguilded friend of this guild leader to invite first.
func (b *Bot) friendRecruit() *Bot {
	for _, o := range bestFriendsOnline(b, 6) {
		if o.GuildID == 0 && o.leaderVision() == nil && o.pin == nil && FriendScore(b.charName(), o.charName()) >= friendTierFriend &&
			time.Now().After(b.buffAt["friendinv:"+o.charName()]) {
			b.buffAt["friendinv:"+o.charName()] = time.Now().Add(15 * time.Minute)
			return o
		}
	}
	return nil
}

// onGuildLeft (b.mu held): ZC_ACK_LEAVE_GUILD 0x015a / ZC_ACK_BAN_GUILD 0x015c for us - we are out of the guild.
func (b *Bot) onGuildLeft(how string) {
	if b.GuildID == 0 && b.GuildName == "" {
		return
	}
	old := b.GuildName
	b.GuildID, b.GuildName, b.guildMaster = 0, "", false
	guildRoster.Delete(b.P.Account)
	b.event("guild", "", how+" the guild "+old)
	b.remember("%s the guild %s", how, old)
}

// ---------------------------------------------------------------- manager

type friendSnap struct {
	b         *Bot
	name      string
	online    bool
	m         string
	kills     int
	conn      time.Time
	guild     string
	gid       uint32
	master    bool
	vision    bool
	pinned    bool
	human     bool // in Ayla's party / guild
	partyName string
	// loyalty.go (help requests, retention)
	lv, zeny, pots, potMin, deaths int
	gearItem                       string
	gearShort                      int
	healer, invReady               bool
}

type friendState struct {
	lastKills map[*Bot]int
	lastConn  map[*Bot]time.Time
	greetAt   map[*Bot]time.Time
	tick      int
	movesDay  []time.Time
	rng       *rand.Rand
	start     time.Time
}

func friendsManager(bots []*Bot, stop <-chan struct{}) {
	friendsLoad()
	st := &friendState{lastKills: map[*Bot]int{}, lastConn: map[*Bot]time.Time{}, greetAt: map[*Bot]time.Time{},
		rng: rand.New(rand.NewSource(time.Now().UnixNano())), start: time.Now()}
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			friendsSave(true)
			return
		case <-t.C:
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("friends: tick panic: %v", r)
				}
			}()
			friendsTick(bots, st, time.Now())
		}()
	}
}

func friendsSnapshot(bots []*Bot) ([]*friendSnap, map[string][]*friendSnap, map[string]string) {
	// parties first (never parties.Lock inside a b.mu)
	type pinfo struct {
		name  string
		human string
	}
	ofBot := map[*Bot]pinfo{}
	parties.Lock()
	for _, p := range parties.byName {
		for _, m := range p.Members {
			ofBot[m] = pinfo{p.Name, p.Human}
		}
	}
	parties.Unlock()
	snaps := make([]*friendSnap, 0, len(bots))
	byParty := map[string][]*friendSnap{}
	humanOf := map[string]string{}
	for _, b := range bots {
		b.mu.Lock()
		s := &friendSnap{b: b, name: b.charName(), online: b.Online && b.MapName != "", m: b.MapName, kills: b.Kills,
			conn: b.ConnectedAt, guild: b.GuildName, gid: b.GuildID, master: b.guildMaster, vision: b.leaderVision() != nil,
			pinned: b.pin != nil}
		s.human = humanGuildMember(b)
		s.lv, s.zeny, s.deaths, s.healer, s.invReady = b.BaseLvOr(), b.Zeny, b.Deaths, b.isHealer(), b.invReady()
		if s.online {
			s.pots, s.potMin = b.realPotCount(), b.potMin()
			if g := b.gearNext; g != nil && g.Cost > g.Have {
				s.gearItem, s.gearShort = g.Item, g.Cost-g.Have
			}
		}
		b.mu.Unlock()
		if pi, ok := ofBot[b]; ok {
			s.partyName = pi.name
			if pi.human != "" {
				humanOf[pi.name] = pi.human
			}
		}
		snaps = append(snaps, s)
		if s.online && s.partyName != "" {
			byParty[s.partyName] = append(byParty[s.partyName], s)
		}
	}
	return snaps, byParty, humanOf
}

func friendsTick(bots []*Bot, st *friendState, t time.Time) {
	st.tick++
	snaps, byParty, humanOf := friendsSnapshot(bots)
	friendGreetings(snaps, byParty, st, t)
	helpAnswerTick(snaps, t) // loyalty.go: open help requests get answered (or expire)
	if st.tick%4 != 0 {      // scoring once a minute
		return
	}
	minute := st.tick / 4
	friends.Lock()
	// partied together on the same map; fighting side by side
	for pn, ms := range byParty {
		for i := 0; i < len(ms); i++ {
			ki := ms[i].kills - st.lastKills[ms[i].b]
			for j := i + 1; j < len(ms); j++ {
				if ms[i].m != ms[j].m {
					continue
				}
				friendAddLocked(ms[i].name, ms[j].name, fkParty, 1, t)
				if kj := ms[j].kills - st.lastKills[ms[j].b]; ki > 0 && kj > 0 && st.lastKills[ms[i].b] > 0 && st.lastKills[ms[j].b] > 0 {
					friendAddLocked(ms[i].name, ms[j].name, fkFight, 1, t)
				}
			}
			if h := humanOf[pn]; h != "" { // the human leader (Ayla) partied with her friends
				if hs, ok := humanAt(h); ok && hs.Map == ms[i].m && t.Sub(hs.At) < 3*time.Minute {
					friendAddLocked(ms[i].name, h, fkParty, 1, t)
				}
			}
		}
	}
	// same guild, same map, not in one party
	groups := map[string][]*friendSnap{}
	for _, s := range snaps {
		if s.online && s.guild != "" {
			groups[s.guild+"|"+s.m] = append(groups[s.guild+"|"+s.m], s)
		}
	}
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		for i, a := range g {
			js := []int{}
			if len(g) <= 12 {
				for j := i + 1; j < len(g); j++ {
					js = append(js, j)
				}
			} else {
				for n := 0; n < 3; n++ {
					if j := st.rng.Intn(len(g)); j != i {
						js = append(js, j)
					}
				}
			}
			for _, j := range js {
				o := g[j]
				if a.partyName != "" && a.partyName == o.partyName {
					continue
				}
				friendAddLocked(a.name, o.name, fkGuild, 1, t)
			}
		}
	}
	friends.Unlock()
	for _, s := range snaps {
		st.lastKills[s.b] = s.kills
	}
	if !friends.seeded && minute >= 5 {
		friendsSeed(snaps, byParty, humanOf, t)
	}
	friendBanter(byParty, st, t)
	friendDiary(t)
	loyaltyMinute(snaps, byParty, t) // loyalty.go: party care, help requests
	if ownsBotParties() {
		friendMoves(snaps, st, t)
	}
	if minute%10 == 0 {
		loyaltyTenMin(snaps, t) // loyalty.go: decay, members at risk, leaders keeping them
		friendsDecay(t)
		friendsBonusRefresh(bots)
		friendsLoadPeer()
		friendsSave(loyaltyDirtyTake())
	}
}

// friendsSeed: the first start of the system - current party mates, Ayla's party and guild mates know each other a bit.
func friendsSeed(snaps []*friendSnap, byParty map[string][]*friendSnap, humanOf map[string]string, t time.Time) {
	friends.Lock()
	defer friends.Unlock()
	set := func(a, b string, s float64) {
		k, a, b := friendKey(a, b)
		p := friends.pairs[k]
		if p == nil {
			p = &friendPair{A: a, B: b, First: t, Last: t, decayAt: t}
			friends.pairs[k] = p
		}
		if p.Score < s {
			p.Score, p.Seeded = s, true
		}
	}
	for pn, ms := range byParty {
		v := 18.0
		if h := humanOf[pn]; h != "" {
			v = 30
			for _, m := range ms {
				set(m.name, h, v)
			}
		}
		for i := range ms {
			for j := i + 1; j < len(ms); j++ {
				set(ms[i].name, ms[j].name, v)
			}
		}
	}
	byGuild := map[string][]string{}
	for _, s := range snaps {
		g := s.guild
		if g == "" {
			if v, ok := guildRoster.Load(s.b.P.Account); ok {
				g = v.(string)
			}
		}
		if g != "" {
			byGuild[g] = append(byGuild[g], s.name)
		}
	}
	r := rand.New(rand.NewSource(t.UnixNano()))
	for _, ns := range byGuild {
		for i, a := range ns {
			if len(ns) <= 11 {
				for j := i + 1; j < len(ns); j++ {
					set(a, ns[j], 4)
				}
				continue
			}
			for n := 0; n < 5; n++ {
				if j := r.Intn(len(ns)); j != i {
					set(a, ns[j], 4)
				}
			}
		}
	}
	friends.seeded, friends.dirty = true, true
	friends.counters["seeded pairs"] = len(friends.pairs)
	log.Printf("friends: seeded %d pairs from parties and guilds", len(friends.pairs))
}

func friendsDecay(t time.Time) {
	friends.Lock()
	defer friends.Unlock()
	for k, p := range friends.pairs {
		if p.decayAt.IsZero() {
			p.decayAt = p.Last
		}
		if t.Sub(p.Last) < friendDecayAfter {
			p.decayAt = t
			continue
		}
		dt := t.Sub(p.decayAt)
		if dt <= 0 {
			continue
		}
		p.Score *= math.Pow(0.5, float64(dt)/float64(friendHalfLife))
		p.decayAt = t
		friends.dirty = true
		if p.Score < 1 && t.Sub(p.Last) > 72*time.Hour {
			delete(friends.pairs, k)
		}
	}
}

// friendsBonusRefresh: the minds' manual relations in memory/<account>.json (only memories loaded in this instance).
func friendsBonusRefresh(bots []*Bot) {
	bonus := map[string]float64{}
	for _, b := range bots {
		v, ok := memories.Load(b.P.Account)
		if !ok {
			continue
		}
		me := b.charName()
		m := v.(*Memory)
		m.mu.Lock()
		for name, p := range m.People {
			r := strings.ToLower(p.Relation)
			x := 0.0
			switch {
			case strings.Contains(r, "enemy") || strings.Contains(r, "hate"):
				x = -30
			case strings.Contains(r, "rival"):
				x = -15
			case strings.Contains(r, "best") || strings.Contains(r, "close"):
				x = 20
			case strings.Contains(r, "friend"):
				x = 10
			}
			if x != 0 && name != me {
				bonus[me+"|"+name] = x
			}
		}
		m.mu.Unlock()
	}
	friends.Lock()
	friends.bonus = bonus
	friends.Unlock()
}

// friendDiary: memory diary entries for new friendships (characters of this instance only).
func friendDiary(t time.Time) {
	friends.Lock()
	ps := friends.promos
	friends.promos = nil
	friends.Unlock()
	for _, p := range ps {
		for _, side := range [][2]string{{p.a, p.b}, {p.b, p.a}} {
			b := botByCharName(side[0])
			if b == nil || b.charName() != side[0] {
				continue
			}
			line := fmt.Sprintf("[%s] %s and I have become good friends - we've spent a lot of time together", side[0], side[1])
			if p.tier == "close friend" {
				line = fmt.Sprintf("[%s] %s is one of my closest friends now", side[0], side[1])
			}
			memoryOf(b.P).add(line)
			friends.Lock()
			friends.counters["diary entries"]++
			friends.Unlock()
		}
	}
}

// ---------------------------------------------------------------- chat between friends

func friendSay(b *Bot, channel, to, text string) {
	text = styled(b.P.Typing, text)
	switch channel {
	case "party":
		b.partySay(text)
	case "guild":
		b.guildSay(text)
	default:
		b.whisper(to, text)
	}
}

// friendGreetings: a bot that just logged in greets its best online friend; the friend answers.
func friendGreetings(snaps []*friendSnap, byParty map[string][]*friendSnap, st *friendState, t time.Time) {
	byName := map[string]*friendSnap{}
	for _, s := range snaps {
		if s.online {
			byName[s.name] = s
		}
	}
	for _, s := range snaps {
		if !s.online || s.conn.IsZero() {
			continue
		}
		if st.lastConn[s.b].Equal(s.conn) {
			continue
		}
		if t.Sub(s.conn) < 20*time.Second {
			continue // let the login settle first
		}
		st.lastConn[s.b] = s.conn
		if t.Sub(s.conn) > 5*time.Minute || t.Sub(st.start) < 3*time.Minute {
			continue // a restart brings everyone online at once: no greeting storm
		}
		if t.Before(st.greetAt[s.b]) {
			continue
		}
		var f *friendSnap
		var fs float64
		for _, c := range friendsOf(s.name, friendTierFriend) {
			if o := byName[c.Name]; o != nil {
				f, fs = o, c.Score
				break
			}
		}
		if f == nil {
			continue
		}
		p := 0.55
		if fs >= friendTierClose {
			p = 0.85
		}
		if st.rng.Float64() > p {
			continue
		}
		st.greetAt[s.b] = t.Add(2 * time.Hour)
		ch := "whisper"
		if s.partyName != "" && s.partyName == f.partyName {
			ch = "party"
		} else if s.guild != "" && s.guild == f.guild && fs < friendTierClose {
			ch = "guild"
		}
		greet := pick("hey %s :)", "yo %s", "hi %s! you on too?", "%s! o/", "evening %s", "hey %s, whatcha up to")
		reply := pick("hey hey :)", "yo! good to see you", "hiya :D", "o/ just farming a bit", "hey! been a while", "sup, glad you're on")
		a, o := s.b, f.b
		name := f.name
		me := s.name
		friends.Lock()
		friends.counters["greetings"]++
		friends.Unlock()
		go func() {
			time.Sleep(time.Duration(2+crand(6)) * time.Second)
			friendSay(a, ch, name, fmt.Sprintf(greet, name))
			if crand(100) < 85 {
				time.Sleep(time.Duration(3+crand(6)) * time.Second)
				friendSay(o, ch, me, reply)
				friends.Lock()
				friends.counters["greeting replies"]++
				friends.Unlock()
			}
			friendNote(me, name, fkChat)
		}()
	}
}

// friendBanter: friends partying together chat now and then (at most ~4 lines a minute server-wide).
func friendBanter(byParty map[string][]*friendSnap, st *friendState, t time.Time) {
	said := 0
	for _, ms := range byParty {
		for i := 0; i < len(ms) && said < 4; i++ {
			for j := i + 1; j < len(ms) && said < 4; j++ {
				if ms[i].m != ms[j].m {
					continue
				}
				s := FriendScore(ms[i].name, ms[j].name)
				if s < friendTierFriend || st.rng.Float64() > 0.004+0.004*(s-friendTierFriend)/30 {
					continue
				}
				a, o := ms[i], ms[j]
				if st.rng.Intn(2) == 0 {
					a, o = o, a
				}
				line := fmt.Sprintf(pick("nice one %s", "%s you're on fire today", "good to be out hunting with you again %s",
					"%s remember when we got wrecked last time lol", "%s wanna keep going after this?", "lol %s carrying as usual",
					"thx for sticking around %s", "%s we make a good team"), o.name)
				reply := pick("haha ty", "lol yeah", "anytime :)", "sure, let's go", "we do!", "hehe", "for sure")
				said++
				friends.Lock()
				friends.counters["banter"]++
				friends.Unlock()
				go func(a, o *Bot) {
					time.Sleep(time.Duration(1+crand(20)) * time.Second)
					friendSay(a, "party", "", line)
					if crand(100) < 60 {
						time.Sleep(time.Duration(3+crand(5)) * time.Second)
						friendSay(o, "party", "", reply)
					}
				}(a.b, o.b)
			}
		}
	}
}

// ---------------------------------------------------------------- guild moves (main instance)

func friendMoveEligible(s *friendSnap) bool {
	// owner 2026-11-14: "Starveil members can freely move" - only Ayla's party friends (pinned) stay
	if !s.online || s.master || s.vision || s.pinned || s.guild == "" {
		return false
	}
	if g := guildByName[strings.ToLower(s.guild)]; g != nil {
		if s.name == guildAlchemistChar2(g.ID) || s.name == guildTrader(g.ID) {
			return false // the guild's brewer / trader stays
		}
	}
	return true
}

func friendMoves(snaps []*friendSnap, st *friendState, t time.Time) {
	byName := map[string]*friendSnap{}
	masters := map[string]*friendSnap{} // guild -> its online master bot
	for _, s := range snaps {
		byName[s.name] = s
		if s.online && s.master && s.guild != "" {
			masters[strings.ToLower(s.guild)] = s
		}
	}
	friendMovesAdvance(byName, masters, t)
	// day cap
	keep := st.movesDay[:0]
	for _, x := range st.movesDay {
		if t.Sub(x) < 24*time.Hour {
			keep = append(keep, x)
		}
	}
	st.movesDay = keep
	if len(st.movesDay) >= friendMovesPerDay || t.Sub(st.start) < 20*time.Minute {
		return
	}
	for _, s := range snaps {
		if !friendMoveEligible(s) || friendMovedRecently(s.b.P.Account, t) {
			continue
		}
		// where do its close friends sit?
		var here float64
		there := map[string]float64{}
		names := map[string][]string{}
		closeThere := map[string]int{}
		for _, f := range friendsOf(s.name, friendTierFriend) {
			g := ""
			if o := byName[f.Name]; o != nil {
				g = o.guild
			} else if o := botByCharName(f.Name); o != nil {
				if v, ok := guildRoster.Load(o.P.Account); ok {
					g = v.(string)
				}
			}
			if g == "" {
				continue
			}
			if strings.EqualFold(g, s.guild) {
				here += f.Score
				continue
			}
			there[g] += f.Score
			names[g] = append(names[g], f.Name)
			if f.Score >= friendTierClose {
				closeThere[g]++
			}
		}
		best, bs := "", 0.0
		for g, v := range there {
			if v > bs && closeThere[g] >= 2 && !isHumanGuild(g) && masters[strings.ToLower(g)] != nil {
				best, bs = g, v
			}
		}
		loyal := loyaltyOf(s.name, s.guild) // loyalty.go: a guild that takes care of its member keeps it
		if best == "" || loyal >= 85 || bs < 1.5*here+40+0.8*loyal {
			continue
		}
		share := bs / (bs + here)
		if st.rng.Float64() > friendMoveDayP*share*2*(1-loyal/100)/1440 { // per-minute chance of a qualifying day
			continue
		}
		friendMoveStart(s, best, names[best], t)
		st.movesDay = append(st.movesDay, t)
		return // one start per minute
	}
}

func friendMovedRecently(acc string, t time.Time) bool {
	friends.Lock()
	defer friends.Unlock()
	for _, m := range friends.moves {
		if m.Account == acc && (m.Stage == "leaving" || m.Stage == "invite" || t.Sub(m.Started) < friendMoveCooldown) {
			return true
		}
	}
	return false
}

func friendMoveStart(s *friendSnap, to string, buddies []string, t time.Time) {
	m := &friendMove{Bot: s.name, Account: s.b.P.Account, From: s.guild, To: to, Friends: buddies, Stage: "leaving", Started: t, At: t}
	friends.Lock()
	friends.moves = append(friends.moves, m)
	if len(friends.moves) > 100 {
		friends.moves = friends.moves[len(friends.moves)-100:]
	}
	friends.counters["guild moves started"]++
	friends.dirty = true
	friends.Unlock()
	b, gid := s.b, s.gid
	line := fmt.Sprintf(pick("gonna join my buddies in %s, gl all", "heading over to %s to play with my friends, thanks for everything guys",
		"moving to %s with my friends, cya around :)", "my friends are all in %s so i'm joining them. take care all!"), to)
	log.Printf("friends: %s leaves %s to join %s (friends %v)", s.name, s.guild, to, buddies)
	go func() {
		b.mu.Lock()
		b.guildSay(styled(b.P.Typing, line))
		b.event("guild", "", "leaving "+m.From+" to join friends in "+to)
		b.remember("left %s to join my friends (%s) in %s", m.From, strings.Join(buddies, ", "), to)
		b.mu.Unlock()
		time.Sleep(time.Duration(5+crand(8)) * time.Second)
		b.send(newPkt(0x0159).u32(gid).u32(b.aid).u32(b.cid).str("", 40).b) // CZ_REQ_LEAVE_GUILD
	}()
}

// friendMovesAdvance: leave confirmed -> the new guild's master invites -> joined (hello) / timeouts.
func friendMovesAdvance(byName, masters map[string]*friendSnap, t time.Time) {
	friends.Lock()
	var act []*friendMove
	for _, m := range friends.moves {
		if m.Stage == "leaving" || m.Stage == "invite" {
			act = append(act, m)
		}
	}
	friends.Unlock()
	for _, m := range act {
		s := byName[m.Bot]
		set := func(stage, why string) {
			friends.Lock()
			m.Stage, m.Why, m.At = stage, why, t
			friends.counters["guild moves "+stage]++
			friends.dirty = true
			friends.Unlock()
		}
		switch {
		case s == nil || !s.online:
			if t.Sub(m.At) > 30*time.Minute {
				set("failed", "went offline")
			}
		case m.Stage == "leaving":
			if s.guild == "" {
				set("invite", "")
			} else if t.Sub(m.At) > 3*time.Minute {
				set("failed", "the leave was not confirmed")
			}
		case m.Stage == "invite":
			if strings.EqualFold(s.guild, m.To) {
				set("done", "")
				b := s.b
				hello := pick("hey all! my friends talked me into coming :)", "hi guys, finally here with my friends", "hello everyone o/ glad to be here",
					"hiya! %s dragged me over lol")
				if strings.Contains(hello, "%s") && len(m.Friends) > 0 {
					hello = fmt.Sprintf(hello, m.Friends[0])
				} else if strings.Contains(hello, "%s") {
					hello = "hi everyone o/"
				}
				go func() {
					time.Sleep(time.Duration(3+crand(5)) * time.Second)
					friendSay(b, "guild", "", hello)
					if len(m.Friends) > 0 {
						if f := botByCharName(m.Friends[0]); f != nil && f.Online && crand(100) < 75 {
							time.Sleep(time.Duration(3+crand(5)) * time.Second)
							friendSay(f, "guild", "", fmt.Sprintf(pick("welcome %s!!", "yay %s is here :D", "finally! welcome %s"), m.Bot))
						}
					}
				}()
				friends.Lock()
				friends.counters["guild changes due to friends"]++
				friends.Unlock()
				continue
			}
			if s.guild != "" {
				set("failed", "joined "+s.guild+" instead")
				continue
			}
			ms := masters[strings.ToLower(m.To)]
			if ms == nil {
				if t.Sub(m.At) > 20*time.Minute {
					set("failed", "no master of "+m.To+" online")
				}
				continue
			}
			if m.Invites >= 3 && t.Sub(m.inviteAt) > 3*time.Minute {
				set("failed", "the invitation was not accepted (guild full?)")
				continue
			}
			if m.Invites < 3 && t.Sub(m.inviteAt) > 90*time.Second {
				friends.Lock()
				m.Invites++
				m.inviteAt = t
				friends.Unlock()
				lead, target := ms.b, s.b
				go func() {
					time.Sleep(time.Duration(2+crand(6)) * time.Second)
					lead.send(newPkt(0x0168).u32(target.aid).u32(lead.aid).u32(lead.cid).b) // CZ_REQ_JOIN_GUILD
				}()
			}
		}
	}
}

// ---------------------------------------------------------------- persistence

type friendFileData struct {
	Saved    time.Time        `json:"saved"`
	Seeded   bool             `json:"seeded"`
	Pairs    []*friendPair    `json:"pairs"`
	Moves    []*friendMove    `json:"moves"`
	Counters map[string]int   `json:"counters"`
	Extra    *json.RawMessage `json:"extra,omitempty"`
	// loyalty.go
	Loyalty      []*guildLoyal  `json:"loyalty,omitempty"`
	Help         []*helpReq     `json:"help,omitempty"`
	LoyalCounter map[string]int `json:"loyalty_counters,omitempty"`
}

func friendsReadFile(path string) (*friendFileData, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d friendFileData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func friendsLoad() {
	path := friendFile()
	d, err := friendsReadFile(path)
	if err != nil {
		for _, bk := range []string{path + ".bak1", path + ".bak2", path + ".bak3"} {
			if d, err = friendsReadFile(bk); err == nil {
				log.Printf("friends: %s unreadable, restored from %s", path, bk)
				break
			}
		}
	}
	friends.Lock()
	defer friends.Unlock()
	if d == nil {
		return
	}
	for _, p := range d.Pairs {
		if !memoryNameOK(p.A) || !memoryNameOK(p.B) { // memory.go: test accounts / NPCs never count
			friends.dirty = true
			continue
		}
		k, a, b := friendKey(p.A, p.B)
		p.A, p.B, p.decayAt = a, b, p.Last
		friends.pairs[k] = p
	}
	friends.moves = d.Moves
	for _, m := range friends.moves {
		if m.Stage == "leaving" || m.Stage == "invite" {
			m.Stage, m.Why = "failed", "interrupted by a restart"
		}
	}
	if d.Counters != nil {
		friends.counters = d.Counters
	}
	loyaltyLoad(d)
	friends.seeded = d.Seeded || len(d.Pairs) > 0
	friends.loadedN = len(d.Pairs)
	log.Printf("friends: loaded %d pairs from %s", len(d.Pairs), path)
	friendsLoadPeerLocked()
}

func friendsLoadPeer() {
	friends.Lock()
	defer friends.Unlock()
	friendsLoadPeerLocked()
}

func friendsLoadPeerLocked() {
	d, err := friendsReadFile(friendPeerFile())
	if err != nil {
		return
	}
	peer := make(map[string]*friendPair, len(d.Pairs))
	for _, p := range d.Pairs {
		k, a, b := friendKey(p.A, p.B)
		p.A, p.B = a, b
		peer[k] = p
	}
	friends.peer = peer
	loyaltyLoadPeer(d)
}

func friendsSave(force bool) {
	friends.Lock()
	if !friends.dirty && !force {
		friends.Unlock()
		return
	}
	d := friendFileData{Saved: time.Now(), Seeded: friends.seeded, Counters: map[string]int{}}
	for _, p := range friends.pairs {
		c := *p
		c.rate = nil
		d.Pairs = append(d.Pairs, &c)
	}
	for _, m := range friends.moves {
		c := *m
		d.Moves = append(d.Moves, &c)
	}
	for k, v := range friends.counters {
		d.Counters[k] = v
	}
	n, loaded := len(d.Pairs), friends.loadedN
	backup := time.Since(friends.backupAt) > time.Hour
	if backup {
		friends.backupAt = time.Now()
	}
	friends.dirty = false
	friends.Unlock()
	loyaltyFill(&d)
	if loaded > 50 && n*2 < loaded { // pairs only fade slowly: a sudden drop is a bug, keep the file
		log.Printf("friends: not saving %d pairs over %d (sudden drop)", n, loaded)
		return
	}
	sort.Slice(d.Pairs, func(i, j int) bool { return d.Pairs[i].Score > d.Pairs[j].Score })
	raw, err := json.Marshal(d)
	if err != nil {
		return
	}
	path := friendFile()
	if backup {
		if _, err := os.Stat(path); err == nil {
			os.Remove(path + ".bak3")
			os.Rename(path+".bak2", path+".bak3")
			os.Rename(path+".bak1", path+".bak2")
			if old, err := os.ReadFile(path); err == nil {
				writeFileAtomic(path+".bak1", old)
			}
		}
	}
	if err := writeFileAtomic(path, raw); err != nil {
		log.Printf("friends: save: %v", err)
		return
	}
	friends.Lock()
	if n > friends.loadedN {
		friends.loadedN = n
	}
	friends.Unlock()
}

// ---------------------------------------------------------------- API

type friendPairOut struct {
	A        string  `json:"a"`
	B        string  `json:"b"`
	Score    float64 `json:"score"`
	Raw      float64 `json:"raw"`
	Tier     string  `json:"tier"`
	PartyMin int     `json:"partied_min"`
	Fights   int     `json:"fight_min"`
	Heals    int     `json:"heals"`
	Chats    int     `json:"chats"`
	Trades   int     `json:"trades"`
	GuildMin int     `json:"guild_min"`
	Last     string  `json:"last_contact"`
	AGuild   string  `json:"a_guild,omitempty"`
	BGuild   string  `json:"b_guild,omitempty"`
	Seeded   bool    `json:"seeded,omitempty"`
}

func guildOfChar(name string) string {
	if b := botByCharName(name); b != nil {
		if v, ok := guildRoster.Load(b.P.Account); ok {
			return v.(string)
		}
	}
	return ""
}

func registerFriends(mux *http.ServeMux) {
	mux.HandleFunc("/api/friends", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		n, _ := strconv.Atoi(q.Get("n"))
		if n <= 0 {
			n = 30
		}
		friends.Lock()
		merged := map[string]*friendPair{}
		for k, p := range friends.peer {
			merged[k] = p
		}
		for k, p := range friends.pairs {
			if o := merged[k]; o == nil || p.Score >= o.Score {
				merged[k] = p
			}
		}
		tiers := map[string]int{"acquaintance": 0, "friend": 0, "close friend": 0}
		var all []friendPairOut
		for _, p := range merged {
			s := friendEffLocked(p.A, p.B)
			t := friendTier(s)
			if t != "" {
				tiers[t]++
			}
			all = append(all, friendPairOut{A: p.A, B: p.B, Score: math.Round(s*10) / 10, Raw: math.Round(p.Score*10) / 10, Tier: t,
				PartyMin: int(p.PartyMin), Fights: p.Fights, Heals: p.Heals, Chats: p.Chats, Trades: p.Trades, GuildMin: int(p.GuildMin),
				Last: p.Last.Format("2006-01-02 15:04"), Seeded: p.Seeded})
		}
		moves := append([]*friendMove(nil), friends.moves...)
		counters := map[string]int{}
		for k, v := range friends.counters {
			counters[k] = v
		}
		ownN, peerN := len(friends.pairs), len(friends.peer)
		friends.Unlock()
		sort.Slice(all, func(i, j int) bool { return all[i].Score > all[j].Score })
		out := map[string]interface{}{"pairs_total": len(all), "pairs_this_instance": ownN, "pairs_peer": peerN, "tiers": tiers,
			"thresholds": map[string]float64{"acquaintance": friendTierAcq, "friend": friendTierFriend, "close_friend": friendTierClose},
			"counters":   counters, "half_life_days": friendHalfLife.Hours() / 24,
			"move_policy": fmt.Sprintf("%.0f%% a qualifying day (2+ close friends in a Legion guild whose master is online, their scores > 1.5x own guild's + 40), max %d a day, once a week per bot; never masters/leaders, Ayla's party, Starveil, guild alchemist/trader", friendMoveDayP*100, friendMovesPerDay)}
		top := all
		if len(top) > n {
			top = top[:n]
		}
		for i := range top {
			top[i].AGuild, top[i].BGuild = guildOfChar(top[i].A), guildOfChar(top[i].B)
		}
		out["top"] = top
		var done []*friendMove
		for i := len(moves) - 1; i >= 0; i-- {
			done = append(done, moves[i])
		}
		out["guild_moves"] = done
		changes := 0
		for _, m := range moves {
			if m.Stage == "done" {
				changes++
			}
		}
		out["guild_changes_due_to_friends"] = changes
		out["loyalty"] = loyaltySummary() // loyalty.go; details on /api/guild-loyalty
		if name := q.Get("bot"); name != "" {
			fl := friendsOf(name, 0.5)
			type fo struct {
				nameScore
				Tier  string `json:"tier"`
				Guild string `json:"guild,omitempty"`
			}
			var list []fo
			for _, f := range fl {
				f.Score = math.Round(f.Score*10) / 10
				list = append(list, fo{f, friendTier(f.Score), guildOfChar(f.Name)})
			}
			out["bot"] = map[string]interface{}{"name": name, "guild": guildOfChar(name), "friends": list}
		}
		writeJSON(w, out)
	})
}
