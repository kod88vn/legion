package main

// Guild loyalty and mutual help (owner rule 2026-11-14, RULES.md 3b "Guild loyalty and mutual help": "Starveil members
// can freely move. Let guild leaders send them gears or money to make them stay. Members should have loyalty if their
// guilds take care of them. Let guild members ask for help and see if they get help. Guild should help each other if
// they can. Overall, this is how a normal society would work"). Built on friends.go, persisted in the same files.
//
//   Loyalty: per character to its current guild, 0-100 (50 on first sight / after a guild change). It grows when the
//   guild takes care of it - zeny/gear mailed by a guild mate +6, a trade with a guild mate +1, a heal from a guild
//   mate +0.15 (1/min), a minute partied with guild mates +0.05 (+0.05 more when carried: 10+ levels under them), a
//   help request answered +8, a leader's "stay" gift +12 - and fades when it's ignored: after 24 h without any care
//   -3 a day, a help request nobody answered -5. Gains shrink near the top (pts x (1 - L/120)).
//   In the guild-move rule (friends.go friendMoves) loyalty is the counterweight: the friends' pull must beat
//   1.5x its own guild's friends + 40 + 0.8 x loyalty, the daily chance is scaled by 2 x (1 - loyalty/100), and
//   loyalty >= 85 never moves. Only Ayla's six party friends are fixed; Starveil members may move like anyone.
//
//   At risk: loyalty < 40 and friends (score >= 35) in other guilds summing >= 60. Bot guild leaders (main instance)
//   mail such a member zeny for its next gear (2-20k, at most 1/8 of the leader's purse, leader keeps 30k; one gift per
//   leader per 2 h, per member per day) with a guild-chat line; Starveil's at-risk members are listed in
//   /api/guild-loyalty with the `ayla.sh guild-fund` command for the ayla mind (the human-guild fund pays).
//
//   Help requests (both instances; helpers from the same instance): a guild member who needs something asks in guild
//   chat with a natural line - gear (short <= 30k for its next piece), pots (almost none and < 1.5k zeny), party (solo
//   30+ min, main instance), rescue (3 deaths in 15 min). 10%/min chance while the need holds (x sociability), one
//   request per member per 3 h, one open request per guild, 6 a hour per instance. 20-120 s later a guild mate who can
//   answers: zeny by RODEX (helpGive: the helper keeps 3k, gives at most half the rest and 30% of its purse a day;
//   full help first, else a partial "can chip in 2k" of >= 1/3 and 500z; helps once per 30 min), a party invite
//   (unpartied mate within 15 levels), or comes over (rescue: a mate 5+ levels higher, solo, on the map or 3 hops
//   away hunts on the asker's map for 10 min). Nobody within 10 min = unanswered.
//
// GET /api/guild-loyalty: per guild averages, the least loyal members, members at risk, help stats and the last
// requests, the leaders' keep gifts. /api/friends carries a short summary.

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type guildLoyal struct {
	Char       string    `json:"char"`
	Guild      string    `json:"guild"`
	Loyalty    float64   `json:"loyalty"`
	Since      time.Time `json:"since"`
	LastCare   time.Time `json:"last_care"`
	Gifts      int       `json:"gifts"`
	Asked      int       `json:"asked"`
	Answered   int       `json:"answered"`
	Unanswered int       `json:"unanswered"`
	Kept       int       `json:"kept"` // leader "stay" gifts received
	Helped     int       `json:"helped_others"`
	decayAt    time.Time
	rate       map[string]time.Time
}

type helpReq struct {
	ID       int       `json:"id"`
	Asker    string    `json:"asker"`
	Guild    string    `json:"guild"`
	Kind     string    `json:"kind"` // gear / pots / party / rescue
	Text     string    `json:"text"`
	Map      string    `json:"map"`
	Lv       int       `json:"lv"`
	Amount   int       `json:"amount,omitempty"`
	Item     string    `json:"item,omitempty"`
	Asked    time.Time `json:"asked"`
	State    string    `json:"state"` // open / answered / unanswered / failed
	Helper   string    `json:"helper,omitempty"`
	Answer   string    `json:"answer,omitempty"`
	At       time.Time `json:"at"`
	answerAt time.Time
}

