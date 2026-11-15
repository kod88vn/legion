package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStarveilAutoLeadDefaultOn(t *testing.T) {
	if !defaultTuning.StarveilAutoLead {
		t.Fatal("starveil_auto_lead must default on (owner order AY19)")
	}
}

func TestStarveilDutyOKNeverAylaParty(t *testing.T) {
	if !starveilDutyOK(&dutySnap{name: "member"}) {
		t.Fatal("a plain Starveil member is under the same duty rules (G25)")
	}
	for _, s := range []*dutySnap{{pinned: true}, {core: true}, {keep: true}, nil} {
		if starveilDutyOK(s) {
			t.Fatalf("Ayla's party / kept account must never be warned or kicked (G20): %+v", s)
		}
	}
}

func TestStarveilFundPick(t *testing.T) {
	none := func(string) bool { return false }
	rich := func(n string) guildMemberInfo {
		return guildMemberInfo{Name: n, Online: true, InGuild: true, Spare: 20000} // fundShare 5000 each
	}
	// the alchemist without the guide comes first
	alch := guildMemberInfo{Name: "Brew", Online: true, InGuild: true, Brewer: true, Zeny: 50000}
	weak := guildMemberInfo{Name: "Weak", Online: true, InGuild: true, Weak: 0.1,
		Next: &gearStep{Item: "Guard", Cost: 3000, Have: 1000, Shop: "prontera"}}
	ms := []guildMemberInfo{rich("A"), rich("B"), alch, weak}
	to, amt, why := starveilFundPick(ms, none)
	if to != "Brew" || why != "Potion Creation Guide" || amt != 10000+100 {
		t.Fatalf("guide first: got %s %d %s", to, amt, why)
	}
	// the alchemist skipped (funded recently): the weakest member's next shop piece
	to, amt, why = starveilFundPick(ms, func(n string) bool { return n == "Brew" })
	if to != "Weak" || amt != 2000 || why != "Guard" {
		t.Fatalf("gear next: got %s %d %s", to, amt, why)
	}
	// a pool that can't cover the gap pays nothing
	poor := []guildMemberInfo{weak, {Name: "C", Online: true, InGuild: true, Spare: 1000}}
	if to, _, _ := starveilFundPick(poor, none); to != "" {
		t.Fatalf("no pool: got %s", to)
	}
	// her party friends' zeny is never in the pool (they pool for their own gear turns)
	friends := []guildMemberInfo{weak, {Name: "F", Online: true, Friend: true, Spare: 50000}}
	if to, _, _ := starveilFundPick(friends, none); to != "" {
		t.Fatalf("friends pool: got %s", to)
	}
}

func TestStarveilKeepAmount(t *testing.T) {
	for _, c := range [][2]int{{0, 2000}, {3450, 3400}, {90000, 5000}} {
		if got := starveilKeepAmount(c[0]); got != c[1] {
			t.Fatalf("keep %d: got %d want %d", c[0], got, c[1])
		}
	}
}

func TestStarveilOrdersTakeOnceAndExpire(t *testing.T) {
	starveilLead.Lock()
	starveilLead.q = nil
	starveilLead.Unlock()
	now := time.Now()
	fetched := 0
	starveilQueue(&starveilOrder{Kind: "warn", Char: "x", Guild: "Starveil", fetch: func() { fetched++ }}, now.Add(-2*starveilOrderTTL))
	starveilQueue(&starveilOrder{Kind: "kick", Char: "y", Guild: "Starveil"}, now)
	got := starveilTake(now)
	if len(got) != 1 || got[0].Kind != "kick" {
		t.Fatalf("live orders: %+v", got)
	}
	if again := starveilTake(now); len(again) != 0 {
		t.Fatalf("orders are taken once: %+v", again)
	}
	if fetched != 0 {
		t.Fatal("an expired warning must not trigger the member's answer")
	}
	for i := 0; i < starveilOrderMax+5; i++ {
		starveilQueue(&starveilOrder{Kind: "say", Guild: "Starveil"}, now)
	}
	if n := len(starveilTake(now)); n != starveilOrderMax {
		t.Fatalf("queue cap: %d", n)
	}
}

