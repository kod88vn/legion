package main

// Leaders talk (owner, repeated 2026-11-14: "ALL party leaders must communicate"; PT10, PT26, PT22, PT03, PR16).
//
// Live sample 2026-11-15 01:58-03:04Z (event ring, 102 talking parties): map moves were already announced (planLine,
// sayPartyDest), but a bot leader said nothing when the party's town run started (it only joined with "town run,
// omw" like a member, or asked "town run? bag is heavy"), nothing when the run was over, nothing while a rally waited
// for someone ("waiting for X"), only "cant all meet up in izlude, lets just go" when it gave up on a late member,
// nothing when it logged off for the day, and 73 of 225 member announcements (brb buying arrows, back, saved, need
// a town run) got no answer from the leader.
//
// This file is the leader's voice for those decisions. It only SAYS what the engine already does (the town the run
// goes to, the party map everyone returns to, who the rally still waits for), so members acting on the engine's plan
// are acting on what the leader said. Once per decision, varied wording, the leader's typing style.
//   - leaderTalkManager (own goroutine, every 4 s): bot-led parties only (Ayla speaks for her own party: partylead.pl
//     say_dest / dest_remind_tick / heard). Town run start and end, rally wait, moving on without a late member, and
//     a member ack ("omw", "k") after a new party map.
//   - leaderHeard (party chat hook, bot.go 0x0109): the bot leader answers a member's announcement (PT26).
//   - dayEndSay (town.go startTrip hook): a member / leader logging off for the day says so instead of vanishing.
//   - leaderTalkSkipTrip (tripAnnounce hook): the leader doesn't also say a member's "town run, omw" on its own run.
// Default on; tuning "leader_talk_off" turns all of it off.

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

func leaderTalkOn() bool { return !tune().LeaderTalkOff }

// ltSay: party chat for this file (a seam for the unit tests).
var ltSay = func(b *Bot, line string) { b.partySay(line) }

// ltRand / ltUnit: the dice and the delay unit (seams for the unit tests).
var (
	ltRand = cfloat
	ltUnit = time.Second
)

// ---- what each bot said lately in party chat (partySay hook): a decision already spoken isn't spoken twice ----

type saidLine struct {
	at   time.Time
	text string
}

var partySaid = struct {
	sync.Mutex
	m map[*Bot][]saidLine
}{m: map[*Bot][]saidLine{}}

// noteSaid: b said text in party chat (partySay).
func noteSaid(b *Bot, text string) {
	if b == nil {
		return
	}
	now := time.Now()
	partySaid.Lock()
	defer partySaid.Unlock()
	ls := partySaid.m[b]
	keep := ls[:0]
	for _, l := range ls {
		if now.Sub(l.at) < 5*time.Minute {
			keep = append(keep, l)
		}
	}
	if len(keep) >= 8 {
		keep = keep[1:]
	}
	partySaid.m[b] = append(keep, saidLine{now, strings.ToLower(text)})
}

// saidRecently: b said something containing one of subs (any case) in the last `within`.
func saidRecently(b *Bot, within time.Duration, now time.Time, subs ...string) bool {
	partySaid.Lock()
	defer partySaid.Unlock()
	for _, l := range partySaid.m[b] {
		if now.Sub(l.at) > within {
			continue
		}
		for _, s := range subs {
			if s != "" && strings.Contains(l.text, strings.ToLower(s)) {
				return true
			}
		}
	}
	return false
}

func typingOf(b *Bot) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.P.Typing
}

// ---- the lines (pure: unit-tested) ----

// listNames: "a", "a and b", "a, b and c"
func listNames(ns []string) string {
	switch len(ns) {
	case 0:
		return ""
	case 1:
		return ns[0]
	}
	return strings.Join(ns[:len(ns)-1], ", ") + " and " + ns[len(ns)-1]
}

// townRunPlanLine: the leader says the party's town run plan - where (the run's town), why (who needs it), how
// (everyone shops there, then back on the party map). by is who called the run.
func townRunPlanLine(by, leader, town, partyMap string) string {
	back := mapTalk(partyMap)
	if partyMap == "" || isTown(partyMap) {
		back = "the spot"
	}
	if strings.EqualFold(by, leader) || by == "" {
		return pick(
			fmt.Sprintf("town run guys: everyone to %s, sell + restock, then we meet back on %s", town, back),
			fmt.Sprintf("lets do a quick town run to %s. sell, pots, then straight back to %s", town, back),
			fmt.Sprintf("ok town run, %s. shop quick, regroup on %s after (~5 min)", town, back))
	}
	return pick(
		fmt.Sprintf("ok %s, town run for all of us: %s, sell + restock, back on %s after", by, town, back),
		fmt.Sprintf("sure, everyone to %s for %s, then we regroup on %s", town, by, back),
		fmt.Sprintf("k town run, %s. shop quick and meet back on %s, ~5 min", town, back),
		fmt.Sprintf("ok lets all go, %s needs town. %s, then back to %s", by, town, back))
}