type atRisk struct {
	Char     string   `json:"char"`
	Guild    string   `json:"guild"`
	Loyalty  float64  `json:"loyalty"`
	Pull     float64  `json:"friend_pull"`
	PullTo   string   `json:"pull_to"`
	Friends  []string `json:"friends"`
	Action   string   `json:"action,omitempty"`
	GearNeed int      `json:"gear_short,omitempty"`
}

var loyalty = struct {
	sync.Mutex
	m        map[string]*guildLoyal
	peer     map[string]*guildLoyal
	help     []*helpReq
	counters map[string]int
	risk     []atRisk
	nextID   int
	dirty    bool
	// manager-only state (also under the mutex for simplicity)
	askedAt    map[string]time.Time // asker -> last request
	helpedAt   map[string]time.Time // helper -> last help
	given      map[string]int       // helper -> zeny given today
	givenDay   string
	keepLeader map[string]time.Time // leader -> last keep gift
	keepMember map[string]time.Time // member -> last keep gift
	lastDeaths map[*Bot]int
	deathsAt   map[*Bot][]time.Time
	soloSince  map[*Bot]time.Time
	rng        *rand.Rand
}{m: map[string]*guildLoyal{}, peer: map[string]*guildLoyal{}, counters: map[string]int{},
	askedAt: map[string]time.Time{}, helpedAt: map[string]time.Time{}, given: map[string]int{},
	keepLeader: map[string]time.Time{}, keepMember: map[string]time.Time{}, lastDeaths: map[*Bot]int{},
	deathsAt: map[*Bot][]time.Time{}, soloSince: map[*Bot]time.Time{}, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}

// loyaltyRecLocked (loyalty.Mutex held): the record of char in guild (a new guild starts at 50).
func loyaltyRecLocked(char, guild string, now time.Time) *guildLoyal {
	r := loyalty.m[char]
	if r == nil || !strings.EqualFold(r.Guild, guild) {
		r = &guildLoyal{Char: char, Guild: guild, Loyalty: 50, Since: now, LastCare: now, decayAt: now}
		loyalty.m[char] = r
		loyalty.dirty = true
	}
	return r
}

// loyaltyCare: the guild took care of char (pts > 0) or ignored it (pts < 0). gap rate-limits per kind.
func loyaltyCare(char, guild string, pts float64, kind string, gap time.Duration) {
	if char == "" || guild == "" {
		return
	}
	now := time.Now()
	loyalty.Lock()
	defer loyalty.Unlock()
	r := loyaltyRecLocked(char, guild, now)
	if gap > 0 {
		if r.rate == nil {
			r.rate = map[string]time.Time{}
		}
		if now.Sub(r.rate[kind]) < gap {
			return
		}
		r.rate[kind] = now
	}
	if pts > 0 {
		r.Loyalty = math.Min(100, r.Loyalty+pts*(1-r.Loyalty/120))
		r.LastCare, r.decayAt = now, now
	} else {
		r.Loyalty = math.Max(0, r.Loyalty+pts)
	}
	switch kind {
	case "gift":
		r.Gifts++
	case "kept":
		r.Kept++
	}
	loyalty.dirty = true
}

// loyaltyOf: char's loyalty to guild (50 = unknown).
func loyaltyOf(char, guild string) float64 {
	loyalty.Lock()
	defer loyalty.Unlock()
	if r := loyalty.m[char]; r != nil && strings.EqualFold(r.Guild, guild) {
		return r.Loyalty
	}
	if r := loyalty.peer[char]; r != nil && strings.EqualFold(r.Guild, guild) {
		return r.Loyalty
	}
	return 50
}

func loyaltyDirtyTake() bool {
	loyalty.Lock()
	defer loyalty.Unlock()
	d := loyalty.dirty
	loyalty.dirty = false
	return d
}

