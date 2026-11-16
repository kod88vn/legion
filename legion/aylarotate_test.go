package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRotRole(t *testing.T) {
	cases := map[int]string{8: "healer", 4009: "healer", 4: "healer", 14: "tank", 4015: "tank", 7: "melee", 4008: "melee",
		12: "melee", 11: "ranged", 4012: "ranged", 9: "caster", 16: "caster", 4010: "caster", 15: "support", 4016: "support",
		19: "support", 18: "support", 0: ""}
	for c, want := range cases {
		if got := rotRole(c); got != want {
			t.Errorf("rotRole(%d) = %q, want %q", c, got, want)
		}
	}
}

func rotTestParty() []rotMember {
	return []rotMember{
		{Char: "Priesty", Class: 8, Build: "priest", Lv: 80},
		{Char: "Leadfox", Class: 9, Build: "wizard", Lv: 80, Leader: true},
		{Char: "Knighty", Class: 7, Build: "knight", Lv: 80},
		{Char: "Crusy", Class: 14, Build: "crusader", Lv: 80},
		{Char: "Monky", Class: 15, Build: "monk", Lv: 80},
	}
}

func TestPickRotationRules(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	lg := &rotLog{Joined: map[string]int64{"knighty": now.Add(-time.Hour).Unix(), "crusy": now.Add(-10 * time.Hour).Unix(),
		"monky": now.Add(-5 * time.Hour).Unix()}, Left: map[string]int64{}, Tested: map[string]int64{}}
	cands := []rotCand{
		{Char: "Paly", Account: "a1", Class: 4015, Build: "paladin", Lv: 81, Guild: "Starveil", Online: true},   // tank, Starveil
		{Char: "Crus2", Account: "a2", Class: 14, Build: "crusader_agi", Lv: 79, Online: true},                  // tank, guildless
		{Char: "Crus3", Account: "a3", Class: 14, Build: "crusader", Lv: 80, Guild: "Starveil", Online: true},   // same class/build as Crusy
		{Char: "FarLv", Account: "a4", Class: 14, Build: "crusader_x", Lv: 90, Guild: "Starveil", Online: true}, // level out of band
		{Char: "Other", Account: "a5", Class: 14, Build: "crusader_y", Lv: 80, Guild: "Kitty Cafe", GuildID: 9, Online: true},
		{Char: "Prst2", Account: "a6", Class: 8, Build: "battle", Lv: 80, Guild: "Starveil", Online: true},
	}
	out, in, ok, why := pickRotation(rotTestParty(), cands, lg, 80, "Starveil", now)
	if !ok {
		t.Fatalf("no swap: %s", why)
	}
	if out.Char != "Crusy" {
		t.Fatalf("out = %s, want Crusy (longest in the party; never the priest or the party leader)", out.Char)
	}
	// Paladin (untested class, Starveil) before the guildless AGI Crusader; Crus3 same key, FarLv, Other excluded
	if in.Char != "Paly" {
		t.Fatalf("in = %s, want Paly", in.Char)
	}
	// Paladin tested lately: Crusader class tested too (in the party now) -> the guildless Crus2 (untested build)
	lg.Tested["paladin/paladin"] = now.Add(-time.Hour).Unix()
	if _, in, ok, _ = pickRotation(rotTestParty(), cands, lg, 80, "Starveil", now); !ok || in.Char != "Crus2" {
		t.Fatalf("in = %s %v, want Crus2", in.Char, ok)
	}
	// Crus2 left her party 3 h ago: never back within 24 h -> no tank; the next member (Monky) has no candidate either
	lg.Left["crus2"] = now.Add(-3 * time.Hour).Unix()
	if _, in, ok, _ = pickRotation(rotTestParty(), cands, lg, 80, "Starveil", now); ok {
		t.Fatalf("swap with %s: nobody fits", in.Char)
	}
	// AY11: a Monk is a frontline. A Bard (same rotRole, support, but a ranged slot) must not take its place;
	// an Alchemist (frontline slot) may: Monky rotates for the Alchemist.
	bard := rotCand{Char: "Bardy", Account: "a7", Class: 19, Build: "bard", Lv: 80, Guild: "Starveil", Online: true}
	if _, in, ok, _ = pickRotation(rotTestParty(), append(cands, bard), lg, 80, "Starveil", now); ok && in.Char == "Bardy" {
		t.Fatal("a Bard took a Monk's frontline slot")
	}
	cands = append(cands, rotCand{Char: "Alchy", Account: "a9", Class: 18, Build: "alchemist", Lv: 80, Guild: "Starveil", Online: true})
	if out, in, ok, _ = pickRotation(rotTestParty(), cands, lg, 80, "Starveil", now); !ok || out.Char != "Monky" || in.Char != "Alchy" {
		t.Fatalf("got %s -> %s %v, want Monky -> Alchy", out.Char, in.Char, ok)
	}
	// offline / pinned / leader / trader / brewer candidates are never taken
	for _, c := range []rotCand{{Online: false}, {Online: true, Pinned: true}, {Online: true, Leader: true},
		{Online: true, Trader: true}, {Online: true, Brewer: true}} {
		c.Char, c.Account, c.Class, c.Build, c.Lv, c.Guild = "Z", "z", 19, "dancer?", 80, "Starveil"
		if _, in, ok, _ := pickRotation(rotTestParty()[:5], []rotCand{c}, &rotLog{Joined: map[string]int64{}, Left: map[string]int64{}, Tested: map[string]int64{}}, 80, "Starveil", now); ok {
			t.Fatalf("took %+v", in)
		}
	}
	// only the healer and the party leader: nobody rotates
	if _, _, ok, _ := pickRotation(rotTestParty()[:2], cands, lg, 80, "Starveil", now); ok {
		t.Fatal("the priest or the party leader rotated")
	}
}

