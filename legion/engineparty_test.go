package main

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// engineMk: a bot whose persona has the given characters (the first one is its main).
func engineMk(acc int, chars ...string) *Bot {
	p := &Persona{Account: "engpers" + chars[0], AccountID: 900500 + acc, Name: chars[0]}
	for _, c := range chars {
		p.Chars = append(p.Chars, CharSpec{Name: c, Role: "main"})
	}
	return NewBot(p)
}

// engineClean: forget the parties a test formed.
func engineClean(t *testing.T, ps ...*Party) {
	t.Helper()
	t.Cleanup(func() {
		parties.Lock()
		for _, p := range ps {
			if p == nil {
				continue
			}
			delete(parties.byName, p.Name)
			for _, m := range p.Members {
				delete(parties.ofBot, m)
			}
		}
		parties.Unlock()
		partySave.Lock()
		partySave.last = nil
		partySave.Unlock()
	})
}

func engineNames(p *Party) string {
	var ns []string
	for _, m := range p.Members {
		ns = append(ns, m.charName())
	}
	return strings.Join(ns, ",")
}

// TestEngineSwitchOffKeepsHumanParty: with engine_parties empty a human's party is human-led, exactly as before.
func TestEngineSwitchOffKeepsHumanParty(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "" })
	if enginePartyOn("Ayla") {
		t.Fatal("engine party on with the switch off")
	}
	a, b, c := engineMk(11, "AylaOffA"), engineMk(12, "OffZen"), engineMk(13, "OffVex")
	p := startHumanParty("AylaOffA", []*Bot{a, b, c}, []string{"AylaOffA", "OffZen", "OffVex"}, 0)
	engineClean(t, p)
	if p == nil || p.Human != "AylaOffA" || p.Engine || p.Leader != a {
		t.Fatalf("switch off: want the human-led party, got %+v", p)
	}
	if !persistsBotParties() {
		t.Fatal("main instance must keep persisting bot parties")
	}
}

// TestEngineSwitchOnFormsBotLedParty: with the switch on the human's bot leads a bot-led party (no p.Human), its
// pin holds the character, and the roster is the three members.
func TestEngineSwitchOnFormsBotLedParty(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaOnA" })
	a, b, c := engineMk(21, "AylaOnA"), engineMk(22, "OnZen"), engineMk(23, "OnVex")
	p := startHumanParty("AylaOnA", []*Bot{a, b, c}, []string{"AylaOnA", "OnZen", "OnVex"}, 0)
	engineClean(t, p)
	if p == nil {
		t.Fatal("engine party not formed")
	}
	if p.Human != "" || !p.Engine || p.Leader != a {
		t.Fatalf("want a bot-led party led by the human's bot: human=%q engine=%v leader=%v", p.Human, p.Engine, p.Leader)
	}
	if got := engineNames(p); got != "AylaOnA,OnZen,OnVex" {
		t.Fatalf("roster %q", got)
	}
	a.mu.Lock()
	pinChar := a.pin.Char
	a.mu.Unlock()
	if pinChar != "AylaOnA" {
		t.Fatalf("leader pinned to %q", pinChar)
	}
	if partyOf(b) != p || partyOf(c) != p {
		t.Fatal("members not in the party")
	}
}

// TestEngineSurvivesRestart: the core instance saves its engine party (and only that), a restart restores it with the
// same leader and roster, and a non-engine party of the same instance is not saved.
func TestEngineSurvivesRestart(t *testing.T) {
	oldOnly, oldDir := *onlyNames, *dataDir
	*onlyNames, *dataDir = "AylaRS", t.TempDir()
	defer func() { *onlyNames, *dataDir = oldOnly, oldDir }()
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaRS" })

	a, b, c := engineMk(31, "AylaRS"), engineMk(32, "RSZen"), engineMk(33, "RSVex")
	d, e := engineMk(34, "RSLoner"), engineMk(35, "RSOther")
	ep := startHumanParty("AylaRS", []*Bot{a, b, c}, []string{"AylaRS", "RSZen", "RSVex"}, 0)
	op := formPartyOpt(d, []*Bot{e}, "prontera", false, nil) // a core party that is not an engine party
	engineClean(t, ep, op)
	if ep == nil || op == nil {
		t.Fatal("parties not formed")
	}
	snap := snapshotBotParties()
	if len(snap) != 1 || !snap[0].Engine || snap[0].Leader != "AylaRS" || len(snap[0].Members) != 3 {
		t.Fatalf("core snapshot should hold only the engine party: %+v", snap)
	}
	if err := saveBotParties(botPartyFile(), snap); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(botPartyFile(), "parties_engine.json") {
		t.Fatalf("core instance must save to parties_engine.json, not %s", botPartyFile())
	}
	// restart: the parties are gone from memory, the bots log in again
	disband(ep)
	disband(op)
	a.pin, b.pin, c.pin = nil, nil, nil
	restoreBotParties([]*Bot{a, b, c, d, e})
	p := partyOf(a)
	if p == nil || p.Leader != a || !p.Engine || p.Human != "" || len(p.Members) != 3 {
		t.Fatalf("engine party not restored: %+v", p)
	}
	if got := engineNames(p); got != "AylaRS,RSZen,RSVex" {
		t.Fatalf("restored roster %q", got)
	}
	engineClean(t, p)
	defer endRestore(p, &partyRestore{chars: map[*Bot]string{a: "", b: "", c: ""}})
	if partyOf(d) != nil {
		t.Fatal("the non-engine core party was restored")
	}
}

