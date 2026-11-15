package main

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// live 2026-11-15 03:43Z: the priest held in geffen ("gef_fild08 too dangerous") while Ayla farmed it.
func TestHealerHoldsForDanger(t *testing.T) {
	if healerHoldsForDanger(0) || healerHoldsForDanger(1) {
		t.Fatal("the healer goes where the leader is unless it died there twice itself")
	}
	if !healerHoldsForDanger(2) {
		t.Fatal("two own deaths there in 10 min: hold")
	}
}

func TestHealerTripBlocked(t *testing.T) {
	if !healerTripBlocked(true, true, false) {
		t.Fatal("healer, leader out farming, bag not urgent: no solo trip")
	}
	if healerTripBlocked(true, true, true) {
		t.Fatal("an urgent bag is an emergency")
	}
	if healerTripBlocked(true, false, false) || healerTripBlocked(false, true, false) {
		t.Fatal("leader in town, or not the healer: old rules")
	}
}

func TestIsPartyLeaderName(t *testing.T) {
	if !isPartyLeaderName(&Party{Human: "Ayla"}, "Ayla") || isPartyLeaderName(&Party{Human: "Ayla"}, "Echobunny") {
		t.Fatal("human party: the human is the leader")
	}
	l := pbPriest("zz_hsleader", 0)
	if !isPartyLeaderName(&Party{Leader: l}, "zz_hsleader") || isPartyLeaderName(nil, "x") {
		t.Fatal("bot party: the leader bot")
	}
}

// Ayla got Blessing 6x / Agi 2x / Kyrie 1x in an hour, Echobunny 6x / 6x / 4x: the leader goes first, Kyrie on her
// while she fights.
func TestBuffCandsLeaderFirst(t *testing.T) {
	now := time.Now()
	b := pbPriest("zz_hspriest", 900)
	p := &Party{Name: "zz_hs pt", Human: "zz_ayla"}
	members := []pubState{
		{Name: "zz_hspriest", Map: "pay_dun01", X: 50, Y: 50, Online: true, HPFrac: 1, Support: true, Back: true},
		{Name: "zz_melee", Map: "pay_dun01", X: 52, Y: 51, Online: true, HPFrac: 1},
		{Name: "zz_ayla", Map: "pay_dun01", X: 53, Y: 50, Online: true, HPFrac: 1, Back: true},
	}
	c := b.buffCands(p, members, false, now)
	if len(c) == 0 || c[0].name != "zz_ayla" || c[0].skill != alBlessing {
		t.Fatalf("leader Blessing first, got %+v", c)
	}
	kyrie := false
	for _, x := range b.buffCands(p, members, true, now) {
		kyrie = kyrie || x.skill == prKyrie && x.name == "zz_ayla"
	}
	if !kyrie {
		t.Fatal("the leader fights: Kyrie on her")
	}
	// her ask still beats everything
	b.buffAskHeard("zz_melee : agi pls", now)
	if c := b.buffCands(p, members, false, now); c[0].name != "zz_melee" {
		t.Fatalf("an explicit ask goes first, got %+v", c[0])
	}
}

// a buff is booked for its full time only when the cast is seen landing; unconfirmed it is retried after buffPending.
func TestBuffPendingConfirm(t *testing.T) {
	now := time.Now()
	b := pbPriest("zz_hsconf", 900)
	b.rng = rand.New(rand.NewSource(1))
	b.ents[9] = &Entity{ID: 9, Type: objPC, Name: "zz_ayla2"}
	k := fmt.Sprintf("%d:zz_ayla2", alBlessing)
	if !b.buffBookPending(alBlessing, "zz_ayla2", now) {
		t.Fatal("first cast: pending")
	}
	if got := b.buffAt[k].Sub(now); got != buffPending {
		t.Fatalf("pending booking %v, want %v", got, buffPending)
	}
	pk := make([]byte, 17)
	binary.LittleEndian.PutUint16(pk, 0x09cb)
	binary.LittleEndian.PutUint16(pk[2:], alBlessing)
	binary.LittleEndian.PutUint32(pk[4:], 10)
	binary.LittleEndian.PutUint32(pk[8:], 9)
	binary.LittleEndian.PutUint32(pk[12:], 1) // src = the priest (aid 1)
	b.buffRecv(0x09cb, pk, now.Add(time.Second))
	if !b.buffAt[k].After(now.Add(200 * time.Second)) {
		t.Fatalf("confirmed: full booking, got %v", b.buffAt[k].Sub(now))
	}
	// never confirmed: three short retries, then the old full booking (no endless spam)
	for i := 1; i <= buffMaxTries; i++ {
		if !b.buffBookPending(alIncAgi, "zz_ayla2", now) {
			t.Fatalf("try %d should be pending", i)
		}
	}
	if b.buffBookPending(alIncAgi, "zz_ayla2", now) {
		t.Fatal("after buffMaxTries unconfirmed casts: book in full")
	}
}