// townRunEndLine: the run is over - everyone back (late empty), or the leader goes on without the late ones.
func townRunEndLine(partyMap string, late []string) string {
	back := mapTalk(partyMap)
	if len(late) == 0 {
		return pick("all back? ok, farming again", "everyone back, lets go", "ok all here, back to it on "+back,
			"good, everyone's back. same spot")
	}
	who := listNames(late)
	return pick(
		fmt.Sprintf("we start again on %s, %s meet us there", back, who),
		fmt.Sprintf("%s still in town? we're back on %s, come straight here", who, back),
		fmt.Sprintf("ok going on without %s, we're on %s", who, back))
}

// rallyWaitLine: the rally waits at the Kafra for the missing members (left = how long it still waits).
func rallyWaitLine(missing []string, town string, left time.Duration) string {
	mins := int(left.Round(time.Minute) / time.Minute)
	if mins < 1 {
		mins = 1
	}
	who := listNames(missing)
	return pick(
		fmt.Sprintf("waiting for %s at the kafra, %d min max", who, mins),
		fmt.Sprintf("%s where r u? we wait at the %s kafra, %d more min", who, town, mins),
		fmt.Sprintf("still waiting for %s, then we go (%d min)", who, mins))
}

// rallyMissLine: the rally gave up on late members - where they should come instead.
func rallyMissLine(missing []string, dest string) string {
	who := listNames(missing)
	d := mapTalk(dest)
	return pick(
		fmt.Sprintf("%s meet us on %s", who, d),
		fmt.Sprintf("%s we go ahead, come straight to %s", who, d),
		fmt.Sprintf("going to %s now, %s catch up there", d, who))
}

// dayEndLine: logging off for the day, said to the party.
func dayEndLine(leader bool, partyMap string) string {
	if leader {
		return pick("gotta log soon guys, heading to town. keep farming "+mapTalk(partyMap)+" without me",
			"thats it for me today, going to town to log. ty all, keep going",
			"calling it a day, ty for the party. u guys keep it up on "+mapTalk(partyMap))
	}
	return pick("gotta log soon, ty for the party", "calling it a day, cya guys", "thats it for me today, ty all gl")
}

// memberAckLine: a member's short answer to the leader's plan.
func memberAckLine() string {
	return pick("omw", "ok", "k", "kk", "on my way", "ok see u there", "k coming")
}

// replyCtx: what the leader knows when a member speaks.
type replyCtx struct {
	partyMap     string
	townRunFresh bool // the party's town run started in the last minute (the plan line covers it)
	townRunAgo   time.Duration
	leaderMap    string
	lx, ly       int
}

var (
	reLeave   = regexp.MustCompile(`ty for the party|gl guys|gl all|\bcya\b|going solo|gonna lvl|lvl elsewhere|lvl a bit on my own|leaving to|ty all|gotta log|calling it a day|party's over|thats it for me`)
	reTownAsk = regexp.MustCompile(`town run\?|can we go to town|need a town run|need a tonw run|town run pls|town run, |next town run|we go to town next|next time we're in town`)
	reJoin    = regexp.MustCompile(`^(ok |k |yes |sure )?town run(,|$)|coming with|following to town|omw to town|town run, omw|need to sell \+ restock|^coming\b`)
	reErrand  = regexp.MustCompile(`\bbrb\b|^getting |getting an? |gonna grab|\bbuying\b|gonna buy|shopping|refiner|upgrading|respec|renting|^1 min|^sec\b|checking a shop`)
	reBack    = regexp.MustCompile(`^(ok |re,? )?back\b|^back!|done, coming back|^re$|back, omw`)
	reRest    = regexp.MustCompile(`need sp|resting|sitting for sp|need a sec|need hp`)
	reLost    = regexp.MustCompile(`where r u|lost u|cant find u|cant get to u|cant reach u|come get me|stuck, come`)
	reSaved   = regexp.MustCompile(`\bsaved\b`)
)

