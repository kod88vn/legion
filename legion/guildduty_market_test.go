package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// WP6 (design market-engine.md 4.3, step 1): the market direction channel is RETIRED. by=market answers 410, the flag
// market_directions_on has no effect, a market direction in an old file is dropped on load, and the guild view says
// market_state "retired". The code path is kept but unreachable; deleting it is the later step.

func marketTune(t *testing.T, on bool) {
	t.Helper()
	old := tune()
	cp := *old
	cp.MarketDirectionsOn = on // the retired flag: must not change anything
	curTuning.Store(&cp)
	t.Cleanup(func() { curTuning.Store(old) })
}

// isolateDuty: a test's guild duty store under a temp data folder (G23), restored afterwards
func isolateDuty(t *testing.T, ids ...string) {
	t.Helper()
	dir := *dataDir
	*dataDir = t.TempDir()
	gduty.Lock()
	saved := map[string][2][]*GuildDirection{}
	for _, id := range ids {
		saved[id] = [2][]*GuildDirection{gduty.dirs[id], gduty.market[id]}
		gduty.dirs[id], gduty.market[id] = nil, nil
	}
	gduty.dirty, gduty.loaded = true, true
	gduty.Unlock()
	t.Cleanup(func() {
		gduty.Lock()
		for id, v := range saved {
			gduty.dirs[id], gduty.market[id] = v[0], v[1]
		}
		gduty.Unlock()
		*dataDir = dir
	})
}

func postDir(t *testing.T, id, body string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	guildDirAPI(rec, httptest.NewRequest("POST", "/api/guild-direction/"+id, strings.NewReader(body)))
	return rec.Code
}

func marketCount(id string) int {
	gduty.Lock()
	defer gduty.Unlock()
	return len(gduty.market[id])
}

func TestMarketByRefusedWithGone(t *testing.T) {
	for _, on := range []bool{false, true} {
		marketTune(t, on)
		isolateDuty(t, "guild04")
		if c := postDir(t, "guild04", `{"kind":"supply","by":"market","hours":4}`); c != 410 {
			t.Fatalf("by=market with market_directions_on=%v: %d, want 410", on, c)
		}
		if marketCount("guild04") != 0 {
			t.Fatal("a refused market direction was stored")
		}
	}
}

func TestMarketExclusiveAndUnknownByStillRefused(t *testing.T) {
	marketTune(t, true)
	isolateDuty(t, "guild08")
	if c := postDir(t, "guild08", `{"kind":"supply","by":"puppet"}`); c != 400 {
		t.Fatalf("unknown by: %d, want 400", c)
	}
	if c := postDir(t, "guild08", `{"kind":"supply","by":"leader","hours":4}`); c != 200 {
		t.Fatalf("by=leader: %d, want 200", c)
	}
}

func TestMarketFlagHasNoEffect(t *testing.T) {
	marketTune(t, true)
	if marketDirOn() {
		t.Fatal("marketDirOn with market_directions_on=true: the channel is retired and must stay off")
	}
	isolateDuty(t, "guild07")
	gduty.Lock()
	gduty.market["guild07"] = []*GuildDirection{{ID: "x", Guild: "guild07", Kind: "supply", SetBy: "market", Expires: time.Now().Add(time.Hour)}}
	gduty.Unlock()
	for _, d := range activeDirsLocked("guild07", time.Now()) {
		if d.SetBy == "market" {
			t.Fatal("a market direction is in force: the channel is retired")
		}
	}
}

func TestMarketOldFileDroppedOnLoad(t *testing.T) {
	isolateDuty(t, "guild02")
	dir := *dataDir
	old := `{"directions":{"guild02":[{"id":"guild02-L1","guild":"guild02","kind":"wealth","set_by":"leader","expires":"` +
		time.Now().Add(time.Hour).Format(time.RFC3339Nano) + `"}]},` +
		`"market_directions":{"guild02":[{"id":"guild02-M1","guild":"guild02","kind":"supply","set_by":"market","expires":"` +
		time.Now().Add(time.Hour).Format(time.RFC3339Nano) + `"}]},"members":[],"kicked":[],"counters":{}}`
	if err := os.WriteFile(filepath.Join(dir, "guild_duty.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	gduty.Lock()
	gduty.dirs["guild02"], gduty.loaded = nil, false
	gduty.Unlock()
	dutyLoad()
	if n := marketCount("guild02"); n != 0 {
		t.Fatalf("old market directions loaded: %d, want 0", n)
	}
	gduty.Lock()
	leader := len(gduty.dirs["guild02"])
	gduty.Unlock()
	if leader != 1 {
		t.Fatalf("leader directions after an old-format load: %d, want 1", leader)
	}
	for _, d := range activeDirsLocked("guild02", time.Now()) {
		if d.SetBy == "market" {
			t.Fatal("old market direction in force after load")
		}
	}
}

func TestMarketNotWrittenOnSave(t *testing.T) {
	isolateDuty(t, "guild03")
	gduty.Lock()
	gduty.dirs["guild03"] = []*GuildDirection{{ID: "guild03-L1", Guild: "guild03", Kind: "wealth", SetBy: "leader", Expires: time.Now().Add(time.Hour)}}
	gduty.dirty = true
	gduty.Unlock()
	dutySave()
	raw, err := os.ReadFile(dutyFilePath())
	if err != nil {
		t.Fatal(err)
	}
	var f map[string]json.RawMessage
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if _, ok := f["market_directions"]; ok {
		t.Fatal("market_directions written to guild_duty.json: the channel is retired")
	}
}

func TestMarketViewReadsRetired(t *testing.T) {
	isolateDuty(t, "guild05")
	v := dirViewOf("guild05", time.Now())
	if v.MarketState != "retired" {
		t.Fatalf("market_state %q, want retired", v.MarketState)
	}
	if len(v.Market) != 0 {
		t.Fatalf("market_set not empty: %d", len(v.Market))
	}
	rec := httptest.NewRecorder()
	guildDirAPI(rec, httptest.NewRequest("GET", "/api/guild-direction/guild05", nil))
	var got struct {
		MarketState string `json:"market_state"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.MarketState != "retired" {
		t.Fatalf("GET view lacks market_state retired: %s", rec.Body.String())
	}
}