// ---------------------------------------------------------------- Ayla on the engine (package D, ayla_engine_on)

// withAylaEngine runs f with ayla_engine_on set to on and the instance set to core (onlyNames) or main, then restores.
func withAylaEngine(t *testing.T, on, core bool, f func()) {
	t.Helper()
	old := tune()
	nt := *old
	nt.EngineParties = ""
	if on {
		nt.EngineParties = "Ayla" // the one switch: package C's engine_parties
	}
	curTuning.Store(&nt)
	oldO, oldE, oldC := *onlyNames, *excludeNames, *coreAPI
	defer func() {
		curTuning.Store(old)
		*onlyNames, *excludeNames, *coreAPI = oldO, oldE, oldC
	}()
	*onlyNames, *excludeNames = "", ""
	if core {
		*onlyNames = "Ayla"
	} else {
		*excludeNames = "Ayla"
	}
	f()
}

func TestAylaEngineDefaultOff(t *testing.T) {
	if defaultTuning.EngineParties != "" || aylaEngineOn() {
		t.Fatal("engine_parties must default empty: the switch stays dark until the owner says so")
	}
	withAylaEngine(t, false, false, func() {
		if aylaEngineOn() {
			t.Fatal("empty engine_parties: Ayla is not on the engine")
		}
	})
	withAylaEngine(t, true, false, func() {
		if !aylaEngineOn() {
			t.Fatal("engine_parties listing Ayla: she is on the engine")
		}
	})
}

func TestAylaEngineOffChangesNothing(t *testing.T) {
	withAylaEngine(t, false, true, func() {
		if starveilAutoLead() {
			t.Fatal("switch off, core instance: Starveil is not led by the engine (unchanged)")
		}
		if starveilDutyOK(&dutySnap{name: "member"}) {
			t.Fatal("switch off, core instance: no duty for Starveil (unchanged)")
		}
	})
	withAylaEngine(t, false, false, func() {
		if !starveilAutoLead() || !starveilDutyOK(&dutySnap{name: "member"}) {
			t.Fatal("switch off, main instance: the engine leads Starveil as before")
		}
	})
	withAylaEngine(t, false, true, func() {
		// the orders endpoint keeps handing the queue to Ayla's plugin while the switch is off
		starveilLead.Lock()
		old := starveilLead.q
		starveilLead.q = nil
		starveilLead.Unlock()
		defer func() { starveilLead.Lock(); starveilLead.q = old; starveilLead.Unlock() }()
		starveilQueue(&starveilOrder{Kind: "say", Guild: "Starveil", Line: "hi"}, time.Now())
		mux := http.NewServeMux()
		registerStarveilOrders(mux)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/human-guild/orders", nil))
		var got struct {
			On     bool            `json:"on"`
			Orders []starveilOrder `json:"orders"`
		}
		json.Unmarshal(rec.Body.Bytes(), &got)
		if len(got.Orders) != 1 {
			t.Fatalf("switch off: the plugin must still get its order, got %d", len(got.Orders))
		}
	})
}

func TestAylaEngineOnCoreRunsStarveilLead(t *testing.T) {
	withAylaEngine(t, true, true, func() {
		if !starveilAutoLead() {
			t.Fatal("switch on, core instance: the engine leads Starveil (gate relaxed for Starveil)")
		}
		if !starveilDutyOK(&dutySnap{name: "member"}) {
			t.Fatal("switch on, core instance: a plain Starveil member is under the duty rules")
		}
		for _, s := range []*dutySnap{{pinned: true}, {core: true}, {keep: true}} {
			if starveilDutyOK(s) {
				t.Fatalf("switch on: Ayla's party is still never warned or kicked (G20): %+v", s)
			}
		}
	})
}