// QA 03:55Z: Ayla in geffen, the friends sat in geffen (meeting town of orcsdun02) after she left for gef_fild10.
func TestHumanMemberTargetFollowsHerOut(t *testing.T) {
	if got := humanMemberTarget("orcsdun02", "geffen", "prontera", "geffen", true, true); got != "geffen" {
		t.Fatalf("she is in the meeting town: go there, got %q", got)
	}
	if got := humanMemberTarget("orcsdun02", "gef_fild10", "geffen", "geffen", false, true); got != "gef_fild10" {
		t.Fatalf("she left the meeting town: her map, got %q", got)
	}
}

// live 2026-11-15: with a member always under 80% HP every skill slot went to Heal and Ayla's buffs lapsed.
func TestLeaderBuffDueBeatsLightHeals(t *testing.T) {
	now := time.Now()
	b := pbPriest("zz_hslb", 900)
	p := &Party{Name: "zz_hslb pt", Human: "zz_ayla3"}
	members := []pubState{
		{Name: "zz_hslb", Map: "pay_dun01", X: 50, Y: 50, Online: true, HPFrac: 1, Support: true, Back: true},
		{Name: "zz_ayla3", Map: "pay_dun01", X: 54, Y: 50, Online: true, HPFrac: 0.7},
	}
	if !b.leaderBuffDue(p, members, now) {
		t.Fatal("Blessing on the leader is due and affordable")
	}
	b.buffAt[fmt.Sprintf("%d:zz_ayla3", alBlessing)] = now.Add(time.Minute)
	b.buffAt[fmt.Sprintf("%d:zz_ayla3", alIncAgi)] = now.Add(time.Minute)
	if b.leaderBuffDue(p, members, now) {
		t.Fatal("both buffs running: not due")
	}
	members[1].X = 70
	delete(b.buffAt, fmt.Sprintf("%d:zz_ayla3", alBlessing))
	if b.leaderBuffDue(p, members, now) {
		t.Fatal("out of reach: not due")
	}
}

// owner 2026-11-15: "the priest can heal itself, why resting?"
func TestSelfHealInsteadOfResting(t *testing.T) {
	now := time.Now()
	b := pbPriest("zz_hsself", 800)
	b.rng = rand.New(rand.NewSource(1))
	b.HP = 400 // 40%
	if !b.selfHealOK() {
		t.Fatal("Heal learned, SP to spare: heal, don't sit")
	}
	if !b.selfHealInstead(b.hpFrac(), 0.5, now) || b.wantSit || b.State != "healing myself (40%)" {
		t.Fatalf("under the rest line: heals itself, state %q wantSit %v", b.State, b.wantSit)
	}
	b.Sitting = true
	b.selfHeal(now.Add(2 * time.Second))
	if b.State != "standing up to heal myself" {
		t.Fatalf("seated: stands up to heal, got %q", b.State)
	}
	b.Sitting, b.SP = false, 50 // 5%: no SP for heals
	if b.selfHealOK() && b.hpFrac() >= 0.4 {
		t.Fatal("SP gone: it may sit for SP")
	}
	m := pbPriest("zz_hsmonk", 800)
	m.skills = map[int]*SkillInfo{}
	if m.selfHealOK() {
		t.Fatal("no Heal learned: old rest")
	}
	if got := b.priestSitLine(pubState{Name: "zz_l", X: 70, Y: 50}); got != healerMoveSitSP {
		t.Fatalf("leader walking off: sit line %d, want %d", got, healerMoveSitSP)
	}
}

