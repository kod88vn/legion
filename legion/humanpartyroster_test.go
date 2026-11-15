package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAlumnusIn(t *testing.T) {
	r := []savedHumanParty{{Human: "Ayla", Members: []string{"Zen.fox", "LilLilyblade"}, Alumni: []string{"Silent rogue", "Zen.fox"}}}
	if h, ok := alumnusIn(r, "silent ROGUE"); !ok || h != "Ayla" {
		t.Fatalf("Silent rogue: got %q %v, want Ayla true", h, ok)
	}
	if _, ok := alumnusIn(r, "Zen.fox"); ok {
		t.Fatal("Zen.fox is back in members: not an alumnus")
	}
	if _, ok := alumnusIn(r, "LilLilyblade"); ok {
		t.Fatal("a member is not an alumnus")
	}
	if humanPartyName("Ayla") != "Ayla pt" {
		t.Fatal("party name")
	}
}

func TestWithAlumniKeepsList(t *testing.T) {
	dir := t.TempDir()
	old := *dataDir
	*dataDir = dir
	defer func() { *dataDir = old }()
	os.WriteFile(filepath.Join(dir, "human-parties.json"),
		[]byte(`[{"human":"Ayla","members":["A"],"alumni":["Silent rogue"]}]`), 0644)
	out := withAlumni([]savedHumanParty{{Human: "Ayla", Members: []string{"A", "LilLilyblade"}}})
	if len(out[0].Alumni) != 1 || out[0].Alumni[0] != "Silent rogue" {
		t.Fatalf("alumni lost on save: %+v", out)
	}
	hpRoster.Lock()
	hpRoster.at = time.Time{}
	hpRoster.Unlock()
	if _, ok := alumnusIn(savedRoster(), "Silent rogue"); !ok {
		t.Fatal("savedRoster didn't read the alumni")
	}
	hpRoster.Lock()
	hpRoster.at, hpRoster.parties = time.Time{}, nil
	hpRoster.Unlock()
}
