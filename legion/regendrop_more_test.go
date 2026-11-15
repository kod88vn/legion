package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// regendrop_more_test.go: the review fixes for the town drop (visit reset, memory pruning, guild wants, party loot).

// A new visit (a different town than the one the count is on) starts at 0: the cap does not outlive the visit.
func TestRegenDropVisitResetsOnNewVisit(t *testing.T) {
	regenItems()
	withTownDrop(t, true)
	saved := shopsByMap
	t.Cleanup(func() { shopsByMap = saved })
	shopsByMap = map[string][]*Shop{}
	b := overweightTown(map[int]*InvItem{10: {Index: 10, ItemID: testJunkID, Count: 25}})
	b.Zeny = 0
	b.rdrop.town, b.rdrop.visit = "aldebaran", regenDropCap
	b.MapName = "payon" // back in a town after leaving: a new visit
	if !hookDrops(b, time.Now()) {
		t.Fatal("a new visit must drop again (the cap is per visit)")
	}
	if b.rdrop.visit != 0 || b.rdrop.town != "payon" {
		t.Fatalf("the visit must reset on the new town: visit %d town %q", b.rdrop.visit, b.rdrop.town)
	}
}

// Memories expire: an own drop older than its lifetime is forgotten, a refusal older than 30 min is retried, and
// both are capped per bot.
func TestRegenMemoryPrunedAndExpires(t *testing.T) {
	regenItems()
	withTownDrop(t, true)
	saved := shopsByMap
	t.Cleanup(func() { shopsByMap = saved })
	shopsByMap = map[string][]*Shop{}
	b := overweightTown(map[int]*InvItem{10: {Index: 10, ItemID: testJunkID, Count: 25}})
	b.Zeny = 0
	b.rdrop.town = b.MapName
	now := time.Now()
	b.rdrop.refused = map[string]time.Time{"10:" + strconv.Itoa(testJunkID): now.Add(-regenRefusedLife - time.Minute)}
	b.rdrop.own = []regenOwnDrop{{itemID: testJunkID, x: 1, y: 1, at: now.Add(-regenOwnLifetime - time.Minute)}}
	if !hookDrops(b, now) {
		t.Fatal("a refusal older than 30 min must not block the drop")
	}
	if len(b.rdrop.own) != 0 {
		t.Fatalf("an own drop older than its lifetime must be pruned (have %d)", len(b.rdrop.own))
	}
	for i := 0; i < 3*regenMemoryCap; i++ {
		b.rdrop.own = append(b.rdrop.own, regenOwnDrop{itemID: testJunkID, x: i, y: 0, at: now})
	}
	b.regenPrune(now)
	if len(b.rdrop.own) > regenMemoryCap {
		t.Fatalf("own drops are capped at %d, have %d", regenMemoryCap, len(b.rdrop.own))
	}
	b.rdrop.refused = map[string]time.Time{}
	for i := 0; i <= regenMemoryCap+1; i++ {
		b.rdrop.refused[strconv.Itoa(1000+i)+":"+strconv.Itoa(testJunkID)] = now
	}
	b.regenPrune(now)
	if len(b.rdrop.refused) > regenMemoryCap {
		t.Fatalf("refusals are capped at %d, have %d", regenMemoryCap, len(b.rdrop.refused))
	}
}

// Guild wants: a junk item the guild's stock lacks is not dropped (guildGap), and a bot with no persona is safe.
func TestRegenGuildWantedNotDropped(t *testing.T) {
	regenItems()
	withTownDrop(t, true)
	saved := shopsByMap
	t.Cleanup(func() { shopsByMap = saved })
	shopsByMap = map[string][]*Shop{}
	b := overweightTown(map[int]*InvItem{10: {Index: 10, ItemID: testJunkID, Count: 25}})
	b.Zeny = 0
	b.P.Guild = "guildTestWant"
	gstock.Lock()
	gstock.gapIDs["guildTestWant"] = []int{testJunkID}
	gstock.Unlock()
	t.Cleanup(func() {
		gstock.Lock()
		delete(gstock.gapIDs, "guildTestWant")
		gstock.Unlock()
	})
	if n := len(b.regenJunk()); n != 0 {
		t.Fatalf("a guild-wanted junk item must not be droppable (%d listed)", n)
	}
	if hookDrops(b, time.Now()) {
		t.Fatalf("dropped a guild-wanted item (state %q)", b.State)
	}
}

// The party loot loop (party.go partyThink) skips the bot's own drop too: a regression guard on the loop, since
// partyThink needs the whole world to run. The loop must call regenOwnDrop before picking up.
func TestPartyLootSkipsOwnDrop(t *testing.T) {
	src, err := os.ReadFile("party.go")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ReplaceAll(string(src), "\r\n", "\n")
	loop := strings.Index(s, "\t\tfor _, it := range b.items {\n")
	if loop < 0 {
		t.Fatal("party loot loop not found")
	}
	if !strings.Contains(s[loop:loop+400], "b.regenOwnDrop(it, now)") {
		t.Fatal("the party loot loop must skip the bot's own drops (regenOwnDrop)")
	}
}