// loyaltyFromEvent (friends.go friendNote, after its unlock): a heal / mail / trade between guild mates.
func loyaltyFromEvent(a, b string, kind int) {
	if kind != fkHeal && kind != fkMail && kind != fkTrade {
		return
	}
	ga := guildOfChar(a)
	if ga == "" || !strings.EqualFold(ga, guildOfChar(b)) {
		return
	}
	switch kind {
	case fkHeal: // a healed b
		loyaltyCare(b, ga, 0.15, "heal", time.Minute)
	case fkMail: // a mailed zeny to b
		loyaltyCare(b, ga, 6, "gift", 0)
	case fkTrade:
		loyaltyCare(b, ga, 1, "trade", 10*time.Minute)
		loyaltyCare(a, ga, 1, "trade", 10*time.Minute)
	}
}

// ---------------------------------------------------------------- once a minute

func loyaltyMinute(snaps []*friendSnap, byParty map[string][]*friendSnap, t time.Time) {
	// seen in a guild: a record exists (starts at 50)
	loyalty.Lock()
	for _, s := range snaps {
		if s.online && s.guild != "" {
			loyaltyRecLocked(s.name, s.guild, t)
		}
	}
	loyalty.Unlock()
	// partied with guild mates (carried when 10+ levels under them)
	for _, ms := range byParty {
		for _, a := range ms {
			if a.guild == "" {
				continue
			}
			mates, lvSum := 0, 0
			for _, o := range ms {
				if o != a && o.m == a.m && strings.EqualFold(o.guild, a.guild) {
					mates++
					lvSum += o.lv
				}
			}
			if mates == 0 {
				continue
			}
			pts := 0.05
			if a.lv+10 <= lvSum/mates {
				pts += 0.05
			}
			loyaltyCare(a.name, a.guild, pts, "party", 50*time.Second)
		}
	}
	helpAskTick(snaps, t)
}

// ---------------------------------------------------------------- help requests

func helpNeed(s *friendSnap, t time.Time) (kind, item string, amount int) {
	loyalty.Lock()
	defer loyalty.Unlock()
	// deaths in the last 15 min; time spent solo
	if last, ok := loyalty.lastDeaths[s.b]; ok && s.deaths > last {
		for i := 0; i < s.deaths-last && i < 5; i++ {
			loyalty.deathsAt[s.b] = append(loyalty.deathsAt[s.b], t)
		}
	}
	loyalty.lastDeaths[s.b] = s.deaths
	ds := loyalty.deathsAt[s.b][:0]
	for _, d := range loyalty.deathsAt[s.b] {
		if t.Sub(d) < 15*time.Minute {
			ds = append(ds, d)
		}
	}
	loyalty.deathsAt[s.b] = ds
	if s.partyName != "" || isTown(s.m) {
		delete(loyalty.soloSince, s.b)
	} else if loyalty.soloSince[s.b].IsZero() {
		loyalty.soloSince[s.b] = t
	}
	switch {
	case len(ds) >= 3:
		return "rescue", "", 0
	case s.invReady && s.lv >= 15 && !s.healer && s.potMin > 0 && s.pots < s.potMin/2 && s.zeny < 1500:
		return "pots", "", min(5000, 1500+s.lv*40)
	case s.gearItem != "" && s.gearShort > 0 && s.gearShort <= 30000 && s.lv >= 20:
		return "gear", s.gearItem, s.gearShort + s.gearShort/20
	case ownsBotParties() && s.partyName == "" && s.lv >= 20 && !loyalty.soloSince[s.b].IsZero() &&
		t.Sub(loyalty.soloSince[s.b]) > 30*time.Minute:
		return "party", "", 0
	}
	return "", "", 0
}

func helpLine(kind, item string, amount, lv int, m string, cls string) string {
	switch kind {
	case "gear":
		return fmt.Sprintf(pick("anyone in the guild able to help me with a %s? i'm %s short", "saving for a %s but still %s short, any help appreciated",
			"could someone chip in for my %s? %s to go"), item, zenyShort(amount))
	case "pots":
		return pick("out of pots and broke lol, anyone spare a few?", "could someone help me with pots? down to my last few",
			"no pots no zeny... anyone? :(", "guild, can anyone spare a little for pots")
	case "party":
		return fmt.Sprintf(pick("anyone around lv%d want to party? soloing is slow", "lf guildies to hunt with, lv%d %s", "any guildies lv%d free? wanna hunt together"), lv, strings.ToLower(cls))
	case "rescue":
		return fmt.Sprintf(pick("keep dying on %s, anyone close by?", "help, getting wrecked on %s", "died 3 times on %s already... anyone nearby?"), m)
	}
	return ""
}

