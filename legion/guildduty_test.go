package main

import (
	"encoding/binary"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDirBasicFit(t *testing.T) {
	d := &GuildDirection{Kind: dirStock, ItemID: 999, MinLv: 30, MaxLv: 60, Classes: []string{"archer line"}}
	cases := []struct {
		in   dutyFitIn
		want bool
	}{
		{dutyFitIn{Lv: 45, Class: "Hunter"}, true},
		{dutyFitIn{Lv: 45, Class: "Sniper"}, true},
		{dutyFitIn{Lv: 45, Class: "Knight"}, false}, // class not asked for
		{dutyFitIn{Lv: 25, Class: "Hunter"}, false}, // under min
		{dutyFitIn{Lv: 61, Class: "Hunter"}, false}, // over max
		{dutyFitIn{Lv: 45, Class: "Hunter", Vendor: true}, false},
		{dutyFitIn{Lv: 45, Class: "Hunter", Beginner: true}, false},
	}
	for i, c := range cases {
		if ok, why := dirBasicFit(d, c.in); ok != c.want {
			t.Errorf("case %d: got %v (%s), want %v", i, ok, why, c.want)
		}
	}
	// healers don't solo-farm, but they can carry
	if ok, _ := dirBasicFit(&GuildDirection{Kind: dirSupply}, dutyFitIn{Lv: 50, Class: "Priest", Healer: true}); ok {
		t.Error("healer fits a farming direction")
	}
	if ok, _ := dirBasicFit(&GuildDirection{Kind: dirCarry, Map: "pay_fild04"}, dutyFitIn{Lv: 50, Class: "Priest", Healer: true}); !ok {
		t.Error("healer should fit carry")
	}
	if ok, _ := dirBasicFit(&GuildDirection{Kind: dirCarry}, dutyFitIn{Lv: 50, Class: "Knight"}); ok {
		t.Error("carry without a map fits")
	}
	if !classMatches("knight", "Knight") || !classMatches("Swordman line", "Lord Knight") || classMatches("mage", "Knight") {
		t.Error("classMatches")
	}
}

func TestDutyDays(t *testing.T) {
	if dutyOwed(300, 120) != 120 || dutyOwed(100, 120) != 50 || dutyOwed(0, 120) != 0 {
		t.Fatal("dutyOwed")
	}
	cases := []struct {
		d    dutyDay
		want string
	}{
		{dutyDay{OnlineMin: 20, OnMin: 20}, "unplayed"},
		{dutyDay{OnlineMin: 300, OnMin: 100, Fit: 1}, "off"}, // the flag was off most of the day
		{dutyDay{OnlineMin: 300, OnMin: 300, DutyMin: 112, Fit: 1}, "met"},
		{dutyDay{OnlineMin: 100, OnMin: 100, DutyMin: 40, Fit: 1}, "met"}, // owed = half of 100, tolerance 10
		{dutyDay{OnlineMin: 300, OnMin: 300, DutyMin: 0, Unfit: 5}, "excused"},
		{dutyDay{OnlineMin: 300, OnMin: 300, DutyMin: 0, Busy: 5}, "excused"},
		{dutyDay{OnlineMin: 300, OnMin: 300, DutyMin: 60, Fit: 1}, "short"},
	}
	for i, c := range cases {
		d := c.d
		if got := closeDay(&d, 120, dutyPlayedMin); got != c.want {
			t.Errorf("case %d: %s, want %s", i, got, c.want)
		}
	}
}

func TestStandingTransitions(t *testing.T) {
	run := func(strict float64, days []string) (dutyStanding, []string) {
		wa, ka := dutyThresholds(3, 2, strict)
		st := dutyStanding{Standing: standGood}
		var acts []string
		for _, r := range days {
			var a string
			st, a = nextStanding(st, r, wa, ka)
			if a != "" {
				acts = append(acts, a)
			}
		}
		return st, acts
	}
	// normal guild: 3 short days -> warning, 2 more -> kick; excused / unplayed days change nothing
	st, acts := run(1.0, []string{"short", "excused", "short", "unplayed", "short"})
	if st.Standing != standWarned || len(acts) != 1 || acts[0] != actWarn {
		t.Fatalf("normal warn: %+v %v", st, acts)
	}
	st, acts = run(1.0, []string{"short", "short", "short", "short", "short"})
	if st.Standing != standKicked || len(acts) != 2 || acts[1] != actKick {
		t.Fatalf("normal kick: %+v %v", st, acts)
	}
	// a met day takes one short day off; behind -> good
	st, _ = run(1.0, []string{"short", "short", "met", "met"})
	if st.Standing != standGood || st.Short != 0 {
		t.Fatalf("recover: %+v", st)
	}
	// warned stays warned until made up completely
	st, _ = run(1.0, []string{"short", "short", "short", "met"})
	if st.Standing != standWarned || st.Short != 2 {
		t.Fatalf("warned recover: %+v", st)
	}
	// strict (1.5): warn after 2, kick after 1 more
	if wa, ka := dutyThresholds(3, 2, 1.5); wa != 2 || ka != 1 {
		t.Fatalf("strict thresholds %d %d", wa, ka)
	}
	st, acts = run(guildStrictness("guild07"), []string{"short", "short", "short"})
	if st.Standing != standKicked || len(acts) != 2 {
		t.Fatalf("strict: %+v %v", st, acts)
	}
	// social (0.5): 5 short days are not even a warning
	st, acts = run(guildStrictness("guild06"), []string{"short", "short", "short", "short", "short"})
	if st.Standing != standBehind || len(acts) != 0 {
		t.Fatalf("social: %+v %v", st, acts)
	}
	// exempt and kicked never move
	if s, a := nextStanding(dutyStanding{Standing: standExempt}, "short", 1, 1); s.Standing != standExempt || a != "" {
		t.Fatal("exempt moved")
	}
}

func TestDutyCloseDaysAcrossDays(t *testing.T) {
	m := &dutyMember{Char: "x", Guild: "guild05", dutyStanding: dutyStanding{Standing: standGood}}
	for _, date := range []string{"2026-11-10", "2026-11-11", "2026-11-12"} {
		m.Days = append(m.Days, &dutyDay{Date: date, OnlineMin: 240, OnMin: 240, DutyMin: 20, Fit: 2})
	}
	gduty.Lock()
	dutyCloseDaysLocked(m, "2026-11-13", 120, time.Now())
	gduty.Unlock()
	for _, d := range m.Days {
		if d.Result != "short" || d.Owed != 120 {
			t.Fatalf("day %s: %s owed %d", d.Date, d.Result, d.Owed)
		}
	}
	if m.Standing != standWarned || !m.WarnPending {
		t.Fatalf("after 3 short days: %+v warn=%v", m.dutyStanding, m.WarnPending)
	}
	// closing again changes nothing (days already closed); today's open day is left alone
	m.Days = append(m.Days, &dutyDay{Date: "2026-11-13", OnlineMin: 10})
	gduty.Lock()
	dutyCloseDaysLocked(m, "2026-11-13", 120, time.Now())
	gduty.Unlock()
	if m.Days[3].Result != "" || m.Short != 3 {
		t.Fatalf("re-close: %+v %+v", m.Days[3], m.dutyStanding)
	}
	// an exempt member's days close without any standing change
	e := &dutyMember{Char: "y", Guild: "guild05", Exempt: "guild trader", dutyStanding: dutyStanding{Standing: standExempt},
		Days: []*dutyDay{{Date: "2026-11-10", OnlineMin: 240, OnMin: 240, Fit: 1}}}
	gduty.Lock()
	dutyCloseDaysLocked(e, "2026-11-13", 120, time.Now())
	gduty.Unlock()
	if e.Standing != standExempt || e.WarnPending || e.KickPending {
		t.Fatalf("exempt: %+v", e)
	}
}

func TestDutyExempt(t *testing.T) {
	if dutyExempt(dutyExemptIn{}) != "" {
		t.Fatal("plain member exempt")
	}
	for i, e := range []dutyExemptIn{{Leader: true}, {Alchemist: true}, {Trader: true}, {AylaParty: true}} {
		if dutyExempt(e) == "" {
			t.Errorf("case %d not exempt", i)
		}
	}
}

func TestRankWealth(t *testing.T) {
	a := guildRankScore(rankIn{GuildLevel: 3, MemberLvs: []int{80, 70, 60}, TopPlaces: []int{1, 50}})
	// 30 + avg(80,70,60)=70 + (100/10 + 51/10) = 115.1
	if a != 115.1 {
		t.Fatalf("score %v", a)
	}
	many := make([]int, 20)
	for i := range many {
		many[i] = 50
	}
	many[0] = 99
	if s := guildRankScore(rankIn{MemberLvs: many}); s != 54.9 { // top 10 only: (99 + 9 x 50) / 10
		t.Fatalf("top10 %v", s)
	}
	if guildWealth(1000, 500) != 1500 || guildWealth(-5, 10) != 10 {
		t.Fatal("wealth")
	}
	pl := rankOrder(map[string]float64{"a": 10, "b": 20, "c": 10}, map[string]int{"a": 5, "c": 9})
	if pl["b"] != 1 || pl["c"] != 2 || pl["a"] != 3 {
		t.Fatalf("order %v", pl)
	}
	top := joinPreference(1, 10, 1, 0, true)
	low := joinPreference(10, 10, 0.1, 0, true)
	fr := joinPreference(10, 10, 0.1, 2, true)
	if !(top > low && fr > low) || joinPreference(1, 10, 1, 3, false) != 0 {
		t.Fatalf("join pref %v %v %v", top, low, fr)
	}
}

func TestRelationDrift(t *testing.T) {
	v := 0.0
	for i := 0; i < 30; i++ {
		v = relApply(v, relPoach)
	}
	if v != -1 {
		t.Fatalf("clamp %v", v)
	}
	if relApply(0.99, 0.5) != 1 {
		t.Fatal("clamp up")
	}
	d := relDrift(-0.8, 10)
	if !(d > -0.8 && d < -0.4) {
		t.Fatalf("drift %v", d)
	}
	if relDrift(0.5, 0) != 0.5 {
		t.Fatal("no time, no drift")
	}
	if relLabel(-0.6) != "rival" || relLabel(0) != "neutral" || relLabel(0.7) != "ally" {
		t.Fatal("labels")
	}
	if relKey("b", "a") != relKey("a", "b") {
		t.Fatal("key order")
	}
}

func TestAutopilotDirs(t *testing.T) {
	now := time.Now()
	lows := []dutyLow{{"Hi", "gl_cas01", 80}, {"Mid", "pay_dun02", 70}, {"Lo", "pay_fild04", 30}, {"Town", "prontera", 20}}
	ds := autopilotDirs("guild02", []int{itemWhitePotion, 1234}, true, lows, now)
	if len(ds) != 3 || ds[0].Kind != dirSupply || ds[1].Kind != dirStock || ds[1].ItemID != 1234 || ds[2].Kind != dirCarry {
		t.Fatalf("dirs %+v", ds)
	}
	if ds[2].Map != "pay_fild04" || ds[2].MinLv != 40 { // the lowest one in town is skipped
		t.Fatalf("carry %+v", ds[2])
	}
	for _, d := range ds {
		if d.SetBy != "autopilot" || !d.Expires.After(now) {
			t.Fatalf("meta %+v", d)
		}
	}
	// nothing to do -> only the wealth fill
	ds = autopilotDirs("guild02", nil, false, nil, now)
	if len(ds) != 1 || ds[0].Kind != dirWealth {
		t.Fatalf("fill %+v", ds)
	}
}

func TestActiveDirsLeaderFirst(t *testing.T) {
	now := time.Now()
	gduty.Lock()
	defer gduty.Unlock()
	saveD, saveA := gduty.dirs["guildT"], gduty.auto["guildT"]
	defer func() { gduty.dirs["guildT"], gduty.auto["guildT"] = saveD, saveA }()
	gduty.dirs["guildT"] = []*GuildDirection{
		{ID: "L1", Kind: dirLevel, Map: "a", SetBy: "leader", Expires: now.Add(time.Hour)},
		{ID: "L0", Kind: dirLevel, Map: "b", SetBy: "leader", Expires: now.Add(-time.Minute)}, // expired
	}
	gduty.auto["guildT"] = autopilotDirs("guildT", []int{77}, true, nil, now)
	act := activeDirsLocked("guildT", now)
	if len(act) != 3 || act[0].ID != "L1" || act[1].Kind != dirSupply || act[2].Kind != dirStock {
		t.Fatalf("active %+v", act)
	}
	if len(gduty.dirs["guildT"]) != 1 {
		t.Fatal("expired leader direction kept")
	}
	gduty.dirs["guildT"][0].Exclusive = true
	if act = activeDirsLocked("guildT", now); len(act) != 1 {
		t.Fatalf("exclusive %+v", act)
	}
}

func TestDutyExpelPacket(t *testing.T) {
	p := dutyExpelPacket(7, 2000001, 150003, "not helping the guild")
	if len(p) != 54 {
		t.Fatalf("len %d, recvpackets.txt says 015B 54", len(p))
	}
	le := binary.LittleEndian
	if le.Uint16(p) != 0x015b || le.Uint32(p[2:]) != 7 || le.Uint32(p[6:]) != 2000001 || le.Uint32(p[10:]) != 150003 {
		t.Fatalf("layout % x", p[:14])
	}
	if string(p[14:35]) != "not helping the guild" || p[35] != 0 {
		t.Fatalf("reason %q", p[14:54])
	}
}

func TestNewLeaderDirection(t *testing.T) {
	now := time.Now()
	if _, err := newLeaderDirection("guild01", dirPost{Kind: "bounty"}, now); err == nil {
		t.Error("bad kind accepted")
	}
	if _, err := newLeaderDirection("guild01", dirPost{Kind: "stock"}, now); err == nil {
		t.Error("stock without item accepted")
	}
	if _, err := newLeaderDirection("guild01", dirPost{Kind: "carry"}, now); err == nil {
		t.Error("carry without map accepted")
	}
	if _, err := newLeaderDirection("guild01", dirPost{Kind: "level", MinLv: 50, MaxLv: 40}, now); err == nil {
		t.Error("min > max accepted")
	}
	d, err := newLeaderDirection("guild01", dirPost{Kind: "level", Hours: 2}, now)
	if err != nil || d.SetBy != "leader" || d.Expires.Sub(now) != 2*time.Hour {
		t.Fatalf("level: %+v %v", d, err)
	}
	d, _ = newLeaderDirection("guild01", dirPost{Kind: "wealth"}, now)
	if d.Expires.Sub(now) != guildDirLeaderTTL {
		t.Fatal("default TTL")
	}
}

func TestDutyGuildParam(t *testing.T) {
	if dutyGuildParam("guild03") != "guild03" || dutyGuildParam("Crimson Oath") != "guild01" ||
		dutyGuildParam("starveil") != humanGuildID || dutyGuildParam("nope") != "" {
		t.Fatal("dutyGuildParam")
	}
	if dutyGuildID("crimson oath", "Starveil") != "guild01" || dutyGuildID("starveil ", "Starveil") != humanGuildID ||
		dutyGuildID("Other", "Starveil") != "" {
		t.Fatal("dutyGuildID")
	}
}

// The meter's duty flag follows the guild block (guildmeter.go) and needs no lock beyond the bot's own.
func TestGuildDutyMeterHook(t *testing.T) {
	old := tune()
	defer curTuning.Store(old)
	if meterDutyHook == nil {
		t.Fatal("meterDutyHook not wired")
	}
	on := *old
	on.GuildDutyOn = true
	curTuning.Store(&on)
	b := &Bot{MapName: "pay_fild04"}
	if meterDutyHook(b) {
		t.Fatal("no goal: not on duty")
	}
	b.fgoal = &farmGoal{Kind: "gear", Maps: []string{"pay_fild04"}}
	if meterDutyHook(b) {
		t.Fatal("a gear goal is not guild duty")
	}
	b.fgoal = &farmGoal{Kind: "guild", Maps: []string{"pay_fild04", "pay_fild08"}}
	if !meterDutyHook(b) {
		t.Fatal("guild block on its map: on duty")
	}
	b.MapName = "payon"
	if meterDutyHook(b) {
		t.Fatal("off the block's maps: not on duty")
	}
	b.MapName, b.Dead = "pay_fild08", true
	if meterDutyHook(b) {
		t.Fatal("dead: not on duty")
	}
	b.Dead = false
	off := *old
	off.GuildDutyOn = false
	curTuning.Store(&off)
	if meterDutyHook(b) {
		t.Fatal("flag off: not on duty")
	}
}

// Guild lines go through chat jobs when chat_jobs_on is set (guildchat.go); off -> the caller's old guildSay path.
func TestGuildChatJobs(t *testing.T) {
	l := &Bot{P: &Persona{Chars: []CharSpec{{Name: "LeaderX"}}}, GuildName: "Iron Wolves"}
	rec := setupChatJobs(t, false)
	if dutyChatWarn(l, "MemberY", "do ur shift", "sorry", 3) {
		t.Fatal("chat jobs off: the caller must say the lines itself")
	}
	if _, used := dutyChatKick(l, "MemberY", "bye", "short days"); used {
		t.Fatal("chat jobs off: kick line is the caller's")
	}
	rec = setupChatJobs(t, true) // on, no agent polling: templates go out at once
	if !dutyChatWarn(l, "MemberY", "do ur shift", "sorry", 3) {
		t.Fatal("chat jobs on: warning handled")
	}
	got := rec.wait(t, 1)
	if len(got[0]) != 2 || got[0][0].Speaker != "LeaderX" || got[0][1].Speaker != "MemberY" || got[0][1].Channel != "guild" {
		t.Fatalf("warning template: %+v", got[0])
	}
	if w, used := dutyChatKick(l, "MemberY", "bye", "short days"); !used || w != 0 {
		t.Fatalf("kick with no agent: said at once, no wait (w=%d used=%v)", w, used)
	}
	if !dutyChatDirection(l, "guild focus: elu", &GuildDirection{Kind: "stock", Item: "Elunium"}) {
		t.Fatal("direction handled")
	}
	rec.wait(t, 3)
	chatJobsPolled(time.Now()) // an agent is polling: the kick line waits, the expel waits for its deadline
	if w, _ := dutyChatKick(l, "MemberY", "bye", "short days"); w < 40 {
		t.Fatalf("kick with an agent: expel waits past the job deadline, got %d", w)
	}
}

// guild ledger (P9, 2026-11-15): wealth must not depend on who is online this round, and directions must persist
func TestRankRoundCountsVendorsAndForgetsLeavers(t *testing.T) {
	econ.mu.Lock()
	saveS := econ.shops
	econ.shops = []shopRef{{name: "Vendor A", items: []vendListing{{ItemID: 501, Count: 3}}}}
	econ.mu.Unlock()
	grank.Lock()
	saveO, saveM, saveC := grank.owner, grank.shops, grank.cur
	grank.owner, grank.shops = map[string]ownerRec{}, map[string]shopMemo{}
	grank.Unlock()
	defer func() {
		econ.mu.Lock()
		econ.shops = saveS
		econ.mu.Unlock()
		grank.Lock()
		grank.owner, grank.shops, grank.cur = saveO, saveM, saveC
		grank.Unlock()
	}()
	want := priceOf(501) * 3
	if want <= 0 {
		t.Fatal("no price for the test item")
	}
	stockOf := func() int {
		grank.Lock()
		defer grank.Unlock()
		if r := grank.cur["guild01"]; r != nil {
			return r.Stock
		}
		return -1
	}
	vendor := func(g string) *dutySnap { return &dutySnap{name: "Vendor A", guild: g, vendor: true} }
	t0 := time.Now()
	// the vendor is online in guild01: its shop counts
	guildRankRound([]*dutySnap{vendor("guild01")}, t0, "")
	if got := stockOf(); got != want {
		t.Fatalf("online vendor: stock %d, want %d", got, want)
	}
	// its guild name reads empty for a moment (login lag): inside the grace the shop still counts
	guildRankRound([]*dutySnap{vendor("")}, t0.Add(time.Minute), "")
	if got := stockOf(); got != want {
		t.Fatalf("login lag: stock %d, want %d", got, want)
	}
	// still with no guild after the grace: it left, the shop is no longer guild01's
	guildRankRound([]*dutySnap{vendor("")}, t0.Add(10*time.Minute), "")
	if got := stockOf(); got != 0 {
		t.Fatalf("left guild, stock %d, want 0", got)
	}
}

func TestRankRoundKeepsOfflineVendorShop(t *testing.T) {
	econ.mu.Lock()
	saveS := econ.shops
	econ.shops = []shopRef{{name: "Vendor A", items: []vendListing{{ItemID: 501, Count: 3}}}}
	econ.mu.Unlock()
	grank.Lock()
	saveO, saveM, saveC := grank.owner, grank.shops, grank.cur
	grank.owner, grank.shops = map[string]ownerRec{}, map[string]shopMemo{}
	grank.Unlock()
	defer func() {
		econ.mu.Lock()
		econ.shops = saveS
		econ.mu.Unlock()
		grank.Lock()
		grank.owner, grank.shops, grank.cur = saveO, saveM, saveC
		grank.Unlock()
	}()
	want := priceOf(501) * 3
	stockOf := func() int {
		grank.Lock()
		defer grank.Unlock()
		if r := grank.cur["guild01"]; r != nil {
			return r.Stock
		}
		return -1
	}
	other := &dutySnap{name: "Other"}
	t0 := time.Now()
	guildRankRound([]*dutySnap{{name: "Vendor A", guild: "guild01", vendor: true}}, t0, "")
	// the vendor goes offline: its last shop value keeps counting for its guild for a day
	econ.mu.Lock()
	econ.shops = nil
	econ.mu.Unlock()
	guildRankRound([]*dutySnap{other}, t0.Add(time.Hour), "")
	if got := stockOf(); got != want {
		t.Fatalf("offline vendor: stock %d, want %d", got, want)
	}
	guildRankRound([]*dutySnap{other}, t0.Add(25*time.Hour), "")
	if got := stockOf(); got != 0 {
		t.Fatalf("offline a day: stock %d, want 0", got)
	}
}

func TestRankRoundKeepsWealthWithoutFeed(t *testing.T) {
	grank.Lock()
	saved := grank.cur
	grank.cur = map[string]*guildRankRow{"guild01": {Guild: "guild01", Wealth: 160514}}
	grank.Unlock()
	defer func() { grank.Lock(); grank.cur = saved; grank.Unlock() }()
	guildRankRound(nil, time.Now(), "")
	grank.Lock()
	defer grank.Unlock()
	if r := grank.cur["guild01"]; r == nil || r.Wealth != 160514 || len(grank.cur) != 1 {
		t.Fatalf("empty feed replaced the last round: %+v", grank.cur)
	}
}

func TestRankFundsSurviveRestart(t *testing.T) {
	old := *dataDir
	*dataDir = t.TempDir()
	defer func() { *dataDir = old }()
	grank.Lock()
	saveF, saveG, saveO := grank.funds, grank.fundGuild, grank.owner
	grank.funds, grank.fundGuild, grank.owner = map[string][2]int{"Leader": {7037, int(time.Now().Unix())}},
		map[string]string{"Leader": "guild01"}, map[string]ownerRec{"Vendor A": {Guild: "guild01", Seen: time.Now().Unix()}}
	grank.loaded, grank.dirty = true, true
	grank.Unlock()
	guildRankSave()
	grank.Lock()
	grank.funds, grank.fundGuild, grank.owner = map[string][2]int{}, map[string]string{}, map[string]ownerRec{}
	grank.loaded = false
	grank.Unlock()
	guildRankLoad()
	grank.Lock()
	defer grank.Unlock()
	ok := grank.funds["Leader"][0] == 7037 && grank.fundGuild["Leader"] == "guild01" && grank.owner["Vendor A"].Guild == "guild01"
	grank.funds, grank.fundGuild, grank.owner = saveF, saveG, saveO
	if !ok {
		t.Fatalf("purses or owners lost on restart: %v %v %v", grank.funds, grank.fundGuild, grank.owner)
	}
}

func TestLeaderDirectionsStackAndPersist(t *testing.T) {
	old := *dataDir
	*dataDir = t.TempDir()
	defer func() { *dataDir = old }()
	gduty.Lock()
	saveD := gduty.dirs["guild02"]
	gduty.dirs["guild02"], gduty.dirty, gduty.loaded = nil, true, true
	gduty.Unlock()
	defer func() { gduty.Lock(); gduty.dirs["guild02"] = saveD; gduty.Unlock() }()
	for _, body := range []string{`{"kind":"supply","min_lv":10,"max_lv":60,"hours":4}`, `{"kind":"wealth","hours":4}`} {
		rec := httptest.NewRecorder()
		guildDirAPI(rec, httptest.NewRequest("POST", "/api/guild-direction/guild02", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("post %s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	if act := activeDirsLocked("guild02", time.Now()); len(act) != 2 {
		t.Fatalf("two leader directions in force, got %d", len(act))
	}
	dutySave()
	gduty.Lock()
	gduty.dirs["guild02"] = nil
	gduty.loaded = false
	gduty.Unlock()
	dutyLoad()
	gduty.Lock()
	n := len(gduty.dirs["guild02"])
	gduty.dirs["guild02"] = saveD
	gduty.Unlock()
	if n != 2 {
		t.Fatalf("directions after restart: %d, want 2", n)
	}
}

func TestClearAutopilotDirectionFreesSlot(t *testing.T) {
	old := *dataDir
	*dataDir = t.TempDir()
	defer func() { *dataDir = old }()
	now := time.Now()
	gduty.Lock()
	saveA, saveD := gduty.auto["guild09"], gduty.dismissed
	gduty.auto["guild09"] = autopilotDirs("guild09", []int{5001}, false, nil, now)
	gduty.dismissed = map[string]time.Time{}
	gduty.loaded, gduty.dirty = true, true
	gduty.Unlock()
	defer func() { gduty.Lock(); gduty.auto["guild09"], gduty.dismissed = saveA, saveD; gduty.Unlock() }()
	id := "guild09-auto-stock-5001"
	if act := activeDirsLocked("guild09", now); len(act) < 1 || act[0].ID != id {
		t.Fatalf("autopilot stock not in force before the clear: %+v", act)
	}
	// an id this guild never had is refused, and records nothing
	rec := httptest.NewRecorder()
	guildDirAPI(rec, httptest.NewRequest("POST", "/api/guild-direction/guild09", strings.NewReader(`{"remove":"guild09-bogus"}`)))
	if rec.Code != 404 {
		t.Fatalf("unknown id: %d, want 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	guildDirAPI(rec, httptest.NewRequest("POST", "/api/guild-direction/guild09", strings.NewReader(`{"remove":"`+id+`"}`)))
	if rec.Code != 200 {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}
	for _, d := range activeDirsLocked("guild09", now) {
		if d.ID == id {
			t.Fatal("cleared autopilot direction still in force")
		}
	}
	// the dismissal survives a restart
	dutySave()
	gduty.Lock()
	gduty.dismissed = map[string]time.Time{}
	gduty.loaded = false
	gduty.Unlock()
	dutyLoad()
	for _, d := range activeDirsLocked("guild09", now) {
		if d.ID == id {
			t.Fatal("dismissal lost on restart")
		}
	}
}

func TestGuildLeaderSayOfflineLeader(t *testing.T) {
	l := &Bot{P: &Persona{Chars: []CharSpec{{Name: "suncat82"}}}}
	rec := httptest.NewRecorder()
	guildLeaderSay(rec, httptest.NewRequest("POST", "/api/guild-leaders/guild08/say", strings.NewReader(`{"text":"hi all"}`)), l)
	if rec.Code != 409 {
		t.Fatalf("offline leader: %d %s, want 409", rec.Code, rec.Body.String())
	}
}

func TestDutyLoadKeepsUnparsedFiles(t *testing.T) {
	old := *dataDir
	*dataDir = t.TempDir()
	defer func() { *dataDir = old }()
	unparsed := func() int {
		ents, _ := os.ReadDir(*dataDir)
		n := 0
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), "guild_duty.json.unparsed-") {
				n++
			}
		}
		return n
	}
	for i := 0; i < 2; i++ { // two bad starts: both kept, the first is not replaced
		if err := os.WriteFile(dutyFilePath(), []byte("{not json"), 0644); err != nil {
			t.Fatal(err)
		}
		dutyLoad()
	}
	if n := unparsed(); n != 2 {
		t.Fatalf("unparseable duty files kept: %d, want 2", n)
	}
}