func TestHandoffRunsAndLogin(t *testing.T) {
	p := &Persona{Account: "lg1", Chars: []CharSpec{{Name: "NewGuy"}}}
	q := &Persona{Account: "lg2", Chars: []CharSpec{{Name: "OldGuy"}}}
	f := handoffFile{ToCore: map[string]int64{"newguy": 100}, ToMain: map[string]int64{"oldguy": 100}}
	if !handoffRuns(p, true, false, f) || handoffRuns(p, false, true, f) {
		t.Fatal("NewGuy belongs to the core instance")
	}
	if handoffRuns(q, true, true, f) || !handoffRuns(q, false, false, f) {
		t.Fatal("OldGuy belongs to the main instance")
	}
	if handoffMayLogin(p, true, f, handoffAck{}) {
		t.Fatal("core logged NewGuy in before main released it")
	}
	if !handoffMayLogin(p, true, f, handoffAck{Released: map[string]int64{"newguy": 100}}) {
		t.Fatal("released: core may log NewGuy in")
	}
	if handoffMayLogin(q, false, f, handoffAck{}) {
		t.Fatal("main logged OldGuy in before core released it")
	}
	f.CoreReleased = map[string]int64{"oldguy": 100}
	if !handoffMayLogin(q, false, f, handoffAck{}) {
		t.Fatal("core released OldGuy: main may log in")
	}
	// an older release stamp doesn't count for a newer handoff
	f.ToMain["oldguy"] = 200
	if handoffMayLogin(q, false, f, handoffAck{}) {
		t.Fatal("stale release stamp")
	}
	r := &Persona{Account: "lg3", Chars: []CharSpec{{Name: "Plain"}}}
	if !handoffRuns(r, true, true, f) || handoffRuns(r, true, false, f) || !handoffMayLogin(r, true, f, handoffAck{}) {
		t.Fatal("no override: the static split")
	}
}

func rotTempData(t *testing.T) func() {
	dir := t.TempDir()
	oldD, oldM := *dataDir, memDir
	*dataDir, memDir = dir, filepath.Join(dir, "memory")
	os.MkdirAll(memDir, 0755)
	handoffInvalidate()
	return func() {
		*dataDir, memDir = oldD, oldM
		handoffInvalidate()
		hpRoster.Lock()
		hpRoster.at, hpRoster.parties = time.Time{}, nil
		hpRoster.Unlock()
	}
}

