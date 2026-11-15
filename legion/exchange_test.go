package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// withTuning publishes a tuning for one test and restores the previous one afterwards.
func withTuning(t *testing.T, f func(tn *Tuning)) {
	t.Helper()
	old := curTuning.Load()
	tn := defaultTuning
	f(&tn)
	curTuning.Store(&tn)
	t.Cleanup(func() {
		if old != nil {
			curTuning.Store(old)
		} else {
			curTuning.Store(&defaultTuning)
		}
	})
}

func resetExch() {
	exch.Lock()
	exch.rel, exch.friend, exch.spare, exch.market = map[string]exRel{}, map[string]bool{}, map[string]map[int]bool{}, map[int]int{}
	exch.favours, exch.asks, exch.askedAt = map[string][]time.Time{}, map[string]exAskRec{}, map[string]time.Time{}
	exch.chosen, exch.counts, exch.recent = map[string]*exChoice{}, map[string]int64{}, nil
	exch.Unlock()
}

func TestExTier(t *testing.T) {
	cases := []struct {
		rel   int
		spare bool
		each  int
		tier  string
	}{
		{relParty, false, 0, tierParty},
		{relParty, true, 0, tierParty},
		{relGuild, true, 0, tierKinFree},
		{relFriend, true, 0, tierKinFree},
		{relGuild, false, 2500, tierKin},
		{relFriend, false, 2500, tierKin},
		{relStranger, true, 10000, tierMarket},
		{relStranger, false, 10000, tierMarket},
	}
	for _, c := range cases {
		each, tier := exTier(c.rel, c.spare, 10000, 25)
		if each != c.each || tier != c.tier {
			t.Errorf("exTier(%s, spare %v) = %d %s, want %d %s", relNames[c.rel], c.spare, each, tier, c.each, c.tier)
		}
	}
	if each, _ := exTier(relGuild, false, 3, 25); each != 1 {
		t.Errorf("a kin price never rounds to 0 for a paid item: got %d", each)
	}
	if each, _ := exTier(relGuild, false, 1234, 25); each != roundPrice(308) {
		t.Errorf("kin price rounding: got %d", each)
	}
}

func TestExSpare(t *testing.T) {
	if !exSpare(2, true) {
		t.Error("a duplicate is a spare even if on the build table")
	}
	if !exSpare(1, false) {
		t.Error("an item off the build table is a spare")
	}
	if exSpare(1, true) {
		t.Error("the only copy of a build item is not a spare")
	}
	table := map[string][]gearBand{"knight": {
		{Min: 1, Max: 20, Slots: map[string][]gearEntry{"weapon": {{ID: 1116}}}},
		{Min: 21, Max: 40, Slots: map[string][]gearEntry{"weapon": {{ID: 1158}}}, Cards: []gearEntry{{ID: 4035}}},
	}}
	if !exOnTable(table, "knight", 10, 1158) {
		t.Error("gear of a later band is on the build table")
	}
	if !exOnTable(table, "knight", 25, 4035) {
		t.Error("a card of the band is on the build table")
	}
	if exOnTable(table, "knight", 30, 1116) {
		t.Error("gear of an earlier band only is not on the table any more")
	}
	if exOnTable(table, "wizard", 10, 1116) {
		t.Error("another build's table")
	}
}

func TestExRelation(t *testing.T) {
	if exRelation("p1", "p1", "g1", "g2", false) != relParty {
		t.Error("same party")
	}
	if exRelation("p1", "p2", "g1", "g1", true) != relGuild {
		t.Error("guild beats friend")
	}
	if exRelation("", "", "", "", true) != relFriend {
		t.Error("friend")
	}
	if exRelation("", "", "", "", false) != relStranger {
		t.Error("empty party / guild names never match")
	}
}

func TestExAskOrder(t *testing.T) {
	st := 3 * time.Minute
	type c struct {
		age          time.Duration
		party, guild bool
		ch           string
		idx          int
	}
	for _, x := range []c{
		{0, true, true, "party", 0},
		{st, true, true, "guild", 1},
		{2 * st, true, true, "#trade", 2},
		{10 * st, true, true, "#trade", 2},
		{0, false, true, "guild", 1},
		{st, false, true, "#trade", 2},
		{0, true, false, "party", 0},
		{st, true, false, "#trade", 2},
		{0, false, false, "#trade", 2},
	} {
		ch, idx := exAskChannel(x.age, st, x.party, x.guild)
		if ch != x.ch || idx != x.idx {
			t.Errorf("age %v party %v guild %v: %s/%d, want %s/%d", x.age, x.party, x.guild, ch, idx, x.ch, x.idx)
		}
	}
	if exSellerAllowed(0, relGuild) || !exSellerAllowed(0, relParty) {
		t.Error("party stage: party members only")
	}
	if !exSellerAllowed(1, relFriend) || exSellerAllowed(1, relStranger) {
		t.Error("guild stage: party, guild, friends")
	}
	if !exSellerAllowed(2, relStranger) {
		t.Error("#trade stage: anyone")
	}
}