// leaderReplyKind: what kind of announcement a member's party line is ("" = nothing the leader answers).
func leaderReplyKind(text string) string {
	t := strings.ToLower(strings.TrimSpace(text))
	switch {
	case t == "":
		return ""
	case reLost.MatchString(t):
		return "lost"
	case reLeave.MatchString(t):
		return "leave"
	case reJoin.MatchString(t):
		return "" // a member joining the run: the plan line said it all
	case reTownAsk.MatchString(t):
		return "town"
	case reBack.MatchString(t):
		return "back"
	case reErrand.MatchString(t):
		return "errand"
	case reRest.MatchString(t):
		return "rest"
	case reSaved.MatchString(t):
		return "saved"
	}
	return ""
}

// leaderReply: the leader's answer to a member's announcement ("" = no answer). who is the member's name.
func leaderReply(kind, who string, c replyCtx) string {
	here := mapTalk(c.partyMap)
	switch kind {
	case "leave":
		return pick("gl "+who+"!", "cya "+who+", ty", "ty for the help, gl", "gl, cya around")
	case "town":
		if c.townRunFresh {
			return ""
		}
		if c.townRunAgo > 0 && c.townRunAgo < 45*time.Minute {
			return pick("we just went, next run in a bit. hang on "+who, "k "+who+", next town run soon",
				"noted, we go next run")
		}
		return pick("k "+who+", noted", "ok, next run", "sure, soon")
	case "errand":
		return pick("k, we stay on "+here, "ok, hurry back", "np, we keep going here", "k "+who+", we're on "+here)
	case "back":
		return pick("wb", "wb "+who, "welcome back", "wb, still on "+here)
	case "rest":
		return pick("k", "take ur time", "np, we cover u", "ok, rest up")
	case "lost":
		if c.leaderMap == "" {
			return ""
		}
		return pick(fmt.Sprintf("im on %s (%d,%d)", mapTalk(c.leaderMap), c.lx, c.ly),
			fmt.Sprintf("%s, we're on %s at %d,%d", who, mapTalk(c.leaderMap), c.lx, c.ly))
	case "saved":
		return pick("nice", "good", "ok")
	}
	return ""
}

// replyChance: how often the leader answers that kind (a person doesn't answer every line).
func replyChance(kind string) float64 {
	switch kind {
	case "lost", "leave":
		return 0.95
	case "town", "errand", "back":
		return 0.75
	case "rest":
		return 0.5
	case "saved":
		return 0.3
	}
	return 0
}

// ---- the narrator (bot-led parties) ----

type ltState struct {
	mapSeen      string
	townRunSeen  time.Time
	trOpen       bool
	trAt         time.Time
	trWent       map[string]bool
	rallyAt      time.Time
	rallyDest    string
	rallyWait    bool
	rallyMissing []string
	lastReply    time.Time
	replied      map[string]time.Time // member|kind -> last answer
}

var leaderTalk = struct {
	sync.Mutex
	m map[*Party]*ltState
}{m: map[*Party]*ltState{}}

func leaderTalkManager(stop <-chan struct{}) {
	t := time.NewTicker(4 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		if !leaderTalkOn() {
			continue
		}
		parties.Lock()
		ps := make([]*Party, 0, len(parties.byName))
		for _, p := range parties.byName {
			ps = append(ps, p)
		}
		parties.Unlock()
		now := time.Now()
		live := map[*Party]bool{}
		for _, p := range ps {
			live[p] = true
			p.leaderTalkTick(ltStateOf(p), now)
		}
		leaderTalk.Lock()
		for p := range leaderTalk.m {
			if !live[p] {
				delete(leaderTalk.m, p)
			}
		}
		leaderTalk.Unlock()
	}
}

func ltStateOf(p *Party) *ltState {
	leaderTalk.Lock()
	defer leaderTalk.Unlock()
	st := leaderTalk.m[p]
	if st == nil {
		st = &ltState{}
		leaderTalk.m[p] = st
	}
	return st
}

// ltMember: a member as the narrator sees it.
type ltMember struct {
	b      *Bot
	name   string
	m      string
	x, y   int
	online bool
	away   bool // on a town trip or in a town
	errand bool
}

