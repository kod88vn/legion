package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
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

// The party loot loop (party.go partyThink) skips the bot's own drop too. partyThink needs the whole world to run, so the
// check parses party.go: inside partyThink, the body of the range over b.items must call regenOwnDrop.
func TestPartyLootSkipsOwnDrop(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "party.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var think *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "partyThink" {
			think = fd
		}
	}
	if think == nil {
		t.Fatal("partyThink not found")
	}
	loops, skipping := 0, 0
	ast.Inspect(think.Body, func(n ast.Node) bool {
		rs, ok := n.(*ast.RangeStmt)
		if !ok || !isSelectorOn(rs.X, "b", "items") {
			return true
		}
		loops++
		ast.Inspect(rs.Body, func(m ast.Node) bool {
			if c, ok := m.(*ast.CallExpr); ok && isSelectorOn(c.Fun, "b", "regenOwnDrop") {
				skipping++
			}
			return true
		})
		return true
	})
	if loops == 0 {
		t.Fatal("no range over b.items in partyThink")
	}
	if skipping == 0 {
		t.Fatal("the party loot loop must skip the bot's own drops: regenOwnDrop must be called in its body")
	}
}

// isSelectorOn: e is recv.name (for b.items, b.regenOwnDrop(...)'s function part).
func isSelectorOn(e ast.Expr, recv, name string) bool {
	if c, ok := e.(*ast.CallExpr); ok {
		e = c.Fun
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == recv
}
