package main

// Ayla's party rotation (AY15, owner 2026-11-15: rotate members to test other jobs and builds; the priest stays, every
// role stays covered, a member is swapped for another character of a fitting level, never by resetting a build - G20).
//
// Every ayla_rotation_hours (tuning, 0 = 3; ayla_rotation_off stops it) the instance owning human-parties.json (core):
//   - waits until the party is calm: no rally, no town run (engine or her status.json), nobody fighting, she is online;
//   - picks ONE member to rotate out: not the healer (Priest / Acolyte), not the in-game party leader (it invites),
//     the longest in the party first;
//   - picks the member to swap in from the main instance's online bots (data/rotation_candidates.json, written by the
//     main instance every 2 min): same role (tank / melee / ranged / caster / support), base level within 5 of the
//     party's average (Ayla included), a class/build not in her party in the last 7 days, never a character that was
//     in her party in the last 24 h, not a guild leader, trader or brewer, not pinned; a Starveil member first, else
//     a guildless bot (Ayla's own recruiting invites party friends first: humanguild.go guild-recruits);
//   - moves the new member's account to the core instance and the old one's to the main instance (handoff.go: no
//     container recreate), edits the party roster in place (no disband: the others stay in the in-game party), writes
//     human-parties.json (old member -> alumni: says goodbye in party chat, leaves, stays in Starveil);
//   - the new member says hi in party chat once it is in the in-game party, a partymate welcomes it;
//   - logs the swap in data/ayla_rotation.json and appends an info item to data/bugqueue.jsonl for per-class QA.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	rotTestedWindow = 7 * 24 * time.Hour
	rotRejoinGap    = 24 * time.Hour
	rotLevelBand    = 5
)

type rotSwap struct {
	At       int64  `json:"at"`
	Human    string `json:"human"`
	Out      string `json:"out"`
	OutClass string `json:"out_class"`
	OutBuild string `json:"out_build"`
	In       string `json:"in"`
	InClass  string `json:"in_class"`
	InBuild  string `json:"in_build"`
	Role     string `json:"role"`
	InLv     int    `json:"in_lv"`
	PartyLv  int    `json:"party_lv"`
	Starveil bool   `json:"starveil"`
	Recruit  bool   `json:"recruit,omitempty"` // guildless: Ayla's recruiting invites it
}

type rotLog struct {
	LastAt int64            `json:"last_at"` // the last swap (or when rotation started)
	Joined map[string]int64 `json:"joined"`  // lower-case char -> joined her party
	Left   map[string]int64 `json:"left"`    // lower-case char -> left her party
	Tested map[string]int64 `json:"tested"`  // "Class" and "Class/build" -> last time in her party
	Greet  map[string]int64 `json:"greet,omitempty"`
	Note   string           `json:"note,omitempty"` // why the last due rotation didn't happen
	Swaps  []rotSwap        `json:"swaps"`
}

type rotCand struct {
	Char    string `json:"char"`
	Account string `json:"account"`
	Class   int    `json:"class"`
	Build   string `json:"build"`
	Lv      int    `json:"lv"`
	Guild   string `json:"guild,omitempty"`
	GuildID int    `json:"guild_id,omitempty"`
	Online  bool   `json:"online"`
	Pinned  bool   `json:"pinned,omitempty"`
	Leader  bool   `json:"leader,omitempty"`
	Trader  bool   `json:"trader,omitempty"`
	Brewer  bool   `json:"brewer,omitempty"`
}

type rotSnapshot struct {
	At   int64     `json:"at"`
	Bots []rotCand `json:"bots"`
}

type rotMember struct {
	Char   string
	Class  int
	Build  string
	Lv     int
	Leader bool // in-game party leader
}

func rotLogPath() string  { return filepath.Join(*dataDir, "ayla_rotation.json") }
func rotCandPath() string { return filepath.Join(*dataDir, "rotation_candidates.json") }

func rotationHours() float64 {
	if h := tune().AylaRotationHours; h > 0 {
		return h
	}
	return 3
}

