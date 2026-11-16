package main

import (
	"testing"
	"time"
)

func TestPitchRefusalOnlyForTheCell(t *testing.T) {
	now := time.Now()
	if !pitchRefusal(useskillFailNPCAround, time.Time{}, now) {
		t.Fatal("NPC around at cast time must move the vendor")
	}
	if !pitchRefusal(useskillFailLevel, time.Time{}, now) {
		t.Fatal("cause 0 at cast time (no-vending cell) must move the vendor")
	}
	if pitchRefusal(1, time.Time{}, now) { // SP shortage
		t.Fatal("SP shortage is not the pitch")
	}
	if pitchRefusal(useskillFailLevel, now.Add(-2*time.Second), now) {
		t.Fatal("a refusal right after the shop list is the shop's, not the pitch's")
	}
	if !pitchRefusal(useskillFailNPCAround, now.Add(-time.Minute), now) {
		t.Fatal("an old shop list must not hide a real cell refusal")
	}
	if vendSkillCause([]byte{0x10, 0x01, 41, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 83}) != 83 || vendSkillCause([]byte{1, 2}) != -1 {
		t.Fatal("cause byte at offset 13")
	}
}

func withMarket(t *testing.T, free [][2]int) {
	market.Lock()
	oldFree, oldTaken, oldSeen := market.free, market.taken, pitchSeen
	market.free, market.taken, pitchSeen = free, map[*Bot][2]int{}, map[*Bot]time.Time{}
	market.Unlock()
	t.Cleanup(func() {
		market.Lock()
		market.free, market.taken, pitchSeen = oldFree, oldTaken, oldSeen
		market.Unlock()
	})
}

func TestStalePitchReclaimed(t *testing.T) {
	withMarket(t, nil)
	now := time.Now()
	away, here := &Bot{}, &Bot{}
	market.Lock()
	market.taken[away], market.taken[here] = [2]int{133, 45}, [2]int{136, 45}
	pitchSeen[away], pitchSeen[here] = now.Add(-10*time.Minute), now.Add(-30*time.Second)
	n := reclaimStalePitchesLocked(now)
	_, awayHeld := market.taken[away]
	_, hereHeld := market.taken[here]
	free := len(market.free)
	market.Unlock()
	if n != 1 || awayHeld || !hereHeld || free != 1 {
		t.Fatalf("reclaimed %d, away held %v, here held %v, free %d", n, awayHeld, hereHeld, free)
	}
}

func TestNotePitchRetakesOrDrops(t *testing.T) {
	withMarket(t, [][2]int{{139, 48}})
	now := time.Now()
	b := &Bot{vend: &VendState{Spot: [2]int{139, 48}}}
	b.notePitch(now) // its old pitch is free again: retaken
	market.Lock()
	p, ok := market.taken[b]
	free := len(market.free)
	market.Unlock()
	if !ok || p != [2]int{139, 48} || free != 0 {
		t.Fatalf("retake: %v %v free %d", p, ok, free)
	}
	c := &Bot{vend: &VendState{Spot: [2]int{139, 48}}} // someone else stands there now
	c.notePitch(now)
	if c.vend.Spot != ([2]int{}) {
		t.Fatal("a closed shop whose pitch is taken must look for another")
	}
	d := &Bot{vend: &VendState{Spot: [2]int{139, 48}, Opened: true}}
	d.notePitch(now)
	if d.vend.Spot == ([2]int{}) {
		t.Fatal("an open shop stays where it is")
	}
}

func TestCartLoadStuckResetsThenPauses(t *testing.T) {
	b := &Bot{P: &Persona{Chars: []CharSpec{{Name: "VendPitchTest"}}}, vend: &VendState{}}
	now := time.Now()
	for i := 1; i < cartLoadReset; i++ {
		if b.cartLoadStuck(now) {
			t.Fatalf("acted after %d tries", i)
		}
	}
	if !b.cartLoadStuck(now) || b.vend.loadTries != cartLoadReset {
		t.Fatal("no reset at cartLoadReset")
	}
	if b.vendPaused(now) {
		t.Fatal("paused too early")
	}
	b.vend.pauseUntil = now.Add(time.Minute)
	if !b.vendPaused(now) {
		t.Fatal("pause not seen")
	}
}

func TestShopReservedSkipsPromisedStock(t *testing.T) {
	now := time.Now()
	vendor, buyer, done := &Bot{}, &Bot{}, &Bot{}
	vendBuys.Store(buyer, &vendBuy{Vendor: vendor, ItemID: 2401, Count: 1, Deadline: now.Add(time.Minute)})
	ended := &vendBuy{Vendor: vendor, ItemID: 2401, Count: 1, Deadline: now.Add(time.Minute)}
	ended.ended.Store(true)
	vendBuys.Store(done, ended)
	t.Cleanup(func() { vendBuys.Delete(buyer); vendBuys.Delete(done) })
	if n := shopReserved(vendor, 2401, now); n != 1 {
		t.Fatalf("reserved %d, want 1", n)
	}
	left := shopAvailable(vendor, []vendListing{{ItemID: 2401, Count: 1, Price: 250}, {ItemID: 512, Count: 5, Price: 9}}, now)
	if len(left) != 1 || left[0].ItemID != 512 {
		t.Fatalf("available %v: the one Sandals is promised", left)
	}
	if shopReserved(vendor, 2401, now.Add(2*time.Minute)) != 0 {
		t.Fatal("an expired trip reserves nothing")
	}
}