func TestSelfKyrie(t *testing.T) {
	now := time.Now()
	b := pbPriest("zz_hskyrie", 900)
	if !b.selfKyrie(now) || b.State != "Kyrie Eleison on myself" {
		t.Fatalf("no Kyrie on us in the field: cast it, state %q", b.State)
	}
	b.skillReady, b.buffAt["buffgap"] = time.Time{}, time.Time{}
	if b.selfKyrie(now.Add(3 * time.Second)) {
		t.Fatal("a cast is on its way: no second one")
	}
	b.buffAt["got:73"] = now.Add(100 * time.Second)
	if b.selfKyrie(now.Add(9 * time.Second)) {
		t.Fatal("Kyrie on: no recast")
	}
	delete(b.buffAt, "got:73") // broke (status icon off)
	if !b.selfKyrie(now.Add(9 * time.Second)) {
		t.Fatal("broken: recast")
	}
	b.skillReady, b.buffAt["buffgap"], b.buffAt["kyrieself"] = time.Time{}, time.Time{}, time.Time{}
	b.HP = 300
	if b.selfKyrie(now.Add(20 * time.Second)) {
		t.Fatal("under 50% HP: heal first")
	}
	b.HP, b.MapName = 1000, "prontera"
	if b.selfKyrie(now.Add(20 * time.Second)) {
		t.Fatal("no Kyrie in town")
	}
}

// owner 2026-11-15: "monk should also know to buff and heal themselves" (LilLilyblade lv81: Heal 3, Agi 10, Blessing 6)
func TestMonkSelfBuffAndHeal(t *testing.T) {
	now := time.Now()
	m := &Bot{P: &Persona{Name: "zz_hsmonk2", Chars: []CharSpec{{Name: "zz_hsmonk2"}}}, Class: 15, HP: 1000, MaxHP: 1000,
		SP: 600, MaxSP: 1000, MapName: "gef_fild10", X: 50, Y: 50, aid: 5, rng: rand.New(rand.NewSource(1)),
		skills: map[int]*SkillInfo{alHeal: {Lv: 3, SP: 19}, alBlessing: {Lv: 6, SP: 40}, alIncAgi: {Lv: 10, SP: 45}},
		buffAt: map[string]time.Time{}, ents: map[uint32]*Entity{}}
	if m.isHealer() {
		t.Skip("class 15 counts as a healer here: test needs a Monk")
	}
	if !m.selfBuffs(now) || m.State != "Blessing on myself" {
		t.Fatalf("no Blessing on us: cast it, state %q", m.State)
	}
	m.skillReady, m.buffAt["selfbuffgap"] = time.Time{}, time.Time{}
	if !m.selfBuffs(now.Add(3*time.Second)) || m.State != "Increase AGI on myself" {
		t.Fatalf("then Increase AGI, state %q", m.State)
	}
	m.skillReady, m.buffAt["selfbuffgap"] = time.Time{}, time.Time{}
	m.buffAt["got:34"] = now.Add(200 * time.Second)
	m.buffAt["got:29"] = now.Add(200 * time.Second)
	if m.selfBuffs(now.Add(5 * time.Second)) {
		t.Fatal("both on: nothing to cast")
	}
	delete(m.buffAt, "got:34") // Blessing icon off
	delete(m.buffAt, "selfcast:34")
	m.SP = 370 // 37%: a cast would leave under the 35% combo floor
	if m.selfBuffs(now.Add(5 * time.Second)) {
		t.Fatal("combo SP floor: no buff")
	}
	m.SP, m.HP = 800, 300
	if !m.selfHealOK() {
		t.Fatal("a Monk with Heal heals itself instead of resting")
	}
	m.MapName = "geffen"
	m.HP = 1000
	if m.selfBuffs(now.Add(10 * time.Second)) {
		t.Fatal("no self-buffs in town")
	}
}