// rotRole: the party role a class fills ("healer" = never rotated, "" = not a party role).
func rotRole(class int) string {
	c := class
	if b, ok := transToSecond[c]; ok {
		c = b
	}
	switch c {
	case 4002:
		c = 1
	case 4003:
		c = 2
	case 4004:
		c = 3
	case 4005:
		c = 4
	case 4006:
		c = 5
	case 4007:
		c = 6
	}
	switch c {
	case 8, 4:
		return "healer"
	case 1, 14:
		return "tank"
	case 7, 12, 17, 10, 6, 5, 23:
		return "melee"
	case 11, 3:
		return "ranged"
	case 9, 16, 2:
		return "caster"
	case 15, 19, 20, 18:
		return "support"
	}
	return ""
}

// rotKey / rotClassKey: lower-case keys of rotLog.Tested ("paladin/paladin", "paladin").
func rotKey(class int, build string) string { return strings.ToLower(className(class) + "/" + build) }
func rotClassKey(class int) string          { return strings.ToLower(className(class)) }

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// pickRotation (pure): who leaves, who joins. ok=false with why when no swap fits.
func pickRotation(members []rotMember, cands []rotCand, lg *rotLog, partyLv int, guild string, now time.Time) (rotMember, rotCand, bool, string) {
	recent := func(m map[string]int64, k string, d time.Duration) bool {
		t := m[strings.ToLower(k)]
		return t > 0 && now.Sub(time.Unix(t, 0)) < d
	}
	inParty, partyKeys := map[string]bool{}, map[string]bool{}
	var outs []rotMember
	for _, m := range members {
		inParty[strings.ToLower(m.Char)] = true
		partyKeys[rotKey(m.Class, m.Build)] = true
		if r := rotRole(m.Class); r != "" && r != "healer" && !m.Leader {
			outs = append(outs, m)
		}
	}
	if len(outs) == 0 {
		return rotMember{}, rotCand{}, false, "no member can rotate (healer / party leader only)"
	}
	sort.SliceStable(outs, func(i, j int) bool {
		a, b := lg.Joined[strings.ToLower(outs[i].Char)], lg.Joined[strings.ToLower(outs[j].Char)]
		if a != b {
			return a < b // the longest in the party first (unknown = since the start)
		}
		return outs[i].Char < outs[j].Char
	})
	why := "no candidate"
	for _, out := range outs {
		role := rotRole(out.Class)
		var ok []rotCand
		for _, c := range cands {
			switch {
			case !c.Online || c.Pinned || c.Leader || c.Trader || c.Brewer || c.Char == "" || c.Account == "":
			case rotRole(c.Class) != role:
			case !swapKeepsComposition(out.Class, c.Class): // AY11: the swap keeps 2 frontline / 2 ranged / 1 support
			case absInt(c.Lv-partyLv) > rotLevelBand:
			case inParty[strings.ToLower(c.Char)] || recent(lg.Joined, c.Char, rotRejoinGap) || recent(lg.Left, c.Char, rotRejoinGap):
			case partyKeys[rotKey(c.Class, c.Build)] || recent(lg.Tested, rotKey(c.Class, c.Build), rotTestedWindow):
			case !(guild != "" && strings.EqualFold(c.Guild, guild)) && (c.GuildID != 0 || c.Guild != ""):
				// in another guild: not ours to take
			default:
				ok = append(ok, c)
			}
		}
		if len(ok) == 0 {
			why = fmt.Sprintf("no online %s within %d levels of %d with an untested class/build", role, rotLevelBand, partyLv)
			continue
		}
		sort.SliceStable(ok, func(i, j int) bool {
			a, b := ok[i], ok[j]
			if x, y := recent(lg.Tested, rotClassKey(a.Class), rotTestedWindow), recent(lg.Tested, rotClassKey(b.Class), rotTestedWindow); x != y {
				return !x // a class not tested lately before another build of a tested class
			}
			if x, y := strings.EqualFold(a.Guild, guild), strings.EqualFold(b.Guild, guild); x != y {
				return x // Starveil first
			}
			if x, y := absInt(a.Lv-partyLv), absInt(b.Lv-partyLv); x != y {
				return x < y
			}
			return a.Char < b.Char
		})
		return out, ok[0], true, ""
	}
	return rotMember{}, rotCand{}, false, why
}

