package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// partyChatReset points the chat at a temp data dir (never the live legion/data, G23).
func partyChatReset(t *testing.T) {
	old := *dataDir
	*dataDir = t.TempDir()
	t.Cleanup(func() { time.Sleep(2500 * time.Millisecond); *dataDir = old }) // let a scheduled save land in the temp dir
	partyChat.Lock()
	partyChat.byHuman, partyChat.loaded = nil, false
	partyChat.Unlock()
}

func TestPartyChatPersists(t *testing.T) {
	partyChatReset(t)
	partyChatHeard("PersistHuman", "Zen : saved line", time.Unix(2000, 0))
	time.Sleep(2500 * time.Millisecond)
	partyChat.Lock()
	partyChat.byHuman, partyChat.loaded = nil, false // a restart
	partyChat.Unlock()
	l := partyChatOf("persisthuman")
	if len(l) != 1 || l[0].Text != "saved line" || l[0].From != "Zen" {
		t.Fatalf("after reload: %+v", l)
	}
}

func TestLeadPosPick(t *testing.T) {
	now := time.Unix(20000, 0)
	st := &humanDetail{Map: "pay_dun00", X: 26, Y: 166, StatusTS: now.Unix() - 1, Fresh: true, Online: true}
	// the party data moved her after the warp (newer than status.json): the party map wins
	m, x, y := "pay_arche", 39, 131
	if src := leadPosPick(st, &m, &x, &y, now, now); src != "party_newer" || m != "pay_arche" || x != 39 {
		t.Fatalf("%s %s %d,%d", src, m, x, y)
	}
	// same map: status.json's cell (fresher than sight)
	m, x, y = "pay_dun00", 20, 175
	if src := leadPosPick(st, &m, &x, &y, now.Add(-time.Minute), now); src != "status" || x != 26 {
		t.Fatalf("%s %d", src, x)
	}
	// a party update over 2 min old on another map: status.json
	m, x, y = "payon", 0, 0
	if src := leadPosPick(st, &m, &x, &y, now.Add(-3*time.Minute), now); src != "status_newer" || m != "pay_dun00" {
		t.Fatalf("%s %s", src, m)
	}
}

func TestLeadPosSource(t *testing.T) {
	now := time.Unix(10000, 0)
	m, x, y := "pay_dun00", 0, 0 // party packets: map, no cell
	d := &humanDetail{Map: "pay_dun00", X: 166, Y: 42, StatusTS: now.Unix() - 45}
	if src := leadPosSource(d, &m, &x, &y, now); src != "status_old" || x != 166 || y != 42 {
		t.Fatalf("src %s %d,%d", src, x, y)
	}
	if !sameMap("pay_dun00.gat", "PAY_DUN00") {
		t.Fatal("sameMap")
	}
}

func TestPartyChatDedupeAndOrder(t *testing.T) {
	partyChatReset(t)
	now := time.Unix(1000, 0)
	partyChatHeard("TestHuman", "Zen : hi", now)
	partyChatHeard("TestHuman", "Zen : hi", now.Add(time.Second)) // the same line via another friend
	partyChatHeard("TestHuman", "Ayla : lets go", now.Add(2*time.Second))
	partyChatHeard("TestHuman", "Zen : hi", now.Add(20*time.Second)) // said again later
	l := partyChatOf("testhuman")
	if len(l) != 3 || l[0].Text != "hi" || l[1].From != "Ayla" || l[1].Text != "lets go" {
		t.Fatalf("got %+v", l)
	}
	for i := 0; i < 40; i++ {
		partyChatHeard("TestHuman", "X : line"+string(rune('a'+i)), now.Add(time.Duration(30+i)*time.Second))
	}
	if n := len(partyChatOf("TestHuman")); n != partyChatKeep {
		t.Fatalf("kept %d", n)
	}
}

func TestExpSampleAndMemberJSONRoundTrip(t *testing.T) {
	t0 := time.Unix(5000, 0)
	expSample("ExpTester", 10, 100, t0)
	expSample("ExpTester", 10, 600, t0.Add(time.Minute))
	h, m := expSample("ExpTester", 11, 400, t0.Add(2*time.Minute)) // level-up: +400 of the new level
	if m != 2 || h != 27000 {
		t.Fatalf("exp/h %d over %v min", h, m)
	}
	// the main instance decodes the core's party JSON (mergeCoreParties): the embedded extras must survive
	in := []partyView{{Name: "x", Human: "H", Members: []partyMemberView{{Name: "b", PartyMemberExtras: &PartyMemberExtras{DistLead: 7, KPM: 1.5}},
		{Name: "H", Human: true, Intent: "farming", Detail: &humanDetail{humanExtras: humanExtras{Intent: "farming"}}}}}}
	raw, _ := json.Marshal(in)
	var out []partyView
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out[0].Members[0].PartyMemberExtras == nil || out[0].Members[0].DistLead != 7 || out[0].Members[1].Intent != "farming" || out[0].Members[1].Detail.Intent != "farming" {
		t.Fatalf("round trip lost fields: %s", raw)
	}
}

