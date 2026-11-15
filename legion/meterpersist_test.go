package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A bot's counters written by meterPersist come back after a "restart" on its first login, inside every window.
func TestMeterPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	oldData := *dataDir
	*dataDir = dir
	defer func() { *dataDir = oldData }()
	oldAyla := meterAylaS
	defer func() { meterAylaS = oldAyla }()
	meterAylaS.samples, meterAylaS.sess, meterAylaS.last = nil, aylaWin{}, nil
	meterPending.Range(func(k, _ interface{}) bool { meterPending.Delete(k); return true })

	now := time.Now()
	b := NewBot(&Persona{AccountID: 1, Chars: []CharSpec{{Name: "Tester"}}})
	b.meterLogin(now.Add(-40 * time.Minute))
	for i := 0; i < 30; i++ { // 30 kills over 30 minutes, one per minute
		at := now.Add(time.Duration(-30+i) * time.Minute)
		b.meter.record(at, func(s *meterSlot) { s.Kills++; s.Fair++; s.Sec[mbFight] += 60 })
	}
	meterAylaS.samples = []aylaSample{{t: now.Add(-time.Minute), zeny: 5, online: true, kills: 3, ksince: 7}}
	meterAylaS.sess = aylaWin{Min: 12, Kills: 9, Sec: map[string]float64{"hunting": 720}}
	meterPersist([]*Bot{b}, now)
	if _, err := os.Stat(filepath.Join(dir, "meter_state.json.gz")); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(m) != 0 {
		t.Fatalf("temp file left: %v", m)
	}

	// restart: fresh process state
	meterAylaS.samples, meterAylaS.sess = nil, aylaWin{}
	later := now.Add(3 * time.Minute)
	f, err := meterReadState(filepath.Join(dir, "meter_state.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if n := meterApplyState(f, later); n != 1 {
		t.Fatalf("pending %d", n)
	}
	if len(meterAylaS.samples) != 1 || meterAylaS.samples[0].kills != 3 || meterAylaS.sess.Kills != 9 {
		t.Fatalf("ayla not restored: %+v %+v", meterAylaS.samples, meterAylaS.sess)
	}
	// a snapshot written before the bot logs in again keeps its pending counters
	meterPersist(nil, later)
	f2, _ := meterReadState(filepath.Join(dir, "meter_state.json.gz"))
	if len(f2.Bots) != 1 || f2.Bots[0].Char != "Tester" {
		t.Fatalf("pending bot lost from the next snapshot: %+v", f2.Bots)
	}

	nb := NewBot(&Persona{AccountID: 1, Chars: []CharSpec{{Name: "Tester"}}})
	nb.meterLogin(later)
	w60 := nb.meter.sumWin(later.Unix()/60, 60)
	if w60.Kills != 30 || w60.Sec[mbFight] != 1800 {
		t.Fatalf("1h window after restart: kills %d fight %v", w60.Kills, w60.Sec[mbFight])
	}
	if nb.meter.sess.Kills != 30 || !nb.meter.sessFrom.Before(now) {
		t.Fatalf("session not restored: %+v from %v", nb.meter.sess, nb.meter.sessFrom)
	}
	if _, ok := meterPending.Load("tester"); ok {
		t.Fatal("restore must consume the pending entry")
	}
	// a second login does not restore twice
	nb.meterLogin(later)
	if nb.meter.sumWin(later.Unix()/60, 60).Kills != 30 {
		t.Fatal("double restore")
	}
}

// A snapshot older than the session keep restores nothing; slots older than 60 min fall out.
func TestMeterPersistStale(t *testing.T) {
	meterPending.Range(func(k, _ interface{}) bool { meterPending.Delete(k); return true })
	oldAyla := meterAylaS
	defer func() { meterAylaS = oldAyla }()
	now := time.Now()
	f := &meterSaveFile{V: meterStateVersion, Saved: now.Add(-3 * time.Hour).Unix(),
		Bots: []meterSaveBot{{Char: "Old", From: now.Add(-4 * time.Hour).Unix(), Ring: []meterSlot{{Min: now.Add(-3*time.Hour).Unix() / 60, Kills: 5}}}}}
	if n := meterApplyState(f, now); n != 0 {
		t.Fatalf("stale snapshot restored %d", n)
	}
	f = &meterSaveFile{V: meterStateVersion, Saved: now.Add(-90 * time.Minute).Unix(),
		Bots: []meterSaveBot{{Char: "Mid", From: now.Add(-3 * time.Hour).Unix(), Sess: meterSlot{Kills: 50},
			Ring: []meterSlot{{Min: now.Add(-90*time.Minute).Unix() / 60, Kills: 5}}}}}
	meterApplyState(f, now)
	var c meterCounters
	c.reset("Mid", now)
	if !meterRestoreInto(&c, "Mid", now) || c.sumWin(now.Unix()/60, 60).Kills != 0 || c.sess.Kills != 50 {
		t.Fatalf("90-min-old snapshot: window %d sess %d", c.sumWin(now.Unix()/60, 60).Kills, c.sess.Kills)
	}
	if meterApplyState(&meterSaveFile{V: 99, Saved: now.Unix()}, now) != 0 {
		t.Fatal("unknown version must be ignored")
	}
}

// Ayla's kills/min from statusout.pl's running exp-gain count; a new OpenKore process (kills_since) adds no jump.
func TestMeterAylaKills(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "status.json")
	t.Setenv("LEGION_HUMAN_STATUS", f)
	old := meterAylaS
	defer func() { meterAylaS = old }()
	meterAylaS.samples, meterAylaS.sess, meterAylaS.last = nil, aylaWin{}, nil
	t0 := time.Now().Add(-20 * time.Minute)
	write := func(at time.Time, kills, since int64) {
		b, _ := json.Marshal(humanStatus{Name: "Ayla", Online: true, HP: 10, MaxHP: 10, Zeny: 100, Map: "pay_dun01",
			BaseLevel: 83, JobID: 12, Guild: "Starveil", TS: at.Unix(), humanExtras: humanExtras{Kills: kills, KillsSince: since, ExpH: 13450}})
		os.WriteFile(f, b, 0644)
		meterAylaSample(at)
	}
	k := int64(0)
	for i := 0; i <= 80; i++ { // 20 min, a sample every 15 s, one kill every 30 s = 2 kills/min
		if i%2 == 1 {
			k++
		}
		since := int64(1000)
		if i == 40 { // OpenKore restarted: new count from 0
			k = 0
		}
		if i >= 40 {
			since = 2000
		}
		write(t0.Add(time.Duration(i)*15*time.Second), k, since)
	}
	end := t0.Add(20 * time.Minute)
	w := aylaWindow(end, time.Hour)
	if w.Kills != 40 { // 20 before and 20 after the restart; the restart step itself adds none
		t.Fatalf("kills %d", w.Kills)
	}
	v := meterBuild(nil, end)
	aw := v.Ayla.Windows["1h"]
	if !aw.Counted || aw.KPM < 1.9 || aw.KPM > 2.0 || v.Ayla.ExpH != 13450 {
		t.Fatalf("ayla 1h %+v exp/h %d", aw, v.Ayla.ExpH)
	}
	n := meterNeo(v, mw60)
	found := false
	for _, m := range n.Members {
		if m.Name == "Ayla" && m.Counted && m.KPM == aw.KPM && m.Instance == "openkore" {
			found = true
		}
	}
	if !found || n.Party.N != 1 {
		t.Fatalf("Ayla not counted in her party: %+v %+v", n.Members, n.Party)
	}
}
