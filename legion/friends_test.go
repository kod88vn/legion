package main

import (
	"testing"
	"time"
)

func resetFriends() {
	friends.Lock()
	friends.pairs, friends.peer, friends.bonus = map[string]*friendPair{}, map[string]*friendPair{}, map[string]float64{}
	friends.counters, friends.promos, friends.moves = map[string]int{}, nil, nil
	friends.Unlock()
}

func TestFriendScoreGrowsAndTiers(t *testing.T) {
	resetFriends()
	now := time.Now()
	friends.Lock()
	for i := 0; i < 120; i++ { // two hours partied on the same map
		friendAddLocked("Ann", "Bob", fkParty, 1, now)
	}
	friends.Unlock()
	s := FriendScore("Bob", "Ann")
	if s < friendTierAcq || s > friendTierClose {
		t.Fatalf("2 h partied: score %.1f, want acquaintance..friend", s)
	}
	if FriendScore("Ann", "Bob") != s {
		t.Fatalf("score must be symmetric")
	}
	friends.Lock()
	for i := 0; i < 2000; i++ {
		friendAddLocked("Ann", "Bob", fkParty, 1, now)
	}
	sc := friends.pairs["Ann|Bob"].Score
	promos := len(friends.promos)
	friends.Unlock()
	if sc > 100 || friendTier(sc) != "close friend" || promos != 2 {
		t.Fatalf("long partying: score %.1f tier %q promos %d", sc, friendTier(sc), promos)
	}
	// a rival note pulls it down, and the chat rate limit holds
	friends.Lock()
	friends.bonus["Ann|Bob"] = -30
	friends.Unlock()
	if FriendScore("Ann", "Bob") >= sc {
		t.Fatalf("rival note must reduce the score")
	}
	friendNote("Cat", "Dan", fkChat)
	friendNote("Dan", "Cat", fkChat)
	friends.Lock()
	chats := friends.pairs["Cat|Dan"].Chats
	friends.Unlock()
	if chats != 1 {
		t.Fatalf("chat within 2 min counted %d times, want 1", chats)
	}
}

func TestFriendDecay(t *testing.T) {
	resetFriends()
	old := time.Now().Add(-8 * 24 * time.Hour)
	friends.Lock()
	friends.pairs["Ann|Bob"] = &friendPair{A: "Ann", B: "Bob", Score: 60, Last: old, decayAt: old.Add(-friendHalfLife + 8*24*time.Hour - friendHalfLife)}
	friends.pairs["Ann|Bob"].decayAt = time.Now().Add(-friendHalfLife)
	friends.Unlock()
	friendsDecay(time.Now())
	friends.Lock()
	s := friends.pairs["Ann|Bob"].Score
	friends.Unlock()
	if s < 29 || s > 31 {
		t.Fatalf("one half-life without contact: %.1f, want ~30", s)
	}
}

func TestFriendPeerMerge(t *testing.T) {
	resetFriends()
	friends.Lock()
	friends.peer["Ann|Zed"] = &friendPair{A: "Ann", B: "Zed", Score: 50}
	friends.Unlock()
	if s := FriendScore("Zed", "Ann"); s != 50 {
		t.Fatalf("peer instance pair: %.1f, want 50", s)
	}
	if fl := friendsOf("Ann", 10); len(fl) != 1 || fl[0].Name != "Zed" {
		t.Fatalf("friendsOf: %+v", fl)
	}
}

func TestLoyaltyCareAndDecay(t *testing.T) {
	loyalty.Lock()
	loyalty.m = map[string]*guildLoyal{}
	loyalty.Unlock()
	if l := loyaltyOf("Ann", "Kitty Cafe"); l != 50 {
		t.Fatalf("unknown member: %.1f, want 50", l)
	}
	loyaltyCare("Ann", "Kitty Cafe", 8, "answered", 0)
	loyaltyCare("Ann", "Kitty Cafe", 6, "gift", 0)
	up := loyaltyOf("Ann", "Kitty Cafe")
	if up <= 55 || up > 100 {
		t.Fatalf("after help + gift: %.1f", up)
	}
	loyaltyCare("Ann", "Kitty Cafe", 0.15, "heal", time.Minute)
	loyaltyCare("Ann", "Kitty Cafe", 0.15, "heal", time.Minute) // rate-limited
	loyaltyCare("Ann", "Kitty Cafe", -5, "ignored", 0)
	if l := loyaltyOf("Ann", "Kitty Cafe"); l >= up+0.3 || l > up {
		t.Fatalf("ignored request must lower loyalty: %.1f vs %.1f", l, up)
	}
	loyalty.Lock()
	r := loyalty.m["Ann"]
	r.LastCare, r.decayAt = time.Now().Add(-72*time.Hour), time.Now().Add(-48*time.Hour)
	before := r.Loyalty
	loyalty.Unlock()
	loyaltyTenMin(nil, time.Now())
	if l := loyaltyOf("Ann", "Kitty Cafe"); l > before-5.9 || l < before-6.1 {
		t.Fatalf("2 days of neglect: %.1f -> %.1f, want -6", before, l)
	}
	if l := loyaltyOf("Ann", "Moonlit Bazaar"); l != 50 {
		t.Fatalf("another guild starts fresh: %.1f", l)
	}
}

func TestHelpGivePoorServer(t *testing.T) {
	// live 14:00 asks: 201z with a 6,470z mate = full; 6,286z with a 4,018z mate = 509 (not worth a partial)
	if g, full := helpGive(6470, 201, 0); !full || g != 201 {
		t.Fatalf("small ask: %d %v", g, full)
	}
	if g, full := helpGive(4018, 6286, 0); full || helpPartialOK(g, 6286) {
		t.Fatalf("big ask, poor mate: %d %v", g, full)
	}
	if g, _ := helpGive(6266, 3277, 0); !helpPartialOK(g, 3277) {
		t.Fatalf("partial chip-in expected: %d", g)
	}
	if g, _ := helpGive(2500, 200, 0); g > 0 {
		t.Fatalf("a mate under the 3k floor gives nothing: %d", g)
	}
	if g, _ := helpGive(10000, 5000, 2900); g > 100 {
		t.Fatalf("daily 30%% cap: %d", g)
	}
}

func TestMemoryBlocksTestAccounts(t *testing.T) {
	for _, n := range []string{"AIBot", "aibot", "Observer", "[Global] Ayla", "Resting Adventurer", "Trailmark", "SomeBot99", ""} {
		if memoryNameOK(n) {
			t.Fatalf("%q must not be remembered", n)
		}
	}
	if !memoryNameOK("Ayla") || !memoryNameOK("Echobunny") {
		t.Fatalf("real names must be remembered")
	}
	m := &Memory{People: map[string]*Person{"AIBot": {}, "Ayla": {}}, Diary: []DiaryEntry{{Text: "AIBot whispered me: hi"}, {Text: "levelled up"}}}
	if !m.memoryClean() || len(m.People) != 1 || len(m.Diary) != 1 {
		t.Fatalf("clean: %v %v", m.People, m.Diary)
	}
	m.meet("AIBot", "", "izlude", true)
	if len(m.People) != 1 {
		t.Fatalf("meet recorded a blocked name")
	}
	resetFriends()
	friendNote("AIBot", "Ann", fkChat)
	if FriendScore("AIBot", "Ann") != 0 {
		t.Fatalf("friendship with a blocked name")
	}
}
