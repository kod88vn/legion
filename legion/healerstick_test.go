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
