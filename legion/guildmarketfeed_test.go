package main

import (
	"reflect"
	"testing"
	"time"
)

// WP5 (guildmarketfeed.go): leaders read the market feed as information. Every test runs under a temp data folder (G23)
// and with its own gmf store, so no live pick leaks in or out.

var gmfNow = time.Date(2026, 11, 15, 12, 0, 0, 0, time.Local)

func gmfTune(t *testing.T, on bool) {
	t.Helper()
	old := tune()
	cp := *old
	cp.GuildMarketFeedOn = on
	curTuning.Store(&cp)
	t.Cleanup(func() { curTuning.Store(old) })
}

// gmfIsolate: an empty pick store for the test, restored afterwards; the duty files go to a temp folder
func gmfIsolate(t *testing.T) {
	t.Helper()
	dir := *dataDir
	*dataDir = t.TempDir()
	gduty.Lock()
	saved := gduty.gmf
	gduty.gmf = map[string]gmfPick{}
	gduty.dirty, gduty.loaded = false, true
	gduty.Unlock()
	t.Cleanup(func() {
		gduty.Lock()
		gduty.gmf = saved
		gduty.Unlock()
		*dataDir = dir
	})
}

// gmfFeedOf: Alpha (501) is wanted in full, Beta (502) has a gap of `betaGap` units; both sell at 1000z and drop on
// three maps. Guild needs: name -> units.
func gmfFeedOf(betaGap int, guildNeeds map[string]map[string]int) *MEFeed {
	f := &MEFeed{Status: "ok", Short: []MEFeedItem{
		{ItemID: 501, Item: "Alpha", Kind: "short", Demand: 10, Listed: 0, Price: 1000, DropMaps: []string{"m1", "m2", "m3"}},
		{ItemID: 502, Item: "Beta", Kind: "short", Demand: 10, Listed: 10 - betaGap, Price: 1000, DropMaps: []string{"m1", "m2", "m3"}},
	}, Guilds: map[string]MEFeedGuild{}}
	for g, needs := range guildNeeds {
		v := MEFeedGuild{ID: g, Needs: []MEFeedGuildNeed{}}
		for name, n := range needs {
			v.Needs = append(v.Needs, MEFeedGuildNeed{Item: name, N: n})
		}
		f.Guilds[g] = v
	}
	return f
}

func TestGMFFlagOffChangesNothing(t *testing.T) {
	gmfTune(t, false)
	gmfIsolate(t)
	prev := meState.feed
	meState.Lock()
	meState.feed = gmfFeedOf(7, nil)
	meState.Unlock()
	defer func() {
		meState.Lock()
		meState.feed = prev
		meState.Unlock()
	}()
	if gmfFeed() != nil {
		t.Fatal("gmfFeed returned a feed with guild_market_feed_on off")
	}
	dirs := autopilotDirs("guild01", nil, false, nil, gmfNow)
	before := make([]GuildDirection, len(dirs))
	for i, d := range dirs {
		before[i] = *d
	}
	out := gmfApply("guild01", dirs, gmfFeedOf(7, nil), gmfNow)
	if len(out) != len(before) {
		t.Fatalf("flag off changed the direction count: %d -> %d", len(before), len(out))
	}
	for i := range out {
		if !reflect.DeepEqual(*out[i], before[i]) {
			t.Fatalf("flag off changed direction %d: %+v -> %+v", i, before[i], *out[i])
		}
	}
	gduty.Lock()
	n := len(gduty.gmf)
	gduty.Unlock()
	if n != 0 {
		t.Fatalf("flag off stored %d picks", n)
	}
}

func TestGMFShortItemRaisesWeightAndIsChosen(t *testing.T) {
	gmfIsolate(t)
	f := gmfFeedOf(7, nil) // Beta is short too (gap 7); Alpha is wanted in full
	sc := gmfScoreItems("guild01", f, nil)
	if sc[501] <= 0 || sc[502] <= 0 {
		t.Fatalf("short items must score above zero: %v", sc)
	}
	// an item that is not short scores nothing: a full market is not a want
	full := gmfFeedOf(7, nil)
	full.Short[0].Listed = 10
	if _, ok := gmfScoreItems("guild01", full, nil)[501]; ok {
		t.Fatal("an item with no gap is scored")
	}
	if got := gmfChoose("guild01", f, gmfNow); got != 501 {
		t.Fatalf("pick %d, want 501 (the bigger gap)", got)
	}
	if got := gmfChoose("guild01", &MEFeed{Status: "ok"}, gmfNow); got != 0 {
		t.Fatalf("no short item: pick %d, want none", got)
	}
}