// leaderTalkTick: one look at party p; the leader says what changed. Only the narrator goroutine touches st.
func (p *Party) leaderTalkTick(st *ltState, now time.Time) {
	parties.Lock()
	if p.Human != "" || p.Leader == nil {
		parties.Unlock()
		return
	}
	l, pm := p.Leader, p.Map
	trAt, trTown, trBy := p.TownRunAt, p.TownRunTown, p.TownRunBy
	r := p.Rally
	r.excluded, r.lastPos = nil, nil
	members := append([]*Bot(nil), p.Members...)
	parties.Unlock()
	first := st.mapSeen == "" && st.townRunSeen.IsZero() && st.rallyAt.IsZero()
	if first {
		// a party seen for the first time (or after a restart): what is already going on was said before
		st.mapSeen, st.townRunSeen, st.rallyAt = pm, trAt, r.At
		if pm == "" {
			st.mapSeen = "-"
		}
		return
	}
	ls, ok := peek(l.P.Name)
	if !ok || !ls.Online {
		return
	}
	var ms []ltMember
	for _, m := range members {
		if m == l {
			continue
		}
		s, ok := peek(m.P.Name)
		if !ok {
			continue
		}
		ms = append(ms, ltMember{b: m, name: s.Name, m: s.Map, x: s.X, y: s.Y, online: s.Online,
			away: s.Trip || townOf(s.Map) != "", errand: s.Errand})
	}

	// 1. a new party map: the leader's plan line (if no path said it yet) and a member's "omw"
	if pm != "" && pm != st.mapSeen {
		st.mapSeen = pm
		if !isTown(pm) {
			if !saidRecently(l, 30*time.Second, now, pm, mapTalk(pm)) {
				why := partyWhy(p)
				sayPartyDest(l, typingOf(l), p, pm, why) // partydest.go: planLine, once per map per 10 min
			}
			ackLater(ms, 1, 0.6, nil)
		}
	}

	// 2. the party's town run started: the plan (where, why, how)
	if !trAt.Equal(st.townRunSeen) {
		st.townRunSeen = trAt
		if !trAt.IsZero() && now.Sub(trAt) < 90*time.Second && trTown != "" {
			st.trOpen, st.trAt, st.trWent = true, trAt, map[string]bool{}
			line := styled(typingOf(l), townRunPlanLine(trBy, ls.Name, trTown, pm))
			say := ltSay
			go func() {
				time.Sleep(time.Duration(2+crand(3)) * ltUnit)
				say(l, line)
			}()
		}
	}
	// 3. the town run is over: everyone back, or the leader goes on without the late ones
	if st.trOpen {
		el := now.Sub(st.trAt)
		for _, m := range ms {
			if m.online && m.away {
				st.trWent[m.name] = true
			}
		}
		leaderBack := !ls.Trip && townOf(ls.Map) == "" && ls.Map != ""
		var late []string
		for _, m := range ms {
			if st.trWent[m.name] && m.online && m.away && !m.errand {
				late = append(late, m.name)
			}
		}
		switch {
		case el > 25*time.Minute:
			st.trOpen = false
		case el > 2*time.Minute && leaderBack && len(st.trWent) > 0 && len(late) == 0:
			st.trOpen = false
			sayLater(l, townRunEndLine(pm, nil), 2)
		case el > 10*time.Minute && leaderBack && len(late) > 0:
			st.trOpen = false
			sayLater(l, townRunEndLine(pm, late), 2)
			ackLater(namedOnly(ms, late), 1, 0.6, nil)
		}
	}

	// 4. a rally: who it still waits for, and where the late ones go when it gives up
	if r.Town != "" {
		if !r.At.Equal(st.rallyAt) {
			st.rallyAt, st.rallyDest, st.rallyWait, st.rallyMissing = r.At, r.Dest, false, nil
		}
		var missing []string
		online := 0
		for _, m := range ms {
			if !m.online || m.errand {
				continue
			}
			online++
			if m.m != r.Town || dist(m.x, m.y, r.X, r.Y) > togetherCells() {
				missing = append(missing, m.name)
			}
		}
		st.rallyMissing = missing
		el := now.Sub(r.At)
		if !st.rallyWait && el > 90*time.Second && len(missing) > 0 && len(missing) < 4 && ls.Map == r.Town {
			st.rallyWait = true
			sayLater(l, rallyWaitLine(missing, r.Town, rallyTimeout-el), 0)
		}
	} else if !st.rallyAt.IsZero() {
		missing, dest := st.rallyMissing, st.rallyDest
		st.rallyAt, st.rallyMissing = time.Time{}, nil
		if !strings.HasPrefix(r.Last, "everyone met") && len(missing) > 0 && len(missing) < 4 && dest != "" {
			sayLater(l, rallyMissLine(missing, dest), 3) // after endRally's "cant all meet up, lets just go"
			ackLater(namedOnly(ms, missing), 1, 0.6, nil)
		}
	}
}

// partyWhy: why the party is on its map, for the plan line ("farming Ninja Suit", "lvling").
func partyWhy(p *Party) string {
	parties.Lock()
	pur := p.Purpose
	parties.Unlock()
	if i := strings.Index(pur, " @ "); i > 0 {
		pur = pur[:i]
	}
	return pur
}

