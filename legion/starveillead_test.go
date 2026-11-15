package main

import (
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