func zenyShort(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%dk", (n+500)/1000)
	}
	return fmt.Sprintf("%dz", n)
}

func helpAskTick(snaps []*friendSnap, t time.Time) {
	loyalty.Lock()
	recent, open := 0, map[string]bool{}
	for _, h := range loyalty.help {
		if t.Sub(h.Asked) < time.Hour {
			recent++
		}
		if h.State == "open" {
			open[strings.ToLower(h.Guild)] = true
		}
	}
	loyalty.Unlock()
	order := loyalty.rng.Perm(len(snaps))
	for _, i := range order {
		s := snaps[i]
		if !s.online || s.guild == "" || s.master || s.vision || s.pinned {
			continue
		}
		kind, item, amount := helpNeed(s, t) // keeps the death / solo trackers current for everyone
		if kind == "" || recent >= 6 || open[strings.ToLower(s.guild)] {
			continue
		}
		loyalty.Lock()
		last := loyalty.askedAt[s.name]
		loyalty.Unlock()
		if t.Sub(last) < 3*time.Hour || loyalty.rng.Float64() > 0.1*(0.5+s.b.P.Sociability) {
			continue
		}
		line := helpLine(kind, item, amount, s.lv, s.m, className(s.b.Class))
		loyalty.Lock()
		loyalty.nextID++
		h := &helpReq{ID: loyalty.nextID, Asker: s.name, Guild: s.guild, Kind: kind, Text: line, Map: s.m, Lv: s.lv, Amount: amount,
			Item: item, Asked: t, At: t, State: "open", answerAt: t.Add(time.Duration(20+loyalty.rng.Intn(100)) * time.Second)}
		loyalty.help = append(loyalty.help, h)
		if len(loyalty.help) > 200 {
			loyalty.help = loyalty.help[len(loyalty.help)-200:]
		}
		loyalty.askedAt[s.name] = t
		loyalty.counters["asked "+kind]++
		loyalty.dirty = true
		if r := loyalty.m[s.name]; r != nil {
			r.Asked++
		}
		loyalty.Unlock()
		recent++
		open[strings.ToLower(s.guild)] = true
		b := s.b
		go friendSay(b, "guild", "", line)
	}
}

// helpAnswerTick (every 15 s): due requests find a guild mate who can help; 10 min without one = unanswered.
func helpAnswerTick(snaps []*friendSnap, t time.Time) {
	loyalty.Lock()
	var due []*helpReq
	for _, h := range loyalty.help {
		if h.State == "open" && !t.Before(h.answerAt) {
			due = append(due, h)
		}
	}
	day := t.Format("2006-01-02")
	if loyalty.givenDay != day {
		loyalty.givenDay, loyalty.given = day, map[string]int{}
	}
	loyalty.Unlock()
	if len(due) == 0 {
		return
	}
	byName := map[string]*friendSnap{}
	for _, s := range snaps {
		byName[s.name] = s
	}
	for _, h := range due {
		asker := byName[h.Asker]
		if asker == nil || !asker.online || !strings.EqualFold(asker.guild, h.Guild) {
			helpClose(h, "failed", "", "asker went offline / left", t)
			continue
		}
		if helpTry(h, asker, snaps, t) {
			continue
		}
		if t.Sub(h.Asked) > 10*time.Minute {
			helpClose(h, "unanswered", "", "", t)
			loyaltyCare(h.Asker, h.Guild, -5, "ignored", 0)
			if loyalty.rng.Intn(100) < 30 {
				b := asker.b
				go friendSay(b, "guild", "", pick("nvm, i'll figure it out", "ok nvm", "guess i'm on my own lol", "all good, nvm"))
			}
		} else {
			loyalty.Lock()
			h.answerAt = t.Add(time.Duration(30+loyalty.rng.Intn(60)) * time.Second)
			loyalty.Unlock()
		}
	}
}

