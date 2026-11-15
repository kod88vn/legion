package main

import (
	"strings"
	"testing"
	"time"
)

func TestRecruitJob(t *testing.T) {
	cases := []struct {
		class  int
		key    int
		second bool
	}{
		{7, 7, true}, {13, 7, true}, {4008, 7, true}, {4014, 7, true}, {21, 14, true}, {4022, 14, true},
		{12, 12, true}, {4013, 12, true}, {1, 1, false}, {4002, 1, false}, {0, 0, false}, {4001, 0, false},
	}
	for _, c := range cases {
		k, s := recruitJob(c.class)
		if k != c.key || s != c.second {
			t.Errorf("recruitJob(%d) = %d,%v want %d,%v", c.class, k, s, c.key, c.second)
		}
	}
}

func TestPickGuildRecruits(t *testing.T) {
	mk := func(name string, class, lv int, friend bool) guildRecruit {
		k, s := recruitJob(class)
		return guildRecruit{Name: name, Class: className(class), Level: lv, Friend: friend, key: k, second: s, account: name}
	}
	// Ayla is an Assassin (dps); the guild has a Knight (tank) and a Priest (healer)
	members := []guildMemberRow{{Name: "k", Class: "Knight", Level: 70, key: 7}, {Name: "p", Class: "Priest", Level: 70, key: 8}}
	cands := []guildRecruit{
		mk("friend", 18, 72, true),  // party friend first (craft)
		mk("wizNear", 9, 77, false), // aoe gap 3: biggest
		mk("wizFar", 9, 55, false),  // same job, further level
		mk("sage", 16, 60, false),   // aoe, a job the guild lacks: before a 2nd Wizard
		mk("knight2", 7, 78, false), // tank gap 2
		mk("crus", 14, 70, false),   // tank, new job: before a 2nd Knight
		mk("rogue", 17, 78, false),  // dps gap 1
		mk("hunter", 11, 75, false), // ranged gap 3
	}
	got, gaps := pickGuildRecruits(members, cands, 12, 78, 4)
	names := []string{}
	for _, g := range got {
		names = append(names, g.Name)
	}
	// friend; then aoe (gap 3, first in target order among the 3-gaps aoe/ranged): wizNear; then ranged (3): hunter;
	// then aoe/ranged/tank all at 2: tank first in target order -> crus (a job the guild lacks)
	want := []string{"friend", "wizNear", "hunter", "crus"}
	if len(names) != len(want) {
		t.Fatalf("got %v want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("got %v want %v", names, want)
		}
	}
	if gaps["aoe"] != 2 || gaps["tank"] != 1 || gaps["healer"] != 2 {
		t.Errorf("gaps %v", gaps)
	}
	// guild size cap: 15 members + leader = full
	full := make([]guildMemberRow, 15)
	if got, _ := pickGuildRecruits(full, cands, 12, 78, 5); len(got) != 0 {
		t.Errorf("full guild still recruits %v", got)
	}
}

func TestGuildPlan(t *testing.T) {
	now := time.Now()
	ms := []guildMemberInfo{
		{Name: "alch", Class: "Alchemist", Role: "craft", Level: 77, Online: true, Zeny: 77000, Spare: 0, Brewer: true, Friend: true, InGuild: true},
		{Name: "rich", Class: "Knight", Role: "tank", Level: 76, Online: true, Zeny: 34000, Spare: 30000, Weak: 5, InGuild: true,
			Next: &gearStep{Item: "Claymore", Cost: 9000, Have: 16000, Shop: "prontera"}},
		{Name: "poor", Class: "Wizard", Role: "aoe", Level: 75, Online: true, Zeny: 900, Spare: 400, Weak: 1, InGuild: true,
			Next: &gearStep{Item: "Arc Wand", Cost: 4000, Have: 400, Shop: "geffen"}},
		{Name: "low", Class: "Priest", Role: "healer", Level: 50, Online: true, InGuild: true},
	}
	week := &guildWeek{Start: now.Add(-time.Hour), StartLv: map[string]int{"rich": 72, "poor": 75}}
	acts, extra := guildPlan("Starveil", "Ayla", ms, nil, map[string]int{"tank": 2}, week, now)
	kinds := map[string]string{}
	for _, a := range acts {
		kinds[a.Kind] += a.What + " | " + a.Cmd
	}
	if !strings.Contains(kinds["guide"], "alch") || !strings.Contains(kinds["guide"], "guild-fund") {
		t.Errorf("no guide funding: %v", kinds)
	}
	if !strings.Contains(kinds["fund"], "poor") || !strings.Contains(kinds["fund"], "3600") {
		t.Errorf("weakest member not funded: %q", kinds["fund"])
	}
	if !strings.Contains(kinds["carry"], "low") {
		t.Errorf("low member not flagged: %v", kinds)
	}
	if _, ok := kinds["found"]; ok {
		t.Errorf("founded guild asked to found again")
	}
	w := extra["week"].(map[string]any)
	if w["members_done"].(int) != 1 || w["levels_gained"].(int) != 4 {
		t.Errorf("week %v", w)
	}
	if acts, _ := guildPlan("", "Ayla", nil, nil, nil, &guildWeek{Start: now}, now); acts[0].Kind != "found" {
		t.Errorf("unfounded guild: first action %v", acts[0])
	}
}