func TestExFavourLimit(t *testing.T) {
	now := time.Now()
	ts := []time.Time{now.Add(-2 * time.Hour), now.Add(-30 * time.Minute), now.Add(-10 * time.Minute)}
	ts, ok := exFavourOK(ts, 3, now)
	if !ok || len(ts) != 2 {
		t.Fatalf("old favours pruned, 2 of 3 used: ok %v n %d", ok, len(ts))
	}
	ts = append(ts, now)
	if _, ok := exFavourOK(ts, 3, now); ok {
		t.Error("the 4th favour within an hour is refused")
	}
}

func TestAylaFloor(t *testing.T) {
	if !aylaQuoteOK("Ayla", false, 0, 0) {
		t.Error("a gift always reaches Ayla")
	}
	if aylaQuoteOK("Ayla", false, 0, 100) {
		t.Error("purse unknown: no paid quote to Ayla")
	}
	if aylaQuoteOK("ayla", true, 12000, 2500) {
		t.Error("12,000 - 2,500 is under the 10k floor")
	}
	if !aylaQuoteOK("Ayla", true, 12500, 2500) {
		t.Error("exactly 10k left is fine")
	}
	if !aylaQuoteOK("Kitty", false, 0, 50000) {
		t.Error("the floor is Ayla's")
	}
	if aylaCap("Ayla", true, 9000) != 0 || aylaCap("Ayla", true, 15000) != 5000 || aylaCap("Ayla", false, 0) != 0 || aylaCap("Bob", true, 1) != -1 {
		t.Error("aylaCap")
	}
}

func TestExchangeTierMatcher(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.ExchangeTiersOn = true })
	resetExch()
	defer resetExch()
	exch.Lock()
	exch.rel = map[string]exRel{"Seller": {party: "P", guild: "g1"}, "Mate": {party: "P", guild: "g1"},
		"Guildie": {guild: "g1"}, "Pal": {}, "Stranger": {guild: "g9"}}
	exch.friend = map[string]bool{pairKey("Seller", "Pal"): true}
	exch.spare = map[string]map[int]bool{"Seller": {100: true, 200: false}}
	exch.market = map[int]int{100: 8000, 200: 8000}
	exch.Unlock()
	s := &econSnap{name: "Seller"}
	get := func(buyer string, item int) (int, string) {
		return exchangeTier(s, &econSnap{name: buyer}, holding{ItemID: item, Ask: 9000})
	}
	if e, tr := get("Mate", 200); e != 0 || tr != tierParty {
		t.Errorf("party: %d %s", e, tr)
	}
	if e, tr := get("Guildie", 100); e != 0 || tr != tierKinFree {
		t.Errorf("guild spare: %d %s", e, tr)
	}
	if e, tr := get("Guildie", 200); e != 2000 || tr != tierKin {
		t.Errorf("guild non-spare: %d %s", e, tr)
	}
	if e, tr := get("Pal", 200); e != 2000 || tr != tierKin {
		t.Errorf("friend non-spare: %d %s", e, tr)
	}
	if _, tr := get("Stranger", 100); tr != "" {
		t.Errorf("stranger: market path, got %q", tr)
	}
	// preference: party beats kin beats stranger
	if exchangePrefer(&econSnap{name: "Mate"}, s, 200, &econSnap{name: "Stranger"}, 200) != 1 {
		t.Error("a party seller beats a stranger")
	}
	// favour limit: 3 per hour, then market
	now := time.Now()
	for i := 0; i < 3; i++ {
		exchangeChosen(s, &econSnap{name: "Guildie"}, 100, 1, 0, now)
	}
	if _, tr := get("Guildie", 100); tr != "" {
		t.Errorf("after 3 favours the giver sells at market, got %q", tr)
	}
	exch.Lock()
	n := exch.counts["matched: "+tierKinFree]
	exch.Unlock()
	if n != 3 {
		t.Errorf("counted %d kin_free matches", n)
	}
	// flag off: nothing changes
	withTuning(t, func(tn *Tuning) { tn.ExchangeTiersOn = false })
	if _, tr := get("Mate", 200); tr != "" {
		t.Error("flag off: market path")
	}
	if !exchangeEligible(&Want{Created: now}, &econSnap{name: "Mate"}, &econSnap{name: "Stranger"}, now) {
		t.Error("flag off: everyone eligible")
	}
}