func namedOnly(ms []ltMember, names []string) []ltMember {
	var out []ltMember
	for _, m := range ms {
		for _, n := range names {
			if m.name == n {
				out = append(out, m)
			}
		}
	}
	return out
}

// sayLater: the leader says line (styled) after delay seconds, from its own goroutine.
func sayLater(l *Bot, line string, delay int) {
	typing := typingOf(l)
	say := ltSay
	go func() {
		if delay > 0 {
			time.Sleep(time.Duration(delay+crand(3)) * ltUnit)
		}
		say(l, styled(typing, line))
	}()
}

// ackLater: up to n online members answer the leader's plan briefly, each with chance pr.
func ackLater(ms []ltMember, n int, pr float64, line func() string) {
	if line == nil {
		line = memberAckLine
	}
	said := 0
	for _, i := range permIdx(len(ms)) {
		if said >= n {
			return
		}
		m := ms[i]
		if !m.online || ltRand() >= pr {
			continue
		}
		said++
		typing, text := typingOf(m.b), line()
		say := ltSay
		go func(b *Bot, d int) {
			time.Sleep(time.Duration(d) * ltUnit)
			say(b, styled(typing, text))
		}(m.b, 5+crand(6)+said*3)
	}
}

func permIdx(n int) []int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	for i := n - 1; i > 0; i-- {
		j := crand(i + 1)
		idx[i], idx[j] = idx[j], idx[i]
	}
	return idx
}

// ---- PT26: the leader answers a member's announcement (party chat hook, bot.go 0x0109) ----

// leaderHeard: bot b heard party line raw ("Name : text"). Only a bot party's leader answers.
func leaderHeard(b *Bot, raw string) {
	if !leaderTalkOn() {
		return
	}
	i := strings.Index(raw, " : ")
	if i <= 0 {
		return
	}
	who, text := raw[:i], raw[i+3:]
	p := partyOf(b)
	if p == nil {
		return
	}
	parties.Lock()
	if p.Human != "" || p.Leader != b {
		parties.Unlock()
		return
	}
	pm, trAt := p.Map, p.TownRunAt
	var member *Bot
	for _, m := range p.Members {
		if m != b && m.charName() == who {
			member = m
		}
	}
	parties.Unlock()
	if member == nil || who == b.charName() {
		return
	}
	kind := leaderReplyKind(text)
	if kind == "" || ltRand() >= replyChance(kind) {
		return
	}
	now := time.Now()
	st := ltStateOf(p)
	leaderTalk.Lock()
	if st.replied == nil {
		st.replied = map[string]time.Time{}
	}
	k := who + "|" + kind
	if now.Sub(st.lastReply) < 8*time.Second || now.Sub(st.replied[k]) < 2*time.Minute {
		leaderTalk.Unlock()
		return
	}
	st.lastReply, st.replied[k] = now, now
	leaderTalk.Unlock()
	c := replyCtx{partyMap: pm, townRunFresh: !trAt.IsZero() && now.Sub(trAt) < time.Minute}
	if !trAt.IsZero() {
		c.townRunAgo = now.Sub(trAt)
	}
	if ls, ok := peek(b.P.Name); ok {
		c.leaderMap, c.lx, c.ly = ls.Map, ls.X, ls.Y
	}
	line := leaderReply(kind, who, c)
	if line == "" {
		return
	}
	typing := typingOf(b)
	time.Sleep(time.Duration(2+crand(4)) * ltUnit)
	ltSay(b, styled(typing, line))
}

// ---- hooks called with b.mu held ----

// dayEndSay (b.mu held): b leaves for the day (startTrip "calling it a day"): it tells its party.
func (b *Bot) dayEndSay(p *Party) {
	if !leaderTalkOn() || p == nil {
		return
	}
	parties.Lock()
	leader, pm, human := p.Leader == b, p.Map, p.Human
	parties.Unlock()
	if human != "" && leader {
		return
	}
	line := styled(b.P.Typing, dayEndLine(leader, pm))
	go ltSay(b, line)
}

// leaderTalkSkipTrip (b.mu held): the bot leader joining its party's town run doesn't say a member's join line - its
// plan line (leaderTalkTick) says where and how.
func (b *Bot) leaderTalkSkipTrip(why string) bool {
	if !leaderTalkOn() || !(strings.HasPrefix(why, "town run") || why == "with the party") {
		return false
	}
	p := partyOf(b)
	if p == nil {
		return false
	}
	parties.Lock()
	defer parties.Unlock()
	return p.Human == "" && p.Leader == b && !p.TownRunAt.IsZero() && time.Since(p.TownRunAt) < 3*time.Minute
}