func helpClose(h *helpReq, state, helper, answer string, t time.Time) {
	loyalty.Lock()
	defer loyalty.Unlock()
	h.State, h.Helper, h.Answer, h.At = state, helper, answer, t
	loyalty.counters[state+" "+h.Kind]++
	loyalty.dirty = true
	if r := loyalty.m[h.Asker]; r != nil {
		switch state {
		case "answered":
			r.Answered++
		case "unanswered":
			r.Unanswered++
		}
	}
	if r := loyalty.m[helper]; r != nil && state == "answered" {
		r.Helped++
	}
}

// helpTry: one guild mate of this instance answers h, if anyone can right now.
func helpTry(h *helpReq, asker *friendSnap, snaps []*friendSnap, t time.Time) bool {
	var cands []*friendSnap
	for _, s := range snaps {
		if s == asker || !s.online || !strings.EqualFold(s.guild, h.Guild) {
			continue
		}
		loyalty.Lock()
		busy := t.Sub(loyalty.helpedAt[s.name]) < 30*time.Minute
		loyalty.Unlock()
		if !busy {
			cands = append(cands, s)
		}
	}
	// friends of the asker answer first, then the richest / closest
	fs := map[string]float64{}
	for _, c := range cands {
		fs[c.name] = FriendScore(h.Asker, c.name)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if fs[cands[i].name] != fs[cands[j].name] {
			return fs[cands[i].name] > fs[cands[j].name]
		}
		return cands[i].zeny > cands[j].zeny
	})
	switch h.Kind {
	case "gear", "pots":
		return helpZeny(h, asker, cands, t, "")
	case "party":
		for _, c := range cands {
			if c.partyName != "" || c.vision || c.master || abs(c.lv-asker.lv) > 15 || isTown(c.m) && isTown(asker.m) {
				continue
			}
			m := c.m
			if isTown(m) {
				m = asker.m
			}
			if formParty(c.b, []*Bot{asker.b}, m) == nil {
				continue
			}
			helpDone(h, c, pick("sure, sending you an invite", "i'll party! invite sent", "yo, join me", "sure thing, inviting"), t)
			return true
		}
	case "rescue":
		for _, c := range cands {
			if c.partyName != "" || c.vision || c.master || c.pinned || c.lv < asker.lv+5 || (c.m != asker.m && townHops(c.m, asker.m) > 3) {
				continue
			}
			hb, m := c.b, asker.m
			go func() {
				hb.mu.Lock()
				old := hb.Goal
				hb.Goal = Goal{Mode: "hunt", Map: m, Set: "help"}
				hb.mu.Unlock()
				time.Sleep(10 * time.Minute)
				hb.mu.Lock()
				if hb.Goal.Set == "help" {
					hb.Goal = old
				}
				hb.mu.Unlock()
			}()
			helpDone(h, c, fmt.Sprintf(pick("omw to %s, hang on", "coming, stay alive lol", "on my way, sit tight"), m), t)
			return true
		}
		// nobody close and strong enough: money for pots and a hint
		h.Amount = min(4000, 1000+asker.lv*30)
		return helpZeny(h, asker, cands, t, "can't come over but here's something for pots, maybe try an easier map?")
	}
	return false
}

// helpGive: what a guild mate with zeny z (already given today) can spare for an ask of amt - a poor server's
// standard (live 14:00: median purse ~1.1k, the old "keep 25k + 3x" rule answered 0 of 6): it keeps 3k and gives
// at most half of the rest, at most 30% of its purse a day. Full = covers the ask.
func helpGive(z, amt, given int) (give int, full bool) {
	g := (z - 3000) / 2
	if room := z*30/100 - given; g > room {
		g = room
	}
	if g >= amt {
		return amt, true
	}
	return max(g, 0), false
}

// helpPartialOK: a partial gift worth sending ("can chip in 2k") - at least a third of the ask and 500z.
func helpPartialOK(give, amt int) bool { return give >= max(500, amt/3) }