func TestExchangeEligibleStages(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.ExchangeTiersOn = true; tn.ExchangeAskStageSec = 60 })
	resetExch()
	defer resetExch()
	exch.Lock()
	exch.rel = map[string]exRel{"Buyer": {party: "P", guild: "g1"}, "Mate": {party: "P", guild: "g1"}, "Guildie": {guild: "g1"}, "Other": {}}
	exch.Unlock()
	now := time.Now()
	buyer := &econSnap{name: "Buyer"}
	w := &Want{Created: now}
	if exchangeEligible(w, buyer, &econSnap{name: "Guildie"}, now) || !exchangeEligible(w, buyer, &econSnap{name: "Mate"}, now) {
		t.Error("party stage")
	}
	w.Created = now.Add(-70 * time.Second)
	if !exchangeEligible(w, buyer, &econSnap{name: "Guildie"}, now) || exchangeEligible(w, buyer, &econSnap{name: "Other"}, now) {
		t.Error("guild stage")
	}
	w.Created = now.Add(-130 * time.Second)
	if !exchangeEligible(w, buyer, &econSnap{name: "Other"}, now) {
		t.Error("#trade stage")
	}
}

// ---------------------------------------------------------------- chat jobs

type sentRec struct {
	sync.Mutex
	jobs  []string
	lines [][]ChatLine
}

func setupChatJobs(t *testing.T, on bool) *sentRec {
	t.Helper()
	withTuning(t, func(tn *Tuning) { tn.ChatJobsOn = on; tn.ChatJobWaitMinSec, tn.ChatJobWaitMaxSec = 20, 40 })
	rec := &sentRec{}
	oldSend, oldLoop := chatSend, chatJobsAutoLoop
	chatSend = func(j *ChatJob, lines []ChatLine) {
		rec.Lock()
		rec.jobs = append(rec.jobs, j.ID)
		rec.lines = append(rec.lines, lines)
		rec.Unlock()
	}
	chatJobsAutoLoop = false
	chatJobs.Lock()
	chatJobs.open, chatJobs.recent, chatJobs.agentSeen, chatJobs.counts = nil, nil, time.Time{}, map[string]int64{}
	chatJobs.Unlock()
	t.Cleanup(func() { chatSend, chatJobsAutoLoop = oldSend, oldLoop })
	return rec
}

