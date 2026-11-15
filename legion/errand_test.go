package main

import (
	"testing"
	"time"
)

// Owner 2026-11-14: a member may leave briefly for a necessary build/class step (Peco rental). A party Knight with
// Riding and the money leaves on an errand when nothing is going on; the leader doesn't; a member on an errand is
// never collected, never waited for in a rest, and a cooldown keeps it from looping.
func TestErrandPecoMember(t *testing.T) {
	lead := &Bot{P: &Persona{Name: "zz_errlead", Chars: []CharSpec{{Name: "zz_errlead"}}}, buffAt: map[string]time.Time{}}
	k := &Bot{P: &Persona{Name: "zz_errknight", BaseLevel: 60, Chars: []CharSpec{{Name: "zz_errknight"}}}, Class: 7, invSeen: true,
		optionSeen: true, Zeny: 9000, MapName: "pay_fild04", HP: 1000, MaxHP: 1000, skills: map[int]*SkillInfo{63: {Lv: 1}},
		buffAt: map[string]time.Time{}, inv: map[int]*InvItem{}, equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
	p := &Party{Name: "zz_errparty", Leader: lead, Members: []*Bot{lead, k}}
	parties.Lock()
	parties.ofBot[lead], parties.ofBot[k] = p, p
	parties.Unlock()
	defer func() {
		parties.Lock()
		delete(parties.ofBot, lead)
		delete(parties.ofBot, k)
		parties.Unlock()
	}()
	now := time.Now()
	if !k.errandCanLeave(p, now) {
		t.Fatal("an idle party member may leave on an errand")
	}
	if kind, _ := k.errandNeed(now); kind != "peco" {
		t.Fatalf("errandNeed = %q, want peco", kind)
	}
	lead.Class, lead.MapName, lead.Zeny, lead.optionSeen, lead.skills = 7, "pay_fild04", 9000, true, map[int]*SkillInfo{63: {Lv: 1}}
	if lead.errandCanLeave(p, now) {
		t.Fatal("the bot leader never leaves on an errand (its Peco rides on a party run)")
	}
	k.buffAt["errand:cd"] = now.Add(errandCooldown)
	if k.errandCanLeave(p, now) {
		t.Fatal("cooldown: one errand per 2 h")
	}
	delete(k.buffAt, "errand:cd")
	k.target = 77
	if k.errandCanLeave(p, now) {
		t.Fatal("not mid-fight")
	}
	k.target = 0
	k.MapName = "prontera"
	if kind, _ := k.errandNeed(now); kind != "" {
		t.Fatal("already in Prontera: peco.go rents it here, no errand")
	}
	// a member on an errand is not collected and not waited for
	s := pubState{Name: "zz_errknight", Map: "pay_fild04", X: 100, Y: 100, Online: true, Errand: true, Resting: true}
	if needsCollect("pay_fild04", 10, 10, s, false) {
		t.Fatal("never collect a member on an errand")
	}
	if _, ok := partyRester([]pubState{s}, "zz_errlead", "pay_fild04", 100, 105); ok {
		t.Fatal("no party rest held for a member on an errand")
	}
	s.Errand = false
	if !needsCollect("pay_fild04", 10, 10, s, false) {
		t.Fatal("a straggler without an errand is collected")
	}
}