func helpZeny(h *helpReq, asker *friendSnap, cands []*friendSnap, t time.Time, line string) bool {
	amt := h.Amount
	if amt <= 0 {
		return false
	}
	for pass := 0; pass < 2; pass++ { // full help first, then the best partial
		for _, c := range cands {
			loyalty.Lock()
			given := loyalty.given[c.name]
			loyalty.Unlock()
			give, full := helpGive(c.zeny, amt, given)
			if pass == 0 && !full || pass == 1 && !helpPartialOK(give, amt) {
				continue
			}
			hb, to, hreq := c.b, h.Asker, h
			hb.mu.Lock()
			if g2, _ := helpGive(hb.Zeny, amt, given); g2 < give { // the purse changed since the snapshot
				give = g2
			}
			ok := give > 0 && hb.mailZeny(to, give, "for you", "from your guild mate, good luck!", func(ok bool, note string) {
				if !ok {
					loyalty.Lock()
					hreq.Answer += " (mail failed: " + note + ")"
					loyalty.counters["help mail failed"]++
					loyalty.Unlock()
				}
			})
			hb.mu.Unlock()
			if !ok {
				continue
			}
			loyalty.Lock()
			loyalty.given[c.name] += give
			if give < amt {
				loyalty.counters["help partial"]++
			}
			loyalty.Unlock()
			l := line
			switch {
			case l != "":
			case give < amt:
				l = fmt.Sprintf(pick("can chip in %s, hope it helps", "don't have much but sent %s", "mailed you %s, it's what i can spare"), zenyShort(give))
			case h.Kind == "pots":
				l = pick("mailed you a bit for pots :)", "sent you some zeny, grab pots", "check your mail, pots money")
			default:
				l = fmt.Sprintf(pick("sent you %s, go get it :)", "check your mail, %s for the gear", "mailed %s, gl!"), zenyShort(give))
			}
			helpDone(h, c, l, t)
			return true
		}
	}
	return false
}

func helpDone(h *helpReq, c *friendSnap, line string, t time.Time) {
	loyalty.Lock()
	loyalty.helpedAt[c.name] = t
	loyalty.Unlock()
	helpClose(h, "answered", c.name, line, t)
	loyaltyCare(h.Asker, h.Guild, 8, "answered", 0)
	friendNote(h.Asker, c.name, fkChat)
	hb := c.b
	asker := botByCharName(h.Asker)
	go func() {
		friendSay(hb, "guild", "", line)
		if asker != nil && crand(100) < 75 {
			time.Sleep(time.Duration(3+crand(6)) * time.Second)
			friendSay(asker, "guild", "", fmt.Sprintf(pick("thank you %s!!", "ty %s <3", "you're the best %s", "thanks a lot %s :D"), c.name))
		}
	}()
	log.Printf("guild help: %s answered %s's %s request (%s)", c.name, h.Asker, h.Kind, h.Guild)
}

// ---------------------------------------------------------------- every 10 minutes

func loyaltyTenMin(snaps []*friendSnap, t time.Time) {
	// decay without care
	loyalty.Lock()
	for _, r := range loyalty.m {
		if r.decayAt.IsZero() {
			r.decayAt = r.LastCare
		}
		if t.Sub(r.LastCare) < 24*time.Hour {
			r.decayAt = t
			continue
		}
		days := t.Sub(r.decayAt).Hours() / 24
		r.Loyalty = math.Max(0, r.Loyalty-3*days)
		r.decayAt = t
		loyalty.dirty = true
	}
	loyalty.Unlock()
	// members at risk: low loyalty, friends elsewhere
	var risk []atRisk
	byName := map[string]*friendSnap{}
	for _, s := range snaps {
		byName[s.name] = s
	}
	for _, s := range snaps {
		if s.guild == "" || s.master || s.vision || s.pinned {
			continue
		}
		l := loyaltyOf(s.name, s.guild)
		if l >= 40 {
			continue
		}
		pull := map[string]float64{}
		names := map[string][]string{}
		for _, f := range friendsOf(s.name, friendTierFriend) {
			g := guildOfChar(f.Name)
			if g == "" || strings.EqualFold(g, s.guild) {
				continue
			}
			pull[g] += f.Score
			names[g] = append(names[g], f.Name)
		}
		best, bs := "", 0.0
		for g, v := range pull {
			if v > bs {
				best, bs = g, v
			}
		}
		if bs < 60 {
			continue
		}
		r := atRisk{Char: s.name, Guild: s.guild, Loyalty: math.Round(l*10) / 10, Pull: math.Round(bs), PullTo: best, Friends: names[best], GearNeed: s.gearShort}
		amt := keepAmount(s, 0)
		if isHumanGuild(s.guild) {
			r.Action = fmt.Sprintf(`ayla.sh guild-fund "%s" %d "keep: next gear"`, s.name, min(amt, 5000))
		}
		risk = append(risk, r)
		if ownsBotParties() && s.online && !isHumanGuild(s.guild) {
			keepMember(s, amt, t)
		}
	}
	sort.Slice(risk, func(i, j int) bool { return risk[i].Loyalty < risk[j].Loyalty })
	loyalty.Lock()
	loyalty.risk = risk
	loyalty.Unlock()
}

