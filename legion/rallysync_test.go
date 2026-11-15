package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeHumanStatus(t *testing.T, path, rallyTown string) {
	t.Helper()
	js := fmt.Sprintf(`{"name":"TestAyla","online":true,"map":"payon","x":180,"y":100,"ts":%d,"rally_town":%q,"rally_x":181,"rally_y":104}`, time.Now().Unix(), rallyTown)
	if err := os.WriteFile(path, []byte(js), 0644); err != nil {
		t.Fatal(err)
	}
	humanStats.Lock()
	humanStats.at = time.Time{} // re-read now
	humanStats.Unlock()
}

func TestHumanRallySyncMirrorsHerRally(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	t.Setenv("LEGION_HUMAN_STATUS", path)
	p := &Party{Name: "TestAyla pt", Human: "TestAyla"}
	writeHumanStatus(t, path, "payon")
	if !humanRallySync(p, time.Now()) || p.Rally.Town != "payon" || p.Rally.X != 181 || p.Rally.Why != rallyMirrorWhy {
		t.Fatalf("her rally not mirrored: %+v", p.Rally)
	}
	writeHumanStatus(t, path, "")
	if !humanRallySync(p, time.Now()) || p.Rally.Town != "" {
		t.Fatalf("her rally ended but the members' rally goes on: %+v", p.Rally)
	}
	// a rally from her party line (not mirrored) is left alone when she has no rally
	p.Rally.Town, p.Rally.Why = "geffen", "TestAyla called it"
	if humanRallySync(p, time.Now()) || p.Rally.Town != "geffen" {
		t.Fatalf("a line rally was ended: %+v", p.Rally)
	}
}

// writeHumanStatusAt writes her status as of t (her plugin rewrites it every few seconds) and drops the read cache.
func writeHumanStatusAt(t *testing.T, path string, at time.Time, town, m string) {
	t.Helper()
	js := fmt.Sprintf(`{"name":"TestAyla","online":true,"map":%q,"x":10,"y":10,"ts":%d,"rally_town":%q,"rally_x":181,"rally_y":104}`, m, at.Unix(), town)
	if err := os.WriteFile(path, []byte(js), 0644); err != nil {
		t.Fatal(err)
	}
	humanStats.Lock()
	humanStats.at = time.Time{}
	humanStats.Unlock()
}

// replay of 2026-11-15 09:02-09:07: she calls the rally at payon, then leads them into pay_dun02 while her status still
// says payon. The engine ended the mirror 20 s later ("left payon") and re-mirrored it on the next tick (15 times in 5 min).
// Now the call's rally is mirrored once and stays ended: no restart, however long her status keeps the call.
func TestRallyMirrorNotRestartedAfterEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	t.Setenv("LEGION_HUMAN_STATUS", path)
	p := &Party{Name: "TestAyla pt", Human: "TestAyla"}
	base := time.Unix(time.Now().Unix(), 0)

	writeHumanStatusAt(t, path, base, "payon", "payon")
	p.rallyTick(base)
	if p.Rally.Town != "payon" || p.Rally.Why != rallyMirrorWhy || !p.Rally.At.Equal(base) {
		t.Fatalf("her call not mirrored: %+v", p.Rally)
	}
	// she is in pay_dun02 from 20 s on; the old rallyThink check ended the rally then
	for at := base.Add(5 * time.Second); at.Before(base.Add(25 * time.Second)); at = at.Add(5 * time.Second) {
		writeHumanStatusAt(t, path, at, "payon", "pay_dun02")
		p.rallyTick(at)
	}
	writeHumanStatusAt(t, path, base.Add(25*time.Second), "payon", "pay_dun02")
	p.endRally(false, "replayed end", base.Add(25*time.Second))
	restarts := 0
	for at := base.Add(30 * time.Second); at.Before(base.Add(rallyMirrorTimeout)); at = at.Add(3 * time.Second) {
		writeHumanStatusAt(t, path, at, "payon", "pay_dun02")
		p.rallyTick(at)
		if p.Rally.Town != "" {
			restarts++
		}
	}
	if restarts != 0 || !p.Rally.At.Equal(base) {
		t.Fatalf("the ended call was mirrored again %d times (rally started %v): %+v", restarts, p.Rally.At, p.Rally)
	}
	// she cancels (status town cleared): the next call of hers is a new rally
	later := base.Add(rallyMirrorTimeout + time.Minute)
	writeHumanStatusAt(t, path, later, "", "pay_dun02")
	p.rallyTick(later)
	if p.Rally.MirrorDone != "" {
		t.Fatalf("her rally is over but the call is still marked done: %+v", p.Rally)
	}
	writeHumanStatusAt(t, path, later.Add(3*time.Second), "payon", "pay_dun02")
	p.rallyTick(later.Add(3 * time.Second))
	if p.Rally.Town != "payon" || !p.Rally.At.Equal(later.Add(3*time.Second)) {
		t.Fatalf("a new call of hers did not start a rally: %+v", p.Rally)
	}
}

// the call's rally ends by the timeout when the members never gather, and is not mirrored again while her status keeps it
func TestRallyMirrorTimesOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	t.Setenv("LEGION_HUMAN_STATUS", path)
	p := &Party{Name: "TestAyla pt", Human: "TestAyla"}
	base := time.Unix(time.Now().Unix(), 0)
	writeHumanStatusAt(t, path, base, "payon", "pay_dun02")
	p.rallyTick(base)
	for at := base.Add(10 * time.Second); at.Before(base.Add(rallyMirrorTimeout + 30*time.Second)); at = at.Add(10 * time.Second) {
		writeHumanStatusAt(t, path, at, "payon", "pay_dun02")
		p.rallyTick(at)
	}
	if p.Rally.Town != "" || p.Rally.Last != "her rally timed out" {
		t.Fatalf("mirrored rally did not time out: %+v", p.Rally)
	}
	writeHumanStatusAt(t, path, base.Add(rallyMirrorTimeout+time.Minute), "payon", "pay_dun02")
	p.rallyTick(base.Add(rallyMirrorTimeout + time.Minute))
	if p.Rally.Town != "" {
		t.Fatalf("timed-out call restarted: %+v", p.Rally)
	}
}
