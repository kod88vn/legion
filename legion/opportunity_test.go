package main

import (
	"testing"
	"time"
)

// economy run 8: healing items are the market's first goods; a missing item is a money opportunity whose score falls
// as bots take it up (no herding); market talk is rate-limited server-wide.
func TestMarketOpportunities(t *testing.T) {
	items[itemYellowPotion] = &Item{ID: itemYellowPotion, Name: "Yellow Potion", Type: "Healing", Buy: 550, Weight: 13}
	items[512] = &Item{ID: 512, Name: "Apple", Type: "Healing", Buy: 15, Weight: 2}
	items[909] = &Item{ID: 909, Name: "Jellopy", Type: "Etc", Buy: 6, Weight: 1}
	if !healItem(itemYellowPotion) || !healItem(512) || healItem(909) || healItem(569) {
		t.Fatal("healItem")
	}
	inv := map[int]*InvItem{1: {Index: 1, ItemID: 909, Count: 3}, 2: {Index: 2, ItemID: 512, Count: 5}, 0: {Index: 0, ItemID: 909, Count: 1}}
	if h := healFirst(inv); h[0].ItemID != 512 || h[1].Index != 0 {
		t.Fatalf("healFirst order: %d, %d", h[0].ItemID, h[1].Index)
	}
	now := time.Now()
	opps.Lock()
	opps.at, opps.chase = time.Time{}, map[int]map[*Bot]time.Time{}
	opps.Unlock()
	score := func() float64 {
		opps.Lock()
		opps.at = time.Time{}
		opps.Unlock()
		for _, o := range marketOpps(now) {
			if o.ID == itemYellowPotion {
				return o.Score
			}
		}
		return 0
	}
	s0 := score()
	if s0 <= 0 {
		t.Fatal("a healing item nobody lists is an opportunity")
	}
	for _, o := range marketOpps(now) {
		if o.ID == 909 {
			t.Fatal("NPC junk is no opportunity")
		}
	}
	opps.Lock()
	opps.chase[itemYellowPotion] = map[*Bot]time.Time{}
	for i := 0; i < 4; i++ {
		opps.chase[itemYellowPotion][&Bot{}] = now.Add(time.Hour)
	}
	opps.Unlock()
	if s1 := score(); s1 >= s0 {
		t.Fatalf("4 chasers: score %v, want below %v", s1, s0)
	}
	// talk: at most one line per oppSayGap server-wide
	opps.Lock()
	opps.lastSay = now
	line, _ := marketTalkLocked(marketOptsRows(), now.Add(10*time.Second))
	opps.chase = map[int]map[*Bot]time.Time{}
	opps.Unlock()
	if line != "" {
		t.Fatalf("talked again 10 s after the last line: %q", line)
	}
}

func marketOptsRows() []oppItem { return opps.rows }