func (r *sentRec) wait(t *testing.T, n int) [][]ChatLine {
	t.Helper()
	for i := 0; i < 200; i++ {
		r.Lock()
		got := len(r.lines)
		r.Unlock()
		if got >= n {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.Lock()
	defer r.Unlock()
	if len(r.lines) != n {
		t.Fatalf("sent %d jobs, want %d", len(r.lines), n)
	}
	return append([][]ChatLine(nil), r.lines...)
}

func spec(text string) ChatJobSpec {
	return ChatJobSpec{Kind: ChatKindAsk, Channel: "party", Hearers: []string{"Mate"},
		Template: []ChatLine{{Speaker: "Asker", Channel: "party", Text: text}, {Speaker: "Mate", Channel: "party", Text: "me!", DelaySec: 3}}}
}

func known(n string) bool { return n == "Asker" || n == "Mate" || n == "Other" }

func TestChatJobNoAgent(t *testing.T) {
	rec := setupChatJobs(t, true)
	if id := enqueueChatJobAt(spec("anyone got a clip?"), time.Now()); id != "" {
		t.Fatalf("no agent: template at once, got job %s", id)
	}
	got := rec.wait(t, 1)
	if got[0][0].Text != "anyone got a clip?" || len(got[0]) != 2 {
		t.Errorf("template lines: %+v", got[0])
	}
}

func TestChatJobFlagOff(t *testing.T) {
	rec := setupChatJobs(t, false)
	chatJobsPolled(time.Now())
	if id := enqueueChatJobAt(spec("x"), time.Now()); id != "" {
		t.Fatal("flag off: template at once even with an agent")
	}
	rec.wait(t, 1)
}

func TestChatJobAgentAnswers(t *testing.T) {
	rec := setupChatJobs(t, true)
	now := time.Now()
	chatJobsPolled(now)
	id := enqueueChatJobAt(spec("anyone got a clip?"), now)
	if id == "" {
		t.Fatal("agent present: the job waits")
	}
	if due := chatJobsDue(now.Add(10 * time.Second)); len(due) != 0 {
		t.Fatal("not due before its deadline")
	}
	// a revealing line is refused, the job keeps waiting
	if _, _, err := chatAnswer(id, []ChatLine{{Speaker: "Asker", Text: "my bot needs a clip"}}, known, now); err == nil {
		t.Fatal("a line mentioning bots must be refused")
	}
	// someone outside the conversation is refused
	if _, _, err := chatAnswer(id, []ChatLine{{Speaker: "Other", Text: "hi"}}, known, now); err == nil {
		t.Fatal("a speaker outside the job is refused")
	}
	// a public line is not allowed when the template had none
	if _, _, err := chatAnswer(id, []ChatLine{{Speaker: "Asker", Channel: "#global", Text: "hi all"}}, known, now); err == nil {
		t.Fatal("no extra public lines")
	}
	j, lines, err := chatAnswer(id, []ChatLine{{Speaker: "Asker", Text: "does anyone have a spare Clip? mine broke lol"},
		{Speaker: "Mate", Text: "i got one, sec"}}, known, now.Add(15*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if j.State != "agent" || len(lines) != 2 || lines[0].Channel != "party" || lines[1].DelaySec <= 0 {
		t.Errorf("agent lines: %s %+v", j.State, lines)
	}
	if _, _, err := chatAnswer(id, []ChatLine{{Speaker: "Asker", Text: "again"}}, known, now); err == nil {
		t.Error("a resolved job takes no more lines")
	}
	if due := chatJobsDue(now.Add(time.Minute)); len(due) != 0 {
		t.Error("an answered job never falls back to its template")
	}
	rec.wait(t, 0) // chatAnswer doesn't send: the API handler does
}

func TestChatJobTimeout(t *testing.T) {
	setupChatJobs(t, true)
	now := time.Now()
	chatJobsPolled(now)
	id := enqueueChatJobAt(spec("anyone got a clip?"), now)
	chatJobsPolled(now.Add(30 * time.Second))
	due := chatJobsDue(now.Add(41 * time.Second))
	if len(due) != 1 || due[0].ID != id || due[0].State != "template" || due[0].Said[0].Text != "anyone got a clip?" {
		t.Fatalf("timeout -> template: %+v", due)
	}
	if _, _, err := chatAnswer(id, []ChatLine{{Speaker: "Asker", Text: "late"}}, known, now.Add(42*time.Second)); err == nil {
		t.Error("late lines are refused")
	}
}

func TestChatJobAgentGone(t *testing.T) {
	setupChatJobs(t, true)
	now := time.Now()
	chatJobsPolled(now)
	enqueueChatJobAt(spec("a"), now)
	if due := chatJobsDue(now.Add(chatAgentFresh + time.Second)); len(due) != 1 {
		t.Fatal("when the agent stops polling, waiting jobs fall back to their templates")
	}
}

func TestChatJobEmptyLinesMeansTemplate(t *testing.T) {
	setupChatJobs(t, true)
	now := time.Now()
	chatJobsPolled(now)
	id := enqueueChatJobAt(spec("a"), now)
	j, lines, err := chatAnswer(id, nil, known, now)
	if err != nil || j.State != "template" || len(lines) != 2 {
		t.Fatalf("pass -> template: %v %+v", err, j)
	}
}

func TestChatLineOK(t *testing.T) {
	for _, bad := range []string{"", "two\nlines", "are you an AI?", "the engine says", strings.Repeat("x", 200), "legion rules"} {
		if _, err := chatLineOK(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, good := range []string{"lol np", "need a priest for orc dungeon", "mailed it, check ur rodex", "about time"} {
		if _, err := chatLineOK(good); err != nil {
			t.Errorf("%q refused: %v", good, err)
		}
	}
}

func TestChatValidateWhisper(t *testing.T) {
	j := &ChatJob{Speaker: "Seller", Channel: "whisper", To: "Buyer", Hearers: []string{"Buyer"},
		Lines: []ChatLine{{Speaker: "Seller", Channel: "whisper", To: "Buyer", Text: "x"}, {Speaker: "Buyer", Channel: "whisper", To: "Seller", Text: "y"}}}
	any := func(string) bool { return true }
	out, err := chatValidate(j, []ChatLine{{Speaker: "Seller", Text: "for u"}, {Speaker: "Buyer", Text: "ty!"}}, any)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].To != "Buyer" || out[1].To != "Seller" {
		t.Errorf("whisper partners filled in: %+v", out)
	}
	if _, err := chatValidate(j, []ChatLine{{Speaker: "Seller", Text: "a"}, {Speaker: "Seller", Text: "b"}, {Speaker: "Seller", Text: "c"}, {Speaker: "Seller", Text: "d"}}, any); err == nil {
		t.Error("too many lines")
	}
}

// ---------------------------------------------------------------- intents

func fakeLookup(q string) (int, string, bool) {
	if strings.EqualFold(q, "clip") || q == "2607" {
		return 2607, "Clip", true
	}
	return 0, "", false
}

func TestParseIntent(t *testing.T) {
	in, err := parseIntent(intentReq{From: "Ayla", Kind: "Need", Item: "clip", MaxPrice: 5000}, fakeLookup)
	if err != nil || in.ItemID != 2607 || in.Count != 1 || in.Kind != "need" || in.ZenyCap != -1 {
		t.Fatalf("%v %+v", err, in)
	}
	if in, err := parseIntent(intentReq{From: "Kitty", Kind: "offer", Item: float64(2607), Count: 2}, fakeLookup); err != nil || in.Count != 2 {
		t.Errorf("numeric item: %v", err)
	}
	for _, bad := range []intentReq{{Kind: "need", Item: "clip"}, {From: "A", Kind: "sell", Item: "clip"}, {From: "A", Kind: "need", Item: "nothing"},
		{From: "A", Kind: "need", Item: "clip", Count: 500}, {From: "A", Kind: "need", Item: "clip", MaxPrice: -1}} {
		if _, err := parseIntent(bad, fakeLookup); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func resetIntents() {
	intents.Lock()
	intents.list, intents.nextID = nil, 0
	intents.Unlock()
	hwants.Lock()
	hwants.m = map[string]*humanWant{}
	hwants.Unlock()
}

func TestIntentHumanFlagOff(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.ChatJobsOn = false })
	resetIntents()
	defer resetIntents()
	in := &chatIntent{From: "Somehuman", Kind: "need", ItemID: 2607, Item: "Clip", Count: 1, ZenyCap: -1}
	out := handleIntent(in, nil, false, time.Now())
	if out["ok"] != true || !in.Done || !strings.HasPrefix(in.Status, "noted") {
		t.Errorf("flag off: noted only: %+v %+v", out, in)
	}
	exchangeRound(nil, time.Now().Add(time.Minute))
	hwants.Lock()
	n := len(hwants.m)
	hwants.Unlock()
	if n != 0 {
		t.Error("flag off: no human want created")
	}
}

func TestIntentAylaFloorToMarket(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.ChatJobsOn = true })
	resetIntents()
	defer resetIntents()
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")
	write := func(zeny int) {
		ts := time.Now().Unix()
		os.WriteFile(path, []byte(`{"name":"Ayla","online":true,"zeny":`+strconv.Itoa(zeny)+`,"ts":`+strconv.Itoa(int(ts))+`}`), 0644)
		humanStats.Lock()
		humanStats.at = time.Time{}
		humanStats.Unlock()
	}
	t.Setenv("LEGION_HUMAN_STATUS", path)
	now := time.Now()

	// purse 13,000: she may pay at most 3,000 in total -> the market want's max is 1,500 each for 2
	write(13000)
	in := &chatIntent{From: "Ayla", Kind: "need", ItemID: 2607, Item: "Clip", Count: 2, ZenyCap: -1}
	handleIntent(in, nil, false, now)
	if in.ZenyCap != 3000 {
		t.Fatalf("zeny cap %d", in.ZenyCap)
	}
	exchangeRound(nil, now.Add(time.Minute))
	hwants.Lock()
	w := hwants.m["Ayla|2607"]
	hwants.Unlock()
	if w == nil || w.Max != 1500 {
		t.Fatalf("market want capped by the floor: %+v", w)
	}

	// purse 9,000: gifts only, nothing on the market
	resetIntents()
	write(9000)
	in = &chatIntent{From: "Ayla", Kind: "need", ItemID: 2607, Item: "Clip", Count: 1, ZenyCap: -1}
	handleIntent(in, nil, false, now)
	exchangeRound(nil, now.Add(time.Minute))
	hwants.Lock()
	n := len(hwants.m)
	hwants.Unlock()
	if n != 0 || in.Done || !strings.Contains(in.Status, "gifts only") {
		t.Errorf("under the floor: no paid market want (n=%d, status %q)", n, in.Status)
	}
}