func keepAmount(s *friendSnap, leaderZeny int) int {
	a := s.gearShort
	if a < 2000 {
		a = 2000
	}
	if a > 20000 {
		a = 20000
	}
	if leaderZeny > 0 && a > leaderZeny/8 {
		a = leaderZeny / 8
	}
	return a
}

// keepMember: the bot guild's leader sends an at-risk member something for its gear, with a guild-chat line.
func keepMember(s *friendSnap, _ int, t time.Time) {
	g := guildByName[strings.ToLower(s.guild)]
	if g == nil {
		return
	}
	l := guildLeader(g.ID)
	if l == nil || l == s.b {
		return
	}
	lname := l.charName()
	loyalty.Lock()
	if t.Sub(loyalty.keepLeader[lname]) < 2*time.Hour || t.Sub(loyalty.keepMember[s.name]) < 24*time.Hour {
		loyalty.Unlock()
		return
	}
	loyalty.Unlock()
	l.mu.Lock()
	if !l.Online || l.Zeny < 30000+2000 {
		l.mu.Unlock()
		return
	}
	amt := keepAmount(s, l.Zeny)
	if l.Zeny-amt < 30000 {
		amt = l.Zeny - 30000
	}
	if amt < 1000 {
		l.mu.Unlock()
		return
	}
	member, guild := s.name, s.guild
	ok := l.mailZeny(member, amt, "for your gear", "you're part of the family, get that next piece", func(ok bool, note string) {
		if ok {
			loyaltyCare(member, guild, 12, "kept", 0)
		}
		loyalty.Lock()
		if ok {
			loyalty.counters["keep gifts sent"]++
		} else {
			loyalty.counters["keep gifts failed"]++
		}
		loyalty.dirty = true
		loyalty.Unlock()
	})
	l.mu.Unlock()
	if !ok {
		return
	}
	loyalty.Lock()
	loyalty.keepLeader[lname], loyalty.keepMember[member] = t, t
	loyalty.counters["keep gifts started"]++
	loyalty.Unlock()
	log.Printf("guild loyalty: %s (leader of %s) mails %dz to %s to keep them", lname, guild, amt, member)
	mb := s.b
	go func() {
		time.Sleep(time.Duration(5+crand(20)) * time.Second)
		friendSay(l, "guild", "", fmt.Sprintf(pick("hey %s, sent you a little something for your gear. we look after our own :)",
			"%s check your mail, that's for your next piece", "%s, mailed you some zeny - shout if you need anything"), member))
		if crand(100) < 70 {
			time.Sleep(time.Duration(4+crand(8)) * time.Second)
			friendSay(mb, "guild", "", fmt.Sprintf(pick("aww thanks %s!", "omg ty %s", "thank you, means a lot"), lname))
		}
	}()
}

// ---------------------------------------------------------------- persistence (friends.go file)

func loyaltyLoad(d *friendFileData) {
	loyalty.Lock()
	defer loyalty.Unlock()
	for _, r := range d.Loyalty {
		r.decayAt = r.LastCare
		loyalty.m[r.Char] = r
	}
	for _, h := range d.Help {
		if h.State == "open" {
			h.State, h.Answer = "failed", "interrupted by a restart"
		}
		if h.ID > loyalty.nextID {
			loyalty.nextID = h.ID
		}
		loyalty.askedAt[h.Asker] = h.Asked
	}
	loyalty.help = d.Help
	if d.LoyalCounter != nil {
		loyalty.counters = d.LoyalCounter
	}
}

