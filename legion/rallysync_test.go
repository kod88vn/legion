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
