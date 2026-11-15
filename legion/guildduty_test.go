package main

import (
	"encoding/binary"
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
	for i, e := range []dutyExemptIn{{Leader: true}, {Alchemist: true}, {Trader: true}, {AylaParty: true}, {Human: true}} {
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