func loyaltyLoadPeer(d *friendFileData) {
	peer := map[string]*guildLoyal{}
	for _, r := range d.Loyalty {
		peer[r.Char] = r
	}
	loyalty.Lock()
	loyalty.peer = peer
	loyalty.Unlock()
}

func loyaltyFill(d *friendFileData) {
	loyalty.Lock()
	defer loyalty.Unlock()
	for _, r := range loyalty.m {
		c := *r
		c.rate = nil
		d.Loyalty = append(d.Loyalty, &c)
	}
	for _, h := range loyalty.help {
		c := *h
		d.Help = append(d.Help, &c)
	}
	d.LoyalCounter = map[string]int{}
	for k, v := range loyalty.counters {
		d.LoyalCounter[k] = v
	}
}

// ---------------------------------------------------------------- API

func loyaltySummary() map[string]interface{} {
	loyalty.Lock()
	defer loyalty.Unlock()
	asked, answered, unanswered := 0, 0, 0
	for k, v := range loyalty.counters {
		switch {
		case strings.HasPrefix(k, "asked "):
			asked += v
		case strings.HasPrefix(k, "answered "):
			answered += v
		case strings.HasPrefix(k, "unanswered "):
			unanswered += v
		}
	}
	return map[string]interface{}{"members_tracked": len(loyalty.m) + len(loyalty.peer), "at_risk": len(loyalty.risk),
		"help_asked": asked, "help_answered": answered, "help_unanswered": unanswered,
		"keep_gifts_sent": loyalty.counters["keep gifts sent"]}
}

func registerLoyalty(mux *http.ServeMux) {
	mux.HandleFunc("/api/guild-loyalty", func(w http.ResponseWriter, r *http.Request) {
		loyalty.Lock()
		all := map[string]*guildLoyal{}
		for k, v := range loyalty.peer {
			all[k] = v
		}
		for k, v := range loyalty.m {
			all[k] = v
		}
		type gsum struct {
			Guild   string  `json:"guild"`
			Members int     `json:"members"`
			Avg     float64 `json:"avg_loyalty"`
			Low     int     `json:"low_under_35"`
			Asked   int     `json:"help_asked"`
			Ans     int     `json:"help_answered"`
		}
		gs := map[string]*gsum{}
		var members []*guildLoyal
		for _, rec := range all {
			cur := guildOfChar(rec.Char)
			if cur != "" && !strings.EqualFold(cur, rec.Guild) {
				continue // stale: it changed guild (the new record lives in the other instance or comes soon)
			}
			g := gs[rec.Guild]
			if g == nil {
				g = &gsum{Guild: rec.Guild}
				gs[rec.Guild] = g
			}
			g.Members++
			g.Avg += rec.Loyalty
			if rec.Loyalty < 35 {
				g.Low++
			}
			members = append(members, rec)
		}
		var help []*helpReq
		for i := len(loyalty.help) - 1; i >= 0 && len(help) < 40; i-- {
			help = append(help, loyalty.help[i])
		}
		for _, h := range loyalty.help {
			if g := gs[h.Guild]; g != nil {
				g.Asked++
				if h.State == "answered" {
					g.Ans++
				}
			}
		}
		counters := map[string]int{}
		for k, v := range loyalty.counters {
			counters[k] = v
		}
		risk := append([]atRisk(nil), loyalty.risk...)
		loyalty.Unlock()
		var guilds []*gsum
		for _, g := range gs {
			g.Avg = math.Round(g.Avg/float64(g.Members)*10) / 10
			guilds = append(guilds, g)
		}
		sort.Slice(guilds, func(i, j int) bool { return guilds[i].Members > guilds[j].Members })
		sort.Slice(members, func(i, j int) bool { return members[i].Loyalty < members[j].Loyalty })
		if len(members) > 40 {
			members = members[:40]
		}
		writeJSON(w, map[string]interface{}{"guilds": guilds, "least_loyal": members, "at_risk": risk, "help_recent": help,
			"counters": counters, "summary": loyaltySummary(),
			"rules": "loyalty 0-100 per member to its guild; grows with gifts/help/parties/heals from guild mates, -3/day after 24h of no care, -5 per unanswered request; counterweights the friends' pull in guild moves (friends.go)"})
	})
}
