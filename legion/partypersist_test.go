package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanPartyName(t *testing.T) {
	cases := map[string]string{"Novaboy": "Novaboy", "Zen.fox": "Zenfox", "Lily_heart": "Lilyheart",
		"ThePixelstorm Heals": "ThePixelstorm Heals", "xX_..Xx": "xXXx", "a  b": "a b"}
	for in, want := range cases {
		if got := cleanPartyName(in); got != want {
			t.Errorf("cleanPartyName(%q) = %q, want %q", in, got, want)
		}
	}
	parties.Lock()
	n := botPartyName("Some.very_long_character_name")
	parties.Unlock()
	if len(n) > 23 || n != cleanPartyName(n) {
		t.Errorf("botPartyName gave %q (must be <= 23 chars, letters/digits/spaces)", n)
	}
}

func TestSaveLoadBotParties(t *testing.T) {
	path := filepath.Join(t.TempDir(), "parties.json")
	formed := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	in := []savedParty{{
		Name: "Novaboy pt", Leader: "Novaboy", Members: []string{"Novaboy", "Lily heart", "Hanaleaf"},
		Map: "pay_fild06", Checkpoint: "payon", Formed: formed,
		Rally: rallyState{Town: "payon", X: 10, Y: 20},
		Gear:  &gearPool{Turn: "Hanaleaf", TurnSince: formed.Add(time.Minute), TurnItem: 2403, LastGot: map[string]time.Time{"Novaboy": formed}},
	}}
	partySave.last = nil
	if err := saveBotParties(path, in); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind")
	}
	out, err := loadSavedParties(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d parties", len(out))
	}
	o := out[0]
	if o.Name != "Novaboy pt" || o.Leader != "Novaboy" || len(o.Members) != 3 || o.Members[1] != "Lily heart" ||
		o.Map != "pay_fild06" || o.Checkpoint != "payon" || !o.Formed.Equal(formed) || o.Rally.Town != "payon" {
		t.Errorf("round trip mismatch: %+v", o)
	}
	if o.Gear == nil || o.Gear.Turn != "Hanaleaf" || o.Gear.TurnItem != 2403 || !o.Gear.LastGot["Novaboy"].Equal(formed) {
		t.Errorf("gear turn lost: %+v", o.Gear)
	}
	// unchanged content: no rewrite
	st, _ := os.Stat(path)
	os.Chtimes(path, st.ModTime().Add(-time.Hour), st.ModTime().Add(-time.Hour))
	if err := saveBotParties(path, in); err != nil {
		t.Fatal(err)
	}
	if st2, _ := os.Stat(path); st2.ModTime().After(st.ModTime().Add(-time.Minute)) {
		t.Errorf("unchanged parties rewrote the file")
	}
	// empty list writes [] (not null)
	if err := saveBotParties(path, nil); err != nil {
		t.Fatal(err)
	}
	if out, err := loadSavedParties(path); err != nil || len(out) != 0 {
		t.Errorf("empty save: %v %v", out, err)
	}
}

func TestRestoreBotParties(t *testing.T) {
	dir := t.TempDir()
	old := *dataDir
	*dataDir = dir
	defer func() { *dataDir = old }()
	mk := func(acc int, chars ...string) *Bot {
		p := &Persona{Account: "ppers" + chars[0], AccountID: 900000 + acc, Name: chars[0]}
		for _, c := range chars {
			p.Chars = append(p.Chars, CharSpec{Name: c, Role: "main"})
		}
		return NewBot(p)
	}
	a, b, c := mk(1, "PpLead"), mk(2, "PpAlt", "PpMember"), mk(3, "PpThird")
	partySave.last = nil
	saveBotParties(botPartyFile(), []savedParty{{Name: "PpLead's pt", Leader: "PpLead", Members: []string{"PpLead", "PpMember", "PpThird"},
		Map: "prt_fild08", Formed: time.Now().Add(-time.Hour), Gear: &gearPool{Turn: "PpThird"}}})
	restoreBotParties([]*Bot{a, b, c})
	p := partyOf(a)
	if p == nil || partyOf(b) != p || partyOf(c) != p || p.Map != "prt_fild08" || p.Leader != a {
		t.Fatalf("party not restored: %+v", p)
	}
	defer func() {
		parties.Lock()
		delete(parties.byName, p.Name)
		for _, m := range p.Members {
			delete(parties.ofBot, m)
		}
		parties.Unlock()
		endRestore(p, &partyRestore{chars: map[*Bot]string{a: "", b: "", c: ""}})
	}()
	if p.Name != "PpLead pt" {
		t.Errorf("old refused name not cleaned: %q", p.Name)
	}
	if !isRestoring(p) {
		t.Errorf("restored party should wait for its members")
	}
	if got := b.pickChar().Name; got != "PpMember" {
		t.Errorf("member logs in on %q, want the saved character PpMember", got)
	}
	gearPools.Lock()
	gp := gearPools.m[p.Name]
	gearPools.Unlock()
	if gp == nil || gp.Turn != "PpThird" {
		t.Errorf("gear turn not restored: %+v", gp)
	}
	snap := snapshotBotParties()
	found := false
	for _, s := range snap {
		if s.Name == p.Name {
			found = true
			if len(s.Members) != 3 || s.Members[1] != "PpMember" {
				t.Errorf("snapshot uses the saved characters while restoring: %v", s.Members)
			}
		}
	}
	if !found {
		t.Errorf("restored party missing from snapshot")
	}
}