// TestSeedLeavesAccountOutsideBotRange: a persona outside the bot account range (Ayla, 2000000) is never inserted and
// never touched by a re-seed (G06): the seed writes no statement for it.
func TestSeedLeavesAccountOutsideBotRange(t *testing.T) {
	ayla := &Persona{Account: "player", AccountID: 2000000, Name: "Ayla"}
	bot := &Persona{Account: "lg0001", AccountID: 2100001, Name: "Engbot"}
	sql := seedSQL([]*Persona{ayla, bot}, rand.New(rand.NewSource(1)))
	if strings.Contains(sql, "2000000") || strings.Contains(sql, "'player'") {
		t.Fatal("the seed writes to Ayla's account 2000000")
	}
	if !strings.Contains(sql, "'lg0001'") {
		t.Fatal("the bot account is not seeded")
	}
}

// TestEngineMembersGetGoals: the API path (startEnginePartyFor) pins the members, and goalTick must still give them
// goals: their pin is their party's, like a human party's; a pinned bot in no party stays skipped.
func TestEngineMembersGetGoals(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaGoal" })
	a, b, c := engineMk(41, "AylaGoal"), engineMk(42, "GoalZen"), engineMk(43, "GoalVex")
	p := startEnginePartyFor("AylaGoal", []*Bot{a, b, c}, []string{"AylaGoal", "GoalZen", "GoalVex"}, 0)
	engineClean(t, p)
	if p == nil {
		t.Fatal("engine party not formed through startEnginePartyFor")
	}
	for _, m := range []*Bot{a, b, c} {
		if goalPinSkips(m) {
			t.Errorf("engine party member %s is skipped by goalTick", m.charName())
		}
	}
	loner := engineMk(44, "GoalLoner")
	loner.pin = &Pin{Char: "GoalLoner"}
	if !goalPinSkips(loner) {
		t.Error("a pinned bot in no party must still be skipped")
	}
}

// TestSyncBotPartyHandsLeadToEngineLeader: the in-game party was founded by a member (Zen.fox), so the engine's
// leader (Ayla) does not re-found it: the member is asked to hand the lead over (acted is stamped for it), and Ayla is
// not sent to leave or re-create the party.
func TestSyncBotPartyHandsLeadToEngineLeader(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "" })
	lead, zen := engineMk(51, "HandAyla"), engineMk(52, "HandZen")
	p := &Party{Name: "Ayla pt", Leader: lead, Members: []*Bot{lead, zen}, Formed: time.Now().Add(-time.Hour), Engine: true}
	parties.Lock()
	parties.byName[p.Name] = p
	parties.ofBot[lead], parties.ofBot[zen] = p, p
	parties.Unlock()
	engineClean(t, p)
	now := time.Now()
	setView := func(b *Bot, pid uint32, pname, leader string, aid uint32) {
		b.mu.Lock()
		b.Online, b.MapName = true, "pay_dun03"
		b.PartyID, b.PartyName, b.partyLeader, b.aid = pid, pname, leader, aid
		b.mu.Unlock()
	}
	setView(lead, 2, "Ayla pt", "HandZen", 2000000)  // in the party, led by Zen.fox
	setView(zen, 2, "Ayla pt", "HandZen", 2100003)  // the in-game leader
	st := &partySync{invited: map[*Bot]time.Time{}, acted: map[*Bot]time.Time{}}
	syncBotParty(p, st, nil, now)
	if st.acted[zen].IsZero() {
		t.Fatal("the in-game leader was not asked to hand the lead to Ayla")
	}
	if !st.acted[lead].IsZero() {
		t.Fatal("Ayla left or re-founded the party while the hand-over was pending")
	}
	if !st.createAt.IsZero() || lead.PartyName != "Ayla pt" {
		t.Fatalf("Ayla re-founded the party: createAt=%v name=%q", st.createAt, lead.PartyName)
	}
}