// writeRotationCandidates (main instance, every 2 min): the online bots a rotation may take.
func writeRotationCandidates(bots []*Bot, now time.Time) {
	snap := rotSnapshot{At: now.Unix()}
	for _, b := range bots {
		b.mu.Lock()
		if !b.Online || b.MapName == "" {
			b.mu.Unlock()
			continue
		}
		cs := b.charSpec()
		build := cs.Plan
		if build == "" {
			build = cs.Build
		}
		c := rotCand{Char: b.charName(), Account: b.P.Account, Class: b.Class, Build: build, Lv: b.BaseLvOr(),
			Guild: b.GuildName, GuildID: int(b.GuildID), Online: true, Pinned: b.pin != nil,
			Leader: b.leaderVision() != nil, Trader: b.isVendorTrader() || b.isHumanGuildTrader(now)}
		b.mu.Unlock()
		c.Brewer = b.guildBrewerChar() != ""
		snap.Bots = append(snap.Bots, c)
	}
	if err := writeJSONFile(rotCandPath(), snap); err != nil {
		log.Printf("rotation: candidates: %v", err)
	}
}

func loadRotLog() *rotLog {
	lg := &rotLog{}
	readJSONFile(rotLogPath(), lg)
	if lg.Joined == nil {
		lg.Joined = map[string]int64{}
	}
	if lg.Left == nil {
		lg.Left = map[string]int64{}
	}
	if lg.Tested == nil {
		lg.Tested = map[string]int64{}
	}
	if lg.Greet == nil {
		lg.Greet = map[string]int64{}
	}
	return lg
}

func humanPartyNow() *Party {
	parties.Lock()
	defer parties.Unlock()
	for _, p := range parties.byName {
		if p.Human != "" {
			return p
		}
	}
	return nil
}

// rotPartyBusy: why the party can't swap a member right now ("" = calm).
func rotPartyBusy(p *Party, now time.Time) string {
	if p.rallyInfo().Town != "" {
		return "rally"
	}
	if at, _, _ := p.townRun(); !at.IsZero() && now.Sub(at) < 10*time.Minute {
		return "town run"
	}
	st, ok := humanStatusOf(p.Human)
	if !ok || !st.Online {
		return p.Human + " offline"
	}
	if st.TownRun {
		return "town run"
	}
	if st.MaxHP > 0 && st.HP*10 < st.MaxHP*7 {
		return "combat (" + p.Human + " hurt)"
	}
	for _, m := range p.Members {
		m.mu.Lock()
		fight := m.Online && m.underAttack(now)
		m.mu.Unlock()
		if fight {
			return "combat"
		}
	}
	return ""
}

func rotSave(lg *rotLog) {
	if err := writeJSONFile(rotLogPath(), lg); err != nil {
		log.Printf("rotation: log: %v", err)
	}
}

