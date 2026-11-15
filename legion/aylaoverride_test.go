package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ovWorld: good_map (lv60 monsters: fair and fit for base 60) and low_map (lv30 monsters: under the fair line of base 60).
func ovWorld(t *testing.T) {
	t.Helper()
	swapFairTables(t)
	mobs = map[int]*Mob{1: {ID: 1, Level: 60}, 2: {ID: 2, Level: 30}}
	spawnsByMap = map[string][]Spawn{
		"good_map": {{"good_map", 1, 30}},
		"low_map":  {{"low_map", 2, 30}},
	}
	maps = map[string]*GameMap{"good_map": {}, "low_map": {}} // the engine's known map table
}

// ovUse makes s the process store for one test (aylaDirective and the API read it).
func ovUse(t *testing.T, s *aylaOvStore) {
	t.Helper()
	old := aylaOvCur
	aylaOvCur = s
	t.Cleanup(func() { aylaOvCur = old })
}

func ovAudit(t *testing.T, dir string) []aylaOvAudit {
	t.Helper()
	f, err := os.Open(aylaOvAuditPath(dir))
	if err != nil {
		return nil
	}
	defer f.Close()
	var rows []aylaOvAudit
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var a aylaOvAudit
		if err := json.Unmarshal(sc.Bytes(), &a); err != nil {
			t.Fatalf("audit line not JSON: %q", sc.Text())
		}
		rows = append(rows, a)
	}
	return rows
}

func TestAylaOverrideExpiry(t *testing.T) {
	dir := t.TempDir()
	s, err := loadAylaOverrideStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	o, err := s.Set(ovPartyOrder, "rally at prontera", "", 1, 0, 0, false, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !o.ExpiresAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("expires %v, want %v", o.ExpiresAt, t0.Add(time.Hour))
	}
	if _, ok := s.Active(ovPartyOrder, t0.Add(59*time.Minute)); !ok {
		t.Fatal("override not in force before expiry")
	}
	if _, ok := s.Active(ovPartyOrder, t0.Add(time.Hour)); ok {
		t.Fatal("override still in force at its expiry time")
	}
	// the expiry is logged once, and the row is no longer active
	rows := s.List(t0.Add(2 * time.Hour))
	if len(rows) != 1 || rows[0].Active || rows[0].EndReason != "expire" {
		t.Fatalf("after expiry rows = %+v, want one inactive row ended by expire", rows)
	}
	s.List(t0.Add(3 * time.Hour))
	n := 0
	for _, a := range ovAudit(t, dir) {
		if a.Action == "expire" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expire lines = %d, want 1", n)
	}
	// hours above the cap are refused
	if _, err := s.Set(ovRally, "x", "", ovMaxHours+1, 0, 0, false, t0); err == nil {
		t.Fatal("hours above the cap accepted")
	}
}

func TestAylaDirectivePrecedence(t *testing.T) {
	dir := t.TempDir()
	s, _ := loadAylaOverrideStore(dir)
	ovUse(t, s)
	t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	// nothing set: puppet beats default, default is used without a puppet, none without either
	if d := aylaDirective(ovGuildDir, t0, "puppet dir", "default dir"); d.Source != "puppet" || d.Value != "puppet dir" {
		t.Fatalf("no owner: %+v, want puppet", d)
	}
	if d := aylaDirective(ovGuildDir, t0, "", "default dir"); d.Source != "default" {
		t.Fatalf("no owner, no puppet: %+v, want default", d)
	}
	if d := aylaDirective(ovGuildDir, t0, "", ""); d.Source != "none" {
		t.Fatalf("nothing at all: %+v, want none", d)
	}

	// owner beats puppet and default
	if _, err := s.Set(ovGuildDir, "gather Elunium", "", 2, 0, 0, false, t0); err != nil {
		t.Fatal(err)
	}
	d := aylaDirective(ovGuildDir, t0.Add(time.Minute), "puppet dir", "default dir")
	if d.Source != "owner" || d.Value != "gather Elunium" || d.OverrideID == "" {
		t.Fatalf("owner set: %+v, want owner", d)
	}
	// other kinds are not affected by this override
	if d := aylaDirective(ovFarmMap, t0, "", "pay_fild08"); d.Source != "default" {
		t.Fatalf("farm_map picked up a guild_dir override: %+v", d)
	}
	// after the owner's time is up the puppet directive is back in force
	if d := aylaDirective(ovGuildDir, t0.Add(3*time.Hour), "puppet dir", "default dir"); d.Source != "puppet" {
		t.Fatalf("after expiry: %+v, want puppet", d)
	}
}