func TestAylaEngineMasterBotFound(t *testing.T) {
	member := &Bot{Online: true, GuildID: 7, GuildName: "Starveil"}
	notOnline := &Bot{Online: false, guildMaster: true, GuildID: 7, GuildName: "Starveil"}
	other := &Bot{Online: true, guildMaster: true, GuildID: 9, GuildName: "Crimson Oath"}
	master := &Bot{Online: true, guildMaster: true, GuildID: 7, GuildName: "Starveil"}
	if got := starveilMasterBot([]*Bot{member, notOnline, other, master}, "Starveil"); got != master {
		t.Fatalf("master bot = %p, want the online master of Starveil", got)
	}
	if got := starveilMasterBot([]*Bot{member, notOnline, other}, "Starveil"); got != nil {
		t.Fatal("no online master: nil, so nothing is sent")
	}
}

func TestAylaEngineDrainPostsToCore(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Write([]byte(`{"orders":1}`))
	}))
	defer srv.Close()
	withAylaEngine(t, true, false, func() {
		*coreAPI = srv.URL
		starveilLead.Lock()
		old := starveilLead.q
		starveilLead.q = nil
		starveilLead.Unlock()
		defer func() { starveilLead.Lock(); starveilLead.q = old; starveilLead.Unlock() }()
		now := time.Now()
		starveilQueue(&starveilOrder{Kind: "invite", Guild: "Starveil", AID: 2000001}, now)
		starveilEngineDrain(now)
		var sent []starveilOrder
		if err := json.Unmarshal(body, &sent); err != nil || len(sent) != 1 || sent[0].AID != 2000001 || sent[0].Kind != "invite" {
			t.Fatalf("core got %s (err %v), want the one invite", body, err)
		}
		if left := starveilTake(now); len(left) != 0 {
			t.Fatal("drained orders are taken once: the queue must be empty")
		}
	})
}

func TestAylaEngineCoreEndpointGated(t *testing.T) {
	mux := http.NewServeMux()
	registerStarveilEngine(mux, nil)
	post := func() int {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/human-guild/engine", strings.NewReader("[]")))
		return rec.Code
	}
	withAylaEngine(t, false, true, func() {
		if c := post(); c != http.StatusConflict {
			t.Fatalf("switch off: %d, want 409", c)
		}
	})
	withAylaEngine(t, true, true, func() {
		if c := post(); c != http.StatusConflict {
			t.Fatalf("switch on but no master bot in core: %d, want 409", c)
		}
	})
}

func TestTaxAndGuildSkillsPlan(t *testing.T) {
	if taxTarget(0) != taxMasterPct || taxTarget(1) != taxOfficerPct || taxTarget(3) != taxOfficerPct || taxTarget(4) != taxMemberPct {
		t.Fatal("tax: master, officers (1-3) and members pay their own pay rates")
	}
	if len(guildSkillPlan) == 0 || guildSkillPlan[0].Name != "GD_EXTENSION" || guildSkillPlan[0].Lv != 10 {
		t.Fatal("guild skills: Guild Extension to level 10 is bought first (more members)")
	}
}

func TestAylaSpendFloorG12(t *testing.T) {
	cases := []struct {
		name         string
		zeny, amount int
		want         bool
	}{
		{"Ayla", 15000, 5000, true},  // leaves exactly 10,000
		{"ayla", 15000, 5001, false}, // one zeny under the floor
		{"Ayla", 10000, 1, false},
		{"Ayla", 15000, 0, false},  // nothing to send
		{"Human", 100, 5000, true}, // other characters are not limited here
	}
	for _, c := range cases {
		if got := aylaSpendOK(c.name, c.zeny, c.amount); got != c.want {
			t.Errorf("aylaSpendOK(%s, %d, %d) = %v, want %v", c.name, c.zeny, c.amount, got, c.want)
		}
	}
}