func TestHandoffAssignAndSelect(t *testing.T) {
	defer rotTempData(t)()
	oldO, oldE := *onlyNames, *excludeNames
	defer func() { *onlyNames, *excludeNames = oldO, oldE }()
	now := time.Unix(1_800_000_000, 0)
	handoffAssign("NewGuy", "OldGuy", now)
	handoffAssign("Third", "Fourth", now.Add(time.Second))
	handoffAssign("OldGuy", "Third", now.Add(2*time.Second)) // OldGuy back to core: removed from to_main
	f, _ := handoffNow()
	if f.ToCore["newguy"] == 0 || f.ToCore["oldguy"] == 0 || f.ToMain["oldguy"] != 0 || f.ToMain["third"] == 0 || f.ToCore["third"] != 0 {
		t.Fatalf("handoff file: %+v", f)
	}
	handoffMarkReleased(false, "newguy", f.ToCore["newguy"])
	if _, a := handoffNow(); a.Released["newguy"] != f.ToCore["newguy"] {
		t.Fatalf("main ack: %+v", a)
	}
	all := []*Persona{{Account: "c1", Chars: []CharSpec{{Name: "Core1"}}}, {Account: "n", Chars: []CharSpec{{Name: "NewGuy"}}},
		{Account: "t", Chars: []CharSpec{{Name: "Third"}}}, {Account: "x", Chars: []CharSpec{{Name: "Bystander"}}}}
	*onlyNames, *excludeNames = "Core1,Third", ""
	got := handoffSelect(all, []*Persona{all[0], all[2]})
	names := []string{}
	for _, p := range got {
		names = append(names, p.Chars[0].Name)
	}
	if strings.Join(names, ",") != "Core1,NewGuy" {
		t.Fatalf("core runs %v, want Core1,NewGuy", names)
	}
	*onlyNames, *excludeNames = "", "Core1,Third"
	got = handoffSelect(all, []*Persona{all[1], all[3]})
	names = names[:0]
	for _, p := range got {
		names = append(names, p.Chars[0].Name)
	}
	if strings.Join(names, ",") != "Bystander,Third" {
		t.Fatalf("main runs %v, want Bystander,Third", names)
	}
	if !handoffCoreName(map[string]bool{"third": true}, "NewGuy") || handoffCoreName(map[string]bool{"third": true}, "Third") {
		t.Fatal("coreForward names")
	}
}

func TestRotApplySwapInPlace(t *testing.T) {
	defer rotTempData(t)()
	mk := func(acct, char string, class int) *Bot {
		b := NewBot(&Persona{Account: acct, Name: acct, Chars: []CharSpec{{Name: char, Class: class}}})
		b.Class = class
		return b
	}
	pr, kn, cr := mk("rt1", "RotPriest", 8), mk("rt2", "RotKnight", 7), mk("rt3", "RotCrus", 14)
	nw := mk("rt4", "RotPaladin", 4015)
	for _, b := range []*Bot{pr, kn, cr} {
		b.pin = &Pin{Char: b.charName(), Human: "RotHuman"}
	}
	p := &Party{Name: humanPartyName("RotHuman"), Leader: cr, Members: []*Bot{pr, kn, cr}, Human: "RotHuman", ShareSet: true}
	parties.Lock()
	parties.byName[p.Name] = p
	for _, m := range p.Members {
		parties.ofBot[m] = p
	}
	parties.Unlock()
	defer func() {
		parties.Lock()
		delete(parties.byName, p.Name)
		for _, m := range []*Bot{pr, kn, cr, nw} {
			delete(parties.ofBot, m)
		}
		parties.Unlock()
	}()
	os.WriteFile(humanPartyFile(), []byte(`[{"human":"RotHuman","members":["RotPriest","RotKnight","RotCrus"],"alumni":["RotPaladin","Old one"]}]`), 0644)
	rotApplySwap(p, cr, nw, "RotPaladin", time.Now())
	if len(p.Members) != 3 || p.Members[2] != nw || p.Leader == cr || !p.ShareSet {
		t.Fatalf("roster not edited in place: %+v", p.Members)
	}
	if partyOf(cr) != nil || partyOf(nw) != p {
		t.Fatal("ofBot not updated")
	}
	if cr.pin != nil || nw.pin == nil || nw.pin.Human != "RotHuman" || nw.pin.Char != "RotPaladin" {
		t.Fatal("pins")
	}
	var saved []savedHumanParty
	raw, _ := os.ReadFile(humanPartyFile())
	json.Unmarshal(raw, &saved)
	if len(saved) != 1 || strings.Join(saved[0].Members, ",") != "RotPriest,RotKnight,RotPaladin" ||
		strings.Join(saved[0].Alumni, ",") != "Old one,RotCrus" {
		t.Fatalf("human-parties.json: %s", raw)
	}
	if h, ok := alumnusIn(savedRoster(), "RotCrus"); !ok || h != "RotHuman" {
		t.Fatal("the rotated member is not an alumnus yet")
	}
}

func TestRotReportAppends(t *testing.T) {
	defer rotTempData(t)()
	rotReport(rotSwap{At: 1, Human: "Ayla", Out: "A", In: "Big Guy", InClass: "Paladin", Role: "tank"})
	raw, _ := os.ReadFile(filepath.Join(*dataDir, "bugqueue.jsonl"))
	var row map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &row) != nil || row["id"] != "rot1-big_guy" || row["kind"] != "info" {
		t.Fatalf("bug queue row: %s", raw)
	}
}