func TestAylaOverrideAtomicAndReload(t *testing.T) {
	dir := t.TempDir()
	s, err := loadAylaOverrideStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	o, err := s.Set(ovRally, "meet at kafra", "test", 6, 0, 0, false, t0)
	if err != nil {
		t.Fatal(err)
	}
	// no temp files left behind
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
	// a failed write (folder missing) changes nothing in memory
	bad, _ := loadAylaOverrideStore(filepath.Join(dir, "missing"))
	if _, err := bad.Set(ovRally, "x", "", 1, 0, 0, false, t0); err == nil {
		t.Fatal("set into a missing folder reported success")
	}
	if len(bad.List(t0)) != 0 {
		t.Fatal("failed set changed the in-memory rows")
	}
	// a restart reloads the same override from disk
	again, err := loadAylaOverrideStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := again.Active(ovRally, t0.Add(time.Hour))
	if !ok || got.ID != o.ID || got.Value != "meet at kafra" || got.Note != "test" {
		t.Fatalf("after reload: %+v ok=%v, want %+v", got, ok, o)
	}
	// an unreadable file is moved aside, not overwritten
	os.WriteFile(aylaOvPath(dir), []byte("{not json"), 0644)
	broke, err := loadAylaOverrideStore(dir)
	if err == nil || len(broke.List(t0)) != 0 {
		t.Fatalf("corrupt file: err=%v rows=%d, want error and empty store", err, len(broke.List(t0)))
	}
	if _, err := os.Stat(aylaOvPath(dir)); !os.IsNotExist(err) {
		t.Fatal("corrupt file was not moved aside")
	}
}

func TestAylaFarmMapGateWarnsAndConfirm(t *testing.T) {
	ovWorld(t)
	dir := t.TempDir()
	s, _ := loadAylaOverrideStore(dir)
	t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	// the good map passes for base 60
	if w := aylaFarmMapWarnings("good_map", 60, 64); len(w) != 0 {
		t.Fatalf("good_map warnings: %v", w)
	}
	// the low map warns (fair gate) and is refused without confirm, nothing stored
	w := aylaFarmMapWarnings("low_map", 60, 64)
	if len(w) == 0 || !strings.Contains(strings.Join(w, ";"), "fair gate") {
		t.Fatalf("low_map warnings = %v, want fair gate", w)
	}
	_, err := s.Set(ovFarmMap, "low_map", "", 4, 60, 64, false, t0)
	var nc *ovNeedsConfirm
	if !errors.As(err, &nc) || len(nc.Warnings) == 0 {
		t.Fatalf("set without confirm: err=%v, want ovNeedsConfirm", err)
	}
	if _, ok := s.Active(ovFarmMap, t0); ok {
		t.Fatal("refused farm map was stored")
	}
	// accepted with confirm, and the warnings go to the audit line
	if _, err := s.Set(ovFarmMap, "low_map", "owner wants it", 4, 60, 64, true, t0); err != nil {
		t.Fatalf("set with confirm: %v", err)
	}
	if o, ok := s.Active(ovFarmMap, t0); !ok || o.Value != "low_map" {
		t.Fatalf("confirmed farm map not in force: %+v ok=%v", o, ok)
	}
	a := ovAudit(t, dir)
	if len(a) != 1 || a[0].Action != "set" || !a[0].Confirm || len(a[0].Warnings) == 0 {
		t.Fatalf("audit = %+v, want one set line with confirm and warnings", a)
	}
	// without a level the gates are not judged, and that is a warning
	if w := aylaFarmMapWarnings("good_map", 0, 0); len(w) != 1 || !strings.Contains(w[0], "base_lv") {
		t.Fatalf("no level: %v", w)
	}
	// a map with no spawn data is a warning too
	if w := aylaFarmMapWarnings("nowhere", 60, 64); len(w) != 1 || !strings.Contains(w[0], "no monster spawn data") {
		t.Fatalf("unknown map: %v", w)
	}
	// bad map names never reach the store
	if _, err := s.Set(ovFarmMap, "../etc", "", 1, 0, 0, true, t0); err == nil {
		t.Fatal("bad map name accepted")
	}
}

