package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// EC43: market directions (set_by market). They live in their own list, fill only a free slot, never evict a leader's
// direction, are refused while tuning market_directions_on is off, and the leader can clear them.

func marketTune(t *testing.T, on bool) {
	t.Helper()
	old := tune()
	cp := *old
	cp.MarketDirectionsOn = on
	curTuning.Store(&cp)
	t.Cleanup(func() { curTuning.Store(old) })
}

func resetGuildDirs(t *testing.T, id string) {
	t.Helper()
	dir := *dataDir
	*dataDir = t.TempDir() // a save in a test never reaches the live folder (G23)
	gduty.Lock()
	saveD, saveM := gduty.dirs[id], gduty.market[id]
	gduty.dirs[id], gduty.market[id], gduty.dirty, gduty.loaded = nil, nil, true, true
	gduty.Unlock()
	t.Cleanup(func() {
		gduty.Lock()
		gduty.dirs[id], gduty.market[id] = saveD, saveM
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

func counts(id string) (leader, market int) {
	gduty.Lock()
	defer gduty.Unlock()
	return len(gduty.dirs[id]), len(gduty.market[id])
}

func TestMarketDirectionFillsFreeSlotOnly(t *testing.T) {
	marketTune(t, true)
	resetGuildDirs(t, "guild04")
	if c := postDir(t, "guild04", `{"kind":"wealth","hours":4}`); c != 200 {
		t.Fatalf("leader post: %d", c)
	}
	if c := postDir(t, "guild04", `{"kind":"supply","by":"market","hours":4}`); c != 200 {
		t.Fatalf("market post: %d", c)
	}
	if l, m := counts("guild04"); l != 1 || m != 1 {
		t.Fatalf("lists: leader %d market %d, want 1 and 1", l, m)
	}
	act := activeDirsLocked("guild04", time.Now())
	var sawMarket bool
	for _, d := range act {
		if d.SetBy == "market" {
			sawMarket = true
		}
	}
	if !sawMarket || len(act) != 2 {
		t.Fatalf("active: want leader + market, got %d (market seen %v)", len(act), sawMarket)
	}
}

func TestMarketNeverEvictsLeaderDirections(t *testing.T) {
	marketTune(t, true)
	resetGuildDirs(t, "guild05")
	if c := postDir(t, "guild05", `{"kind":"wealth","hours":4}`); c != 200 {
		t.Fatalf("leader post: %d", c)
	}
	if c := postDir(t, "guild05", `{"kind":"level","by":"market","hours":4}`); c != 200 {
		t.Fatalf("market post with one leader direction: %d", c)
	}
	// leaders fill up to the cap of three; the oldest LEADER direction goes, the market one stays
	for _, b := range []string{`{"kind":"supply","min_lv":10,"hours":4}`, `{"kind":"level","min_lv":20,"hours":4}`, `{"kind":"wealth","hours":4}`} {
		if c := postDir(t, "guild05", b); c != 200 {
			t.Fatalf("leader post %s: %d", b, c)
		}
	}
	gduty.Lock()
	defer gduty.Unlock()
	if len(gduty.dirs["guild05"]) != guildDirMax || len(gduty.market["guild05"]) != 1 {
		t.Fatalf("leader %d market %d, want 3 and 1", len(gduty.dirs["guild05"]), len(gduty.market["guild05"]))
	}
	for _, d := range gduty.dirs["guild05"] {
		if d.SetBy != "leader" {
			t.Fatalf("a non-leader direction in the leader list: %+v", d)
		}
	}
}

func TestMarketRefusedWithTwoLeaderDirections(t *testing.T) {
	marketTune(t, true)
	resetGuildDirs(t, "guild06")
	for _, b := range []string{`{"kind":"wealth","hours":4}`, `{"kind":"supply","hours":4}`} {
		postDir(t, "guild06", b)
	}
	if c := postDir(t, "guild06", `{"kind":"level","by":"market","hours":4}`); c == 200 {
		t.Fatal("market accepted in a guild whose leader already has 2 directions")
	}
	if _, m := counts("guild06"); m != 0 {
		t.Fatalf("refused market direction was stored: %d", m)
	}
}

func TestMarketOffByDefaultRefusesAndShowsNone(t *testing.T) {
	marketTune(t, false)
	resetGuildDirs(t, "guild07")
	if c := postDir(t, "guild07", `{"kind":"supply","by":"market","hours":4}`); c != 409 {
		t.Fatalf("market with the flag off: %d, want 409", c)
	}
	// a market direction left in the store (flag turned off later) is not in force
	gduty.Lock()
	gduty.market["guild07"] = []*GuildDirection{{ID: "x", Guild: "guild07", Kind: "supply", SetBy: "market", Expires: time.Now().Add(time.Hour)}}
	gduty.Unlock()
	for _, d := range activeDirsLocked("guild07", time.Now()) {
		if d.SetBy == "market" {
			t.Fatal("market direction in force with market_directions_on off")
		}
	}
}

func TestMarketNeverExclusiveAndUnknownBy(t *testing.T) {
	marketTune(t, true)
	resetGuildDirs(t, "guild08")
	if c := postDir(t, "guild08", `{"kind":"supply","by":"market","exclusive":true}`); c != 400 {
		t.Fatalf("exclusive market: %d, want 400", c)
	}
	if c := postDir(t, "guild08", `{"kind":"supply","by":"puppet"}`); c != 400 {
		t.Fatalf("unknown by: %d, want 400", c)
	}
}

func TestMarketExclusiveLeaderBlocksMarket(t *testing.T) {
	marketTune(t, true)
	resetGuildDirs(t, "guild09")
	postDir(t, "guild09", `{"kind":"wealth","exclusive":true,"hours":4}`)
	postDir(t, "guild09", `{"kind":"supply","by":"market","hours":4}`)
	for _, d := range activeDirsLocked("guild09", time.Now()) {
		if d.SetBy == "market" {
			t.Fatal("market direction beside an exclusive leader direction")
		}
	}
}

func TestMarketClearedByLeaderAndExpires(t *testing.T) {
	marketTune(t, true)
	resetGuildDirs(t, "guild10")
	postDir(t, "guild10", `{"kind":"supply","by":"market","hours":4}`)
	gduty.Lock()
	id := gduty.market["guild10"][0].ID
	gduty.Unlock()
	// remove by id clears the market direction and leaves the leader list alone
	postDir(t, "guild10", `{"kind":"wealth","hours":4}`)
	if c := postDir(t, "guild10", `{"remove":"`+id+`"}`); c != 200 {
		t.Fatalf("remove market by id: %d", c)
	}
	if l, m := counts("guild10"); l != 1 || m != 0 {
		t.Fatalf("after remove: leader %d market %d, want 1 and 0", l, m)
	}
	postDir(t, "guild10", `{"kind":"supply","by":"market","hours":4}`)
	if c := postDir(t, "guild10", `{"clear":true}`); c != 200 {
		t.Fatalf("clear: %d", c)
	}
	if l, m := counts("guild10"); l != 0 || m != 0 {
		t.Fatalf("after clear: leader %d market %d, want 0 and 0", l, m)
	}
	// an expired market direction is dropped by the next read
	gduty.Lock()
	gduty.market["guild10"] = []*GuildDirection{{ID: "old", Guild: "guild10", Kind: "supply", SetBy: "market",
		Expires: time.Now().Add(-time.Minute)}}
	gduty.Unlock()
	activeDirsLocked("guild10", time.Now())
	if _, m := counts("guild10"); m != 0 {
		t.Fatalf("expired market direction kept: %d", m)
	}
}

func TestMarketDirectionPersists(t *testing.T) {
	marketTune(t, true)
	resetGuildDirs(t, "guild02")
	if c := postDir(t, "guild02", `{"kind":"supply","by":"market","hours":4}`); c != 200 {
		t.Fatalf("market post: %d", c)
	}
	dutySave()
	gduty.Lock()
	gduty.market["guild02"] = nil
	gduty.loaded = false
	gduty.Unlock()
	dutyLoad()
	if _, m := counts("guild02"); m != 1 {
		t.Fatalf("market directions after restart: %d, want 1", m)
	}
}

func TestMarketDirectionNotAnnouncedInLeaderVoice(t *testing.T) {
	marketTune(t, true)
	resetGuildDirs(t, "guild03")
	// the announce goroutine is started only for leader directions; a market post must leave no leader-set entry
	if c := postDir(t, "guild03", `{"kind":"supply","by":"market","hours":4}`); c != 200 {
		t.Fatalf("market post: %d", c)
	}
	if l, _ := counts("guild03"); l != 0 {
		t.Fatalf("a market post appeared in the leader list: %d", l)
	}
}