func TestAylaGiftCapG12(t *testing.T) {
	if got := aylaGiftCap("Ayla", 12000, 9000); got != 2000 {
		t.Fatalf("Ayla with 12,000z may give 2,000z, got %d", got)
	}
	if got := aylaGiftCap("Ayla", 9000, 5000); got != 0 {
		t.Fatalf("Ayla under the floor gives nothing, got %d", got)
	}
	if got := aylaGiftCap("Human", 12000, 9000); got != 9000 {
		t.Fatalf("other characters keep their gift, got %d", got)
	}
}

func TestStarveilLeadTickInterval(t *testing.T) {
	withAylaEngine(t, false, false, func() {
		if got := starveilLeadTick(); got != time.Minute {
			t.Fatalf("switch off: lead loop every %v, want 1m0s (unchanged)", got)
		}
	})
	withAylaEngine(t, true, false, func() {
		if got := starveilLeadTick(); got != 15*time.Second {
			t.Fatalf("switch on: lead loop every %v, want 15s", got)
		}
	})
}

func TestStarveilTargetOKG20(t *testing.T) {
	core := []*Bot{{aid: 2000001, cid: 150001, cur: &CharSpec{Name: "Core1"}}}
	cases := []struct {
		name string
		o    starveilOrder
		want bool
	}{
		{"invite a core bot by account", starveilOrder{Kind: "invite", AID: 2000001}, false},
		{"kick a core bot by char id", starveilOrder{Kind: "kick", CID: 150001, Char: "Core1"}, false},
		{"kick a core bot by name, other case", starveilOrder{Kind: "kick", Char: "core1"}, false},
		{"kick Ayla by name", starveilOrder{Kind: "kick", Char: "Ayla"}, false},
		{"invite an outside player", starveilOrder{Kind: "invite", AID: 2100005, Char: "Outsider"}, true},
		{"kick an outside member", starveilOrder{Kind: "kick", AID: 2100006, CID: 150777, Char: "Member"}, true},
	}
	for _, c := range cases {
		if got := starveilTargetOK(core, c.o); got != c.want {
			t.Errorf("%s: starveilTargetOK = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestStarveilExecRefusesCoreTargets(t *testing.T) {
	core := []*Bot{{aid: 2000001, cid: 150001, cur: &CharSpec{Name: "Core1"}}}
	before := func() int {
		starveilLead.Lock()
		defer starveilLead.Unlock()
		return starveilLead.counters["engine refused: kick aimed at a core bot or Ayla"] +
			starveilLead.counters["engine refused: invite aimed at a core bot or Ayla"]
	}
	n := before()
	// the master bot has no connection: a refused order returns before anything is sent
	master := &Bot{cur: &CharSpec{Name: "Ayla"}}
	starveilExec(master, starveilOrder{Kind: "kick", CID: 150001, Char: "Core1"}, core)
	starveilExec(master, starveilOrder{Kind: "invite", AID: 2000001}, core)
	if got := before(); got != n+2 {
		t.Fatalf("refused core targets: counted %d, want %d", got, n+2)
	}
}

func TestG12MailCallSiteKeepsFloor(t *testing.T) {
	// 12,000z: a 5,000z gift costs 5,100 with the RODEX fee and would leave Ayla under 10,000z
	b := &Bot{cur: &CharSpec{Name: "Ayla"}, Zeny: 12000}
	if b.mailZeny("Outsider", 5000, "gift", "", nil) {
		t.Fatal("Ayla's mail gift must not take her under 10,000z (G12)")
	}
}

func TestG12TradeAndGiftCallSites(t *testing.T) {
	ayla := &Bot{cur: &CharSpec{Name: "Ayla"}, Zeny: 12000, P: &Persona{}}
	if ayla.zenyPayOK(5000) {
		t.Fatal("Ayla's trade payment of 5,000z from 12,000z leaves her under the floor (G12, strict)")
	}
	if !ayla.zenyPayOK(2000) {
		t.Fatal("a 2,000z payment from 12,000z keeps exactly 10,000z")
	}
	human := &Bot{cur: &CharSpec{Name: "Outsider"}, Zeny: 12000, P: &Persona{}}
	if !human.zenyPayOK(12000) {
		t.Fatal("other characters are not limited by the floor here")
	}
	if g := ayla.giftableZeny(); g > 2000 {
		t.Fatalf("Ayla with 12,000z may give at most 2,000z to a gear pool, got %d", g)
	}
}

func TestAylaEngineDrainRequeuesOn409DeliversOnce(t *testing.T) {
	var status int32 = http.StatusConflict
	var posts []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got []starveilOrder
		json.Unmarshal(body, &got)
		posts = append(posts, len(got))
		w.WriteHeader(int(atomic.LoadInt32(&status)))
	}))
	defer srv.Close()
	withAylaEngine(t, true, false, func() {
		*coreAPI = srv.URL
		starveilLead.Lock()
		old := starveilLead.q
		starveilLead.q = nil
		starveilLead.Unlock()
		defer func() { starveilLead.Lock(); starveilLead.q = old; starveilLead.Unlock() }()
		humanGuild.Lock()
		delete(humanGuild.invited, "acct-1")
		humanGuild.Unlock()

		now := time.Now()
		fetches := 0
		starveilQueue(&starveilOrder{Kind: "warn", Char: "Member", Guild: "Starveil", Line: "hi",
			fetch: func() { fetches++ }}, now)
		starveilQueue(&starveilOrder{Kind: "invite", Guild: "Starveil", AID: 2100009, invitedAcct: "acct-1"}, now)

		starveilEngineDrain(now) // core answers 409: nothing delivered
		starveilLead.Lock()
		left := len(starveilLead.q)
		starveilLead.Unlock()
		if left != 2 {
			t.Fatalf("409: %d orders queued, want both back in the queue", left)
		}
		if fetches != 0 {
			t.Fatalf("409: the warning's member answer ran %d times before delivery", fetches)
		}
		humanGuild.Lock()
		_, marked := humanGuild.invited["acct-1"]
		humanGuild.Unlock()
		if marked {
			t.Fatal("409: the candidate must not be marked suggested before delivery")
		}

		atomic.StoreInt32(&status, http.StatusOK)
		starveilEngineDrain(now) // core takes them now
		if len(posts) != 2 || posts[1] != 2 {
			t.Fatalf("posts = %v, want the second post to carry both orders", posts)
		}
		if fetches != 1 {
			t.Fatalf("delivered warning ran its member answer %d times, want once", fetches)
		}
		humanGuild.Lock()
		_, marked = humanGuild.invited["acct-1"]
		humanGuild.Unlock()
		if !marked {
			t.Fatal("delivered invite: the candidate is marked suggested")
		}

		starveilEngineDrain(now) // nothing left: no second delivery, no second run
		if fetches != 1 || len(posts) != 2 {
			t.Fatalf("after delivery: fetches %d, posts %d; a delivered order is never sent again", fetches, len(posts))
		}
	})
}

func TestStarveilRequeueDropsExpired(t *testing.T) {
	starveilLead.Lock()
	old := starveilLead.q
	starveilLead.q = nil
	starveilLead.Unlock()
	defer func() { starveilLead.Lock(); starveilLead.q = old; starveilLead.Unlock() }()
	now := time.Now()
	stale := &starveilOrder{Kind: "say", Line: "old", At: now.Add(-starveilOrderTTL - time.Minute)}
	fresh := &starveilOrder{Kind: "say", Line: "new", At: now}
	starveilRequeue([]*starveilOrder{stale, fresh}, now)
	starveilLead.Lock()
	defer starveilLead.Unlock()
	if len(starveilLead.q) != 1 || starveilLead.q[0] != fresh {
		t.Fatalf("requeue kept %d orders, want only the one within its TTL", len(starveilLead.q))
	}
}