func TestAylaOverrideAuditLines(t *testing.T) {
	ovWorld(t)
	dir := t.TempDir()
	s, _ := loadAylaOverrideStore(dir)
	t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	first, err := s.Set(ovTownRun, "town run now", "", 1, 0, 0, false, t0)
	if err != nil {
		t.Fatal(err)
	}
	// a second set of the same kind replaces the first
	if _, err := s.Set(ovTownRun, "town run later", "", 1, 0, 0, false, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// clear the second
	cur, _ := s.Active(ovTownRun, t0.Add(2*time.Minute))
	if _, err := s.Clear(cur.ID, t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Clear(cur.ID, t0.Add(2*time.Minute)); !errors.Is(err, errOvNotFound) {
		t.Fatalf("second clear: %v, want not found", err)
	}
	// an expiry on a fresh set
	if _, err := s.Set(ovRally, "rally 1", "", 1, 0, 0, false, t0); err != nil {
		t.Fatal(err)
	}
	s.List(t0.Add(5 * time.Hour))

	a := ovAudit(t, dir)
	want := []string{"set", "set", "clear", "set", "expire"}
	if len(a) != len(want) {
		t.Fatalf("audit lines = %d (%+v), want %d", len(a), a, len(want))
	}
	for i, w := range want {
		if a[i].Action != w {
			t.Errorf("line %d action %q, want %q", i, a[i].Action, w)
		}
	}
	if a[1].Replaced != first.ID {
		t.Errorf("replacement line replaced=%q, want %q", a[1].Replaced, first.ID)
	}
}

// the API: POST gates a farm map (409 with warnings), confirm stores it, DELETE clears it, GET shows both layers.
func TestAylaOverridesAPI(t *testing.T) {
	ovWorld(t)
	dir := t.TempDir()
	oldData := *dataDir
	*dataDir = dir
	t.Cleanup(func() { *dataDir = oldData })
	s, _ := loadAylaOverrideStore(dir)
	ovUse(t, s)
	mux := http.NewServeMux()
	registerAylaOverrides(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tok, err := loadOwnerToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	post := func(body string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/ayla/overrides", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Owner-Token", tok)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	// without the token the write is refused and nothing is stored
	r0, _ := http.Post(srv.URL+"/api/ayla/overrides", "application/json", strings.NewReader(`{"kind":"rally","value":"x","hours":1}`))
	r0.Body.Close()
	if r0.StatusCode != http.StatusForbidden {
		t.Fatalf("POST without token: %d, want 403", r0.StatusCode)
	}
	if len(s.List(time.Now())) != 0 {
		t.Fatal("refused POST stored a row")
	}
	r := post(`{"kind":"farm_map","value":"low_map","base_lv":60,"max_lv":64}`)
	if r.StatusCode != http.StatusConflict {
		t.Fatalf("gated POST status %d, want 409", r.StatusCode)
	}
	r.Body.Close()
	r = post(`{"kind":"farm_map","value":"low_map","base_lv":60,"max_lv":64,"confirm":true}`)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("confirmed POST status %d", r.StatusCode)
	}
	var o AylaOverride
	json.NewDecoder(r.Body).Decode(&o)
	r.Body.Close()

	g, _ := http.Get(srv.URL + "/api/ayla/overrides")
	var v ovView
	json.NewDecoder(g.Body).Decode(&v)
	g.Body.Close()
	if v.Owner[ovFarmMap] == nil || v.Owner[ovFarmMap].ID != o.ID || v.Precedence != "owner > puppet > default" {
		t.Fatalf("GET owner view: %+v", v.Owner)
	}

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/ayla/overrides/"+o.ID, nil)
	d0, _ := http.DefaultClient.Do(req) // no token
	d0.Body.Close()
	if d0.StatusCode != http.StatusForbidden {
		t.Fatalf("DELETE without token: %d, want 403", d0.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/ayla/overrides/"+o.ID, nil)
	req.Header.Set("X-Owner-Token", tok)
	d, _ := http.DefaultClient.Do(req)
	d.Body.Close()
	if d.StatusCode != http.StatusOK {
		t.Fatalf("DELETE with token: %d", d.StatusCode)
	}
	if _, ok := s.Active(ovFarmMap, time.Now()); ok {
		t.Fatal("cleared override still active")
	}
	if n := len(ovAudit(t, dir)); n != 2 {
		t.Fatalf("audit lines = %d, want 2 (set, clear)", n)
	}
}

// the owner token: created once in the data folder, 64 hex characters, kept across calls
func TestOwnerTokenCreatedOnce(t *testing.T) {
	dir := t.TempDir()
	a, err := loadOwnerToken(dir)
	if err != nil || len(a) != 64 {
		t.Fatalf("token %q err %v", a, err)
	}
	b, _ := loadOwnerToken(dir)
	if a != b {
		t.Fatal("token changed between calls")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "owner.token"))
	if strings.TrimSpace(string(raw)) != a {
		t.Fatal("token file does not hold the token")
	}
}

// the GET stays open; a write with a wrong token is 403
func TestAylaOverridesGetOpenWriteNeedsToken(t *testing.T) {
	ovWorld(t)
	dir := t.TempDir()
	oldData := *dataDir
	*dataDir = dir
	t.Cleanup(func() { *dataDir = oldData })
	s, _ := loadAylaOverrideStore(dir)
	ovUse(t, s)
	mux := http.NewServeMux()
	registerAylaOverrides(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	if r, _ := http.Get(srv.URL + "/api/ayla/overrides"); r.StatusCode != http.StatusOK {
		t.Fatalf("GET status %d, want 200", r.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/ayla/overrides", strings.NewReader(`{"kind":"rally","value":"x","hours":1}`))
	req.Header.Set("X-Owner-Token", "wrong")
	r, _ := http.DefaultClient.Do(req)
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong token: %d, want 403", r.StatusCode)
	}
}

// a write whose audit line fails is still stored and reported as a success
func TestAylaOverrideAuditFailureKeepsSet(t *testing.T) {
	dir := t.TempDir()
	s, _ := loadAylaOverrideStore(dir)
	// the audit path is a folder, so the append fails after the save
	if err := os.Mkdir(aylaOvAuditPath(dir), 0755); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	o, err := s.Set(ovRally, "meet", "", 1, 0, 0, false, t0)
	if err != nil {
		t.Fatalf("set with failing audit returned %v, want success", err)
	}
	again, _ := loadAylaOverrideStore(dir)
	if got, ok := again.Active(ovRally, t0); !ok || got.ID != o.ID {
		t.Fatal("override not on disk after the audit failure")
	}
}

// Clear on a row whose time is up but not yet swept: swept now, expire logged, expired state returned
func TestAylaClearExpiredUnswept(t *testing.T) {
	dir := t.TempDir()
	s, _ := loadAylaOverrideStore(dir)
	t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	o, _ := s.Set(ovRally, "meet", "", 1, 0, 0, false, t0)
	got, err := s.Clear(o.ID, t0.Add(2*time.Hour))
	if !errors.Is(err, errOvExpired) || got.EndReason != "expire" || got.Active {
		t.Fatalf("clear after expiry: %+v err=%v, want expired state", got, err)
	}
	n := 0
	for _, a := range ovAudit(t, dir) {
		if a.Action == "expire" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expire lines = %d, want 1", n)
	}
}

// farm_map must be a map the engine knows, even with confirm=true
func TestAylaFarmMapUnknownMapRefused(t *testing.T) {
	ovWorld(t)
	s, _ := loadAylaOverrideStore(t.TempDir())
	t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	if _, err := s.Set(ovFarmMap, "pay_fidl08", "", 1, 60, 64, true, t0); err == nil {
		t.Fatal("typo map accepted with confirm=true")
	}
	if len(s.List(t0)) != 0 {
		t.Fatal("refused map stored")
	}
}

// two store instances on one folder never lose a row, whichever writes second
func TestAylaOverrideTwoInstancesNoLoss(t *testing.T) {
	ovWorld(t)
	dir := t.TempDir()
	a, _ := loadAylaOverrideStore(dir)
	b, _ := loadAylaOverrideStore(dir) // loaded before a writes
	t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	if _, err := a.Set(ovRally, "first", "", 2, 0, 0, false, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Set(ovTownRun, "second", "", 2, 0, 0, false, t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Set(ovPartyOrder, "third", "", 2, 0, 0, false, t0.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	final, _ := loadAylaOverrideStore(dir)
	if n := len(final.List(t0.Add(3 * time.Second))); n != 3 {
		t.Fatalf("rows on disk = %d, want 3", n)
	}
	// a reader instance sees the other writer's row through the mtime check
	if _, ok := a.Active(ovTownRun, t0.Add(3*time.Second)); !ok {
		t.Fatal("instance a does not see the row written by b")
	}
}