func TestPickGuildTrader(t *testing.T) {
	members := []guildMemberRow{{Name: "k", Class: "Knight", Level: 70, key: 7}}
	cands := []guildRecruit{
		{Name: "wiz", Class: "Wizard", Level: 77, key: 9, second: true},
		{Name: "shop", Class: "Blacksmith", Level: 55, Trader: true, key: 10, second: true},
	}
	got, gaps := pickGuildRecruits(members, cands, 12, 78, 1)
	if len(got) != 1 || got[0].Name != "shop" {
		t.Fatalf("trader not first: %v", got)
	}
	if gaps["trader"] != 0 {
		t.Errorf("trader gap left: %v", gaps)
	}
	// a guild with a trader: the vendor counts as its class role (craft), the wizard fills aoe first
	members = append(members, guildMemberRow{Name: "v", Class: "Blacksmith", Level: 60, Trader: true, key: 10})
	got, _ = pickGuildRecruits(members, cands, 12, 78, 1)
	if len(got) != 1 || got[0].Name != "wiz" {
		t.Fatalf("got %v", got)
	}
}

func TestTraderShiftNotTrader(t *testing.T) {
	// a bot that isn't Starveil's trader never starts a shift (and never blocks on the trader pick)
	b := &Bot{P: &Persona{Chars: []CharSpec{{Name: "x", Role: "main"}}}}
	if b.traderShift(false, time.Now()) {
		t.Fatal("non-trader started a shop shift")
	}
}

func TestLoyaltyActions(t *testing.T) {
	loyalty.Lock()
	oldR, oldH := loyalty.risk, loyalty.help
	loyalty.risk = []atRisk{{Char: "m1", Guild: "Starveil", Loyalty: 22, PullTo: "Kitty Cafe", Action: `ayla.sh guild-fund "m1" 3000 "keep: next gear"`},
		{Char: "x", Guild: "Iron Wolves", Loyalty: 10}}
	loyalty.help = []*helpReq{{Asker: "m2", Guild: "Starveil", Kind: "pots", Amount: 1500, Text: "out of pots", Map: "pay_dun01", Lv: 60, State: "open"},
		{Asker: "m3", Guild: "Starveil", Kind: "rescue", Map: "gl_cas01", State: "answered"}}
	loyalty.Unlock()
	defer func() { loyalty.Lock(); loyalty.risk, loyalty.help = oldR, oldH; loyalty.Unlock() }()
	acts := loyaltyActions("Starveil")
	if len(acts) != 2 || acts[0].Kind != "keep" || !strings.Contains(acts[0].Cmd, "m1") ||
		acts[1].Kind != "help" || !strings.Contains(acts[1].Cmd, `"m2" 1500`) {
		t.Fatalf("got %+v", acts)
	}
}

func TestFundShare(t *testing.T) {
	// saving for its own 9.5k Staff with 10k spare: nothing to give (the Zen.fox bug)
	if g := fundShare(guildMemberInfo{Online: true, Spare: 10000, Next: &gearStep{Cost: 9500}}); g != 0 {
		t.Errorf("saver gives %d", g)
	}
	if g := fundShare(guildMemberInfo{Online: true, Spare: 40000, Next: &gearStep{Cost: 9500, Turn: true}}); g != 0 {
		t.Errorf("gear-turn member gives %d", g)
	}
	if g := fundShare(guildMemberInfo{Online: true, Friend: true, Spare: 40000}); g != 0 {
		t.Errorf("party friend gives %d", g)
	}
	if g := fundShare(guildMemberInfo{Online: true, Spare: 20000, Next: &gearStep{Cost: 4000}}); g != 5000 {
		t.Errorf("surplus member gives %d, want 5000", g)
	}
}