// TestPartyDetailExpForEngineParty (aylawatch): every member of a party carries its base exp total and exp/h, engine
// parties included (no human leader there), and the human-led extras keep the same numbers. Served through the real
// handler (GET /api/parties/detail?local=1: the core merge is off).
func TestPartyDetailExpForEngineParty(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaOwn" })
	_, lead, mem := ovHookParty(t, "parity-exp-view", true)
	lead.BaseLv, lead.BaseExp = 50, 123456
	mem.BaseLv, mem.BaseExp = 40, 7890
	mux := http.NewServeMux()
	registerPartyDash(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/parties/detail?local=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var views []partyView
	if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
		t.Fatal(err)
	}
	var found *partyView
	for i := range views {
		if views[i].Name == "parity-exp-view" {
			found = &views[i]
		}
	}
	if found == nil {
		t.Fatalf("engine party missing from the detail: %s", rec.Body.String())
	}
	want := map[string]int64{"AylaOwn": 123456, "AylaMate": 7890}
	for _, m := range found.Members {
		if got, ok := want[m.Name]; !ok || m.BaseExp != got {
			t.Errorf("member %s: base_exp %d, want %d", m.Name, m.BaseExp, got)
		}
	}
	if !strings.Contains(rec.Body.String(), `"base_exp":123456`) || !strings.Contains(rec.Body.String(), `"exp_per_h":`) {
		t.Errorf("the additive JSON fields are missing: %s", rec.Body.String())
	}
}

// helperReset points the helper reader at a temp data dir with a fresh cache (G23: never the live legion/data).
func helperReset(t *testing.T, rows string) {
	old := *dataDir
	*dataDir = t.TempDir()
	t.Cleanup(func() { *dataDir = old })
	if err := os.WriteFile(filepath.Join(*dataDir, "puppet_calls.jsonl"), []byte(rows), 0644); err != nil {
		t.Fatal(err)
	}
	helperCache.Lock()
	helperCache.at, helperCache.run = time.Time{}, nil
	helperCache.Unlock()
}

// the rows dispatch.py append_log writes (json.dumps: ", " and ": " separators)
const helperAylaRow = `{"t": 100.0, "role": "ayla", "trigger": "wake", "model": "haiku", "exit": %s, "is_error": false, "result": "ok text", "duration_s": 2.5}` + "\n"

func TestHelperLastRunStatus(t *testing.T) {
	cases := []struct{ name, exit, status string }{
		{"exit 0", "0", "ok"},
		{"exit 1", "1", "failed"},
		{"timeout", `"timeout"`, "timeout"},
		{"refused", `"refused"`, "refused"},
		{"error", `"error"`, "error"},
		{"null", "null", "failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// a newer row of another role must not be taken, and a malformed line must be skipped
			rows := fmt.Sprintf(helperAylaRow, c.exit) +
				`{"t": 200.0, "role": "guild01", "trigger": "x", "exit": "timeout"}` + "\n" +
				`{"t": 300.0, "role": "ayla", "exit": "timeo` + "\n"
			helperReset(t, rows)
			r := helperLastRun("ayla")
			if r == nil {
				t.Fatal("no ayla run found")
			}
			if r.Status != c.status {
				t.Errorf("status %q, want %q", r.Status, c.status)
			}
			if string(r.Exit) != c.exit {
				t.Errorf("exit %s, want %s (passed on as written)", r.Exit, c.exit)
			}
			if r.Role != "ayla" || r.Trigger != "wake" || r.Result != "ok text" {
				t.Errorf("fields lost: %+v", r)
			}
		})
	}
	t.Run("exit 0 with is_error is failed", func(t *testing.T) {
		helperReset(t, strings.Replace(fmt.Sprintf(helperAylaRow, "0"), `"is_error": false`, `"is_error": true`, 1))
		if r := helperLastRun("ayla"); r == nil || r.Status != "failed" {
			t.Errorf("got %+v, want failed", r)
		}
	})
	t.Run("newest ayla row wins, malformed last line skipped", func(t *testing.T) {
		rows := fmt.Sprintf(helperAylaRow, "0") + fmt.Sprintf(helperAylaRow, `"timeout"`) + `{"t": 9`
		helperReset(t, rows)
		if r := helperLastRun("ayla"); r == nil || r.Status != "timeout" {
			t.Errorf("got %+v, want the newer timeout row", r)
		}
	})
	t.Run("no ayla row, only other roles and junk", func(t *testing.T) {
		helperReset(t, `{"t": 1.0, "role": "guild02", "exit": 0}`+"\n"+"garbage\n")
		if r := helperLastRun("ayla"); r != nil {
			t.Errorf("got %+v, want nil", r)
		}
	})
}

func TestExpMeterPrune(t *testing.T) {
	t0 := time.Unix(7000, 0)
	expSample("PruneKeep", 5, 10, t0)
	expSample("PruneGone", 5, 10, t0)
	expMeterPrune(map[string]bool{"prunekeep": true})
	expMeter.Lock()
	_, kept := expMeter.by["prunekeep"]
	_, gone := expMeter.by["prunegone"]
	expMeter.Unlock()
	if !kept || gone {
		t.Fatalf("kept=%v gone=%v, want kept=true gone=false", kept, gone)
	}
}