// rotationTick (handoff.go handoffManager, every minute, the instance owning human-parties.json).
func rotationTick(all []*Persona, now time.Time) {
	p := humanPartyNow()
	if p == nil {
		return
	}
	lg := loadRotLog()
	rotGreet(p, lg, now)
	if tune().AylaRotationOff {
		return
	}
	if lg.LastAt == 0 { // first run: the clock starts now, today's members and alumni count as recent
		lg.LastAt = now.Unix()
		for _, m := range p.Members {
			m.mu.Lock()
			cs := m.charSpec()
			k, cls := rotKey(m.Class, firstNonEmpty(cs.Plan, cs.Build)), rotClassKey(m.Class)
			m.mu.Unlock()
			lg.Tested[k], lg.Tested[cls] = now.Unix(), now.Unix()
		}
		for _, s := range savedRoster() {
			for _, a := range s.Alumni {
				lg.Left[strings.ToLower(a)] = now.Unix()
			}
		}
		rotSave(lg)
		return
	}
	if now.Sub(time.Unix(lg.LastAt, 0)) < time.Duration(rotationHours()*float64(time.Hour)) {
		return
	}
	note := func(s string) {
		if lg.Note != s {
			lg.Note = s
			rotSave(lg)
			log.Printf("rotation: due, not now: %s", s)
		}
	}
	if why := rotPartyBusy(p, now); why != "" {
		note("waiting: " + why)
		return
	}
	// members and the party level (Ayla included)
	var ms []rotMember
	byChar := map[string]*Bot{}
	sum, n := 0, 0
	for _, m := range p.Members {
		m.mu.Lock()
		cs := m.charSpec()
		rm := rotMember{Char: m.charName(), Class: m.Class, Build: firstNonEmpty(cs.Plan, cs.Build), Lv: m.BaseLvOr(),
			Leader: m.PartyID != 0 && strings.EqualFold(m.partyLeader, m.charName())}
		m.mu.Unlock()
		ms = append(ms, rm)
		byChar[strings.ToLower(rm.Char)] = m
		if rm.Lv > 0 {
			sum, n = sum+rm.Lv, n+1
		}
	}
	if st, ok := humanStatusOf(p.Human); ok && st.BaseLevel > 0 {
		sum, n = sum+st.BaseLevel, n+1
	}
	if n == 0 {
		return
	}
	partyLv := (sum + n/2) / n
	var cands []rotCand
	if handoffActive() {
		var snap rotSnapshot
		readJSONFile(rotCandPath(), &snap)
		if now.Sub(time.Unix(snap.At, 0)) > 5*time.Minute {
			note("waiting: no fresh candidate list from the main instance")
			return
		}
		cands = snap.Bots
	} else {
		writeRotationCandidates(allInstanceBots(), now)
		var snap rotSnapshot
		readJSONFile(rotCandPath(), &snap)
		cands = snap.Bots
	}
	guild := humanGuildInfo().Guild
	if guild == "" {
		guild = "Starveil"
	}
	out, in, ok, why := pickRotation(ms, cands, lg, partyLv, guild, now)
	if !ok {
		lg.LastAt = now.Add(-time.Duration(rotationHours()*float64(time.Hour)) + 30*time.Minute).Unix() // retry in 30 min
		lg.Note = why
		rotSave(lg)
		log.Printf("rotation: no swap: %s", why)
		return
	}
	var inP *Persona
	for _, pp := range all {
		if pp.Account == in.Account {
			inP = pp
		}
	}
	outBot := byChar[strings.ToLower(out.Char)]
	if inP == nil || outBot == nil {
		note("candidate " + in.Char + " not found in personas")
		return
	}
	if handoffActive() {
		handoffAssign(in.Char, out.Char, now)
	}
	inBot := instanceBotFor(inP)
	rotApplySwap(p, outBot, inBot, in.Char, now)
	sw := rotSwap{At: now.Unix(), Human: p.Human, Out: out.Char, OutClass: className(out.Class), OutBuild: out.Build,
		In: in.Char, InClass: className(in.Class), InBuild: in.Build, Role: rotRole(in.Class), InLv: in.Lv, PartyLv: partyLv,
		Starveil: strings.EqualFold(in.Guild, guild), Recruit: in.Guild == "" && in.GuildID == 0}
	lg.LastAt, lg.Note = now.Unix(), ""
	lg.Joined[strings.ToLower(in.Char)] = now.Unix()
	lg.Left[strings.ToLower(out.Char)] = now.Unix()
	delete(lg.Joined, strings.ToLower(out.Char))
	lg.Tested[rotKey(in.Class, in.Build)], lg.Tested[rotClassKey(in.Class)] = now.Unix(), now.Unix()
	lg.Greet[strings.ToLower(in.Char)] = now.Unix()
	lg.Swaps = append(lg.Swaps, sw)
	rotSave(lg)
	rotReport(sw)
	log.Printf("rotation: %s (%s/%s) out, %s (%s/%s lv%d) in for %s's party", sw.Out, sw.OutClass, sw.OutBuild, sw.In, sw.InClass, sw.InBuild, sw.InLv, sw.Human)
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// rotApplySwap: the roster edited in place (startHumanParty would disband: every member would leave the in-game party).
func rotApplySwap(p *Party, out, in *Bot, inChar string, now time.Time) {
	parties.Lock()
	ms := make([]*Bot, 0, len(p.Members))
	for _, m := range p.Members {
		if m != out {
			ms = append(ms, m)
		}
	}
	ms = append(ms, in)
	p.Members = ms
	if p.Leader == out {
		p.Leader = ms[0]
	}
	delete(parties.ofBot, out)
	if old := parties.ofBot[in]; old != nil && old != p {
		parties.Unlock()
		disband(old) // an engine party the new member had in this instance (single-instance mode)
		parties.Lock()
	}
	parties.ofBot[in] = p
	parties.Unlock()
	markPartiesDirty()
	human := p.Human
	out.mu.Lock()
	out.pin = nil
	out.mu.Unlock()
	in.mu.Lock()
	in.pin = &Pin{Char: inChar, Human: human}
	in.mu.Unlock()
	select {
	case in.wake <- struct{}{}:
	default:
	}
	mem := memoryOf(in.P)
	mem.meet(human, "partymate", "", true)
	for _, m := range ms {
		if m != in {
			mem.meet(m.charName(), "partymate", "", false)
		}
	}
	mem.add("joined " + human + "'s party (guild friends asked me along)")
	out.event("party", "", "rotated out of "+p.Name+" (AY15): plays on its own now, stays in the guild")
	rotRosterFile(human, out.charName(), inChar)
}

// rotRosterFile: human-parties.json alumni += out, -= in; then the members are saved from the engine.
func rotRosterFile(human, out, in string) {
	var saved []savedHumanParty
	readJSONFile(humanPartyFile(), &saved)
	for i := range saved {
		if !strings.EqualFold(saved[i].Human, human) {
			continue
		}
		var al []string
		for _, a := range saved[i].Alumni {
			if !strings.EqualFold(a, in) && !strings.EqualFold(a, out) {
				al = append(al, a)
			}
		}
		saved[i].Alumni = append(al, out)
	}
	writeJSONFile(humanPartyFile(), saved)
	saveHumanParties() // members from the engine, alumni kept (withAlumni)
	hpRoster.Lock()
	hpRoster.at = time.Time{} // alumnusIn sees the change now
	hpRoster.Unlock()
}

// rotGreet: the new member says hi once it is in the in-game party; a partymate welcomes it.
func rotGreet(p *Party, lg *rotLog, now time.Time) {
	if len(lg.Greet) == 0 {
		return
	}
	changed := false
	for k, at := range lg.Greet {
		if now.Sub(time.Unix(at, 0)) > 30*time.Minute {
			delete(lg.Greet, k)
			changed = true
			continue
		}
		var nb, other *Bot
		for _, m := range p.Members {
			m.mu.Lock()
			in := m.Online && m.PartyID != 0 && strings.EqualFold(m.PartyName, p.Name)
			name := strings.ToLower(m.charName())
			m.mu.Unlock()
			if !in {
				continue
			}
			if name == k {
				nb = m
			} else if other == nil {
				other = m
			}
		}
		if nb == nil {
			continue
		}
		delete(lg.Greet, k)
		changed = true
		nt, newName := nb.P.Typing, nb.charName()
		go nb.partySay(styled(nt, pick("hi all, thx for the invite", "hey guys o/ happy to join", "yo, thx for having me")))
		if other != nil {
			ot := other.P.Typing
			go func() {
				time.Sleep(4 * time.Second)
				other.partySay(styled(ot, pick("welcome "+newName, "wb "+newName+" o/", "hey "+newName+", welcome")))
			}()
		}
	}
	if changed {
		rotSave(lg)
	}
}

// rotReport: an info item in the bug queue, so QA can file follow-ups for the new class.
func rotReport(sw rotSwap) {
	guild := "Starveil member"
	if sw.Recruit {
		guild = "guildless, to be recruited into Starveil"
	}
	row := map[string]any{
		"id": fmt.Sprintf("rot%d-%s", sw.At, strings.ReplaceAll(strings.ToLower(sw.In), " ", "_")), "t": sw.At,
		"from": "ayla-rotation", "priority": "low", "kind": "info", "status": "open", "metric": "kpm",
		"key": "ayla-rotation-" + strings.ToLower(sw.InClass),
		"text": fmt.Sprintf("INFO AY15 rotation: %s (%s/%s, %s) left %s's party, %s (%s/%s lv%d, %s) joined (party lv %d). "+
			"QA: watch %s in her party for 2 h (role play, kills/min, deaths, heals/buffs, gear) and file per-class follow-ups.",
			sw.Out, sw.OutClass, sw.OutBuild, sw.Role, sw.Human, sw.In, sw.InClass, sw.InBuild, sw.InLv, guild, sw.PartyLv, sw.In),
	}
	f, err := os.OpenFile(filepath.Join(*dataDir, "bugqueue.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("rotation: bug queue: %v", err)
		return
	}
	defer f.Close()
	b, _ := json.Marshal(row)
	f.Write(append(b, '\n'))
}