func TestGMFHerdPenalty(t *testing.T) {
	gmfIsolate(t)
	f := gmfFeedOf(7, nil)
	if got := gmfChoose("guild01", f, gmfNow); got != 501 {
		t.Fatalf("guild01 picks %d, want 501", got)
	}
	// without the herd penalty guild03 would take 501 too (its gap weight favours the bigger gap)
	if got := gmfScoreItems("guild03", f, nil); got[501] <= got[502] {
		t.Fatalf("test setup: guild03 alone should prefer 501: %v", got)
	}
	if got := gmfChoose("guild03", f, gmfNow); got != 502 {
		t.Fatalf("guild03 picks %d, want 502: guild01 chases 501 and the penalty must spread the guilds", got)
	}
}

func TestGMFHysteresisHold(t *testing.T) {
	gmfIsolate(t)
	t0 := gmfNow
	// at t0 only Beta is short, so guild07 takes Beta
	only := gmfFeedOf(7, nil)
	only.Short = only.Short[1:]
	if got := gmfChoose("guild07", only, t0); got != 502 {
		t.Fatalf("t0 pick %d, want 502", got)
	}
	// an hour later Alpha is short too and scores much higher: the hold keeps Beta
	both := gmfFeedOf(7, nil)
	if got := gmfChoose("guild07", both, t0.Add(time.Hour)); got != 502 {
		t.Fatalf("inside the 24 h hold the pick moved to %d, want 502", got)
	}
	// after the hold a clearly better item (over 1.25 times) replaces it
	if got := gmfChoose("guild07", both, t0.Add(25*time.Hour)); got != 501 {
		t.Fatalf("after the hold pick %d, want 501", got)
	}
}

func TestGMFHeldItemDroppedWhenNoLongerShort(t *testing.T) {
	gmfIsolate(t)
	t0 := gmfNow
	if got := gmfChoose("guild07", gmfFeedOf(7, nil), t0); got != 501 {
		t.Fatalf("t0 pick %d, want 501", got)
	}
	full := gmfFeedOf(7, nil)
	full.Short[0].Listed = 10 // Alpha is covered now; Beta still short
	if got := gmfChoose("guild07", full, t0.Add(time.Hour)); got != 502 {
		t.Fatalf("a covered pick is dropped at once and the next short item taken: got %d, want 502", got)
	}
}

func TestGMFPersonalitiesDiffer(t *testing.T) {
	gmfIsolate(t)
	f := gmfFeedOf(7, map[string]map[string]int{"guild04": {"Beta": 5}}) // guild04's members all wait for Beta
	if got := gmfChoose("guild03", f, gmfNow); got != 501 {
		t.Fatalf("guild03 (gap weight) picks %d, want 501", got)
	}
	if got := gmfChoose("guild04", f, gmfNow); got != 502 {
		t.Fatalf("guild04 (need weight) picks %d, want 502", got)
	}
}

func TestGMFApplySetsWealthItem(t *testing.T) {
	gmfTune(t, true)
	gmfIsolate(t)
	dirs := autopilotDirs("guild01", nil, false, nil, gmfNow)
	if len(dirs) != 1 || dirs[0].Kind != dirWealth {
		t.Fatalf("setup: want only the wealth direction, got %d", len(dirs))
	}
	if dirs[0].ItemID != 0 {
		t.Fatal("setup: the autopilot wealth direction has an item")
	}
	out := gmfApply("guild01", dirs, gmfFeedOf(7, nil), gmfNow)
	var w *GuildDirection
	for _, d := range out {
		if d.Kind == dirWealth {
			w = d
		}
	}
	if w == nil || w.ItemID != 501 || w.ID != "guild01-auto-wealth" || w.SetBy != "autopilot" {
		t.Fatalf("wealth direction after the feed: %+v", w)
	}
	// no feed: nothing changes
	same := gmfApply("guild02", autopilotDirs("guild02", nil, false, nil, gmfNow), nil, gmfNow)
	if same[0].ItemID != 0 {
		t.Fatal("a nil feed changed the autopilot")
	}
}

func TestGMFPickPersistsAndOldFileLoads(t *testing.T) {
	gmfTune(t, true)
	gmfIsolate(t)
	if got := gmfChoose("guild03", gmfFeedOf(7, nil), gmfNow); got == 0 {
		t.Fatal("no pick to persist")
	}
	gduty.Lock()
	gduty.dirty = true
	gduty.Unlock()
	dutySave()
	want := gduty.gmf["guild03"]
	gduty.Lock()
	gduty.gmf = map[string]gmfPick{}
	gduty.loaded = false
	gduty.Unlock()
	dutyLoad()
	gduty.Lock()
	got, ok := gduty.gmf["guild03"]
	gduty.Unlock()
	if !ok || got.ItemID != want.ItemID || !got.Since.Equal(want.Since) {
		t.Fatalf("pick after restart: %+v, want %+v", got, want)
	}
}
