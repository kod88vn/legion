package main

// aylaoverride.go: the owner's overrides for Ayla's puppet-master directives (AY25, owner 2026-11-15: the owner can
// override any puppet-master directive for Ayla - farm map, guild direction, party orders and the like - until he
// clears it or it expires; every override and clear is logged). Package A of the Ayla-on-engine plan: store, API and
// precedence only. Nothing in the party, AI or guild code calls this yet (package B does that).
//
// Store: <data>/ayla_overrides.json (atomic write: temp file in the same folder, fsync, rename), one row per override.
// Audit: <data>/ayla_overrides.jsonl, append-only, one line per set, replace, clear and expiry.
// At most one active override per kind: a new set of a kind ends the previous active one (logged as "replaced").
//
// API (registered in api.go):
//   GET    /api/ayla/overrides                   owner overrides per kind + the puppet masters' current directives
//                                                (Starveil guild directions from guildduty, last owner and helper orders)
//   POST   /api/ayla/overrides                   {"kind":"farm_map","value":"pay_fild08","hours":24,"note":"...",
//                                                 "base_lv":50,"max_lv":54,"confirm":false}
//                                                farm_map is checked against the fair and fit gates; warnings answer
//                                                409 and nothing is stored unless "confirm":true
//   DELETE /api/ayla/overrides/{id}              clear an active override (or ?id=)
//
// Precedence (aylaDirective): owner override (active and not expired) > puppet-master directive > engine default.

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	ovFarmMap    = "farm_map"
	ovGuildDir   = "guild_dir"
	ovPartyOrder = "party_order"
	ovTownRun    = "town_run"
	ovRally      = "rally"

	ovDefaultHours = 24.0
	ovMaxHours     = 720.0 // 30 days
	ovMaxText      = 200
	ovHistoryKeep  = 50 // rows kept in the file (active ones always kept)
	ovOrdersShow   = 5  // owner orders and helper results shown in the GET
)

// ovKinds: the directive kinds an owner can override.
var ovKinds = []string{ovFarmMap, ovGuildDir, ovPartyOrder, ovTownRun, ovRally}

var ovMapRe = regexp.MustCompile(`^[a-z0-9_]{2,24}$`)

// AylaOverride is one owner override row.
type AylaOverride struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Value     string    `json:"value"`
	SetBy     string    `json:"set_by"`
	SetAt     time.Time `json:"set_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Note      string    `json:"note,omitempty"`
	Active    bool      `json:"active"`
	ClearedAt time.Time `json:"cleared_at,omitempty"`
	EndReason string    `json:"end_reason,omitempty"` // clear | expire | replaced
}

// AylaDirective is the result of the precedence: the value in force and where it came from.
type AylaDirective struct {
	Kind       string    `json:"kind"`
	Value      string    `json:"value"`
	Source     string    `json:"source"` // owner | puppet | default | none
	OverrideID string    `json:"override_id,omitempty"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
}

// ovNeedsConfirm: a farm_map set the gates object to; nothing was stored.
type ovNeedsConfirm struct{ Warnings []string }

func (e *ovNeedsConfirm) Error() string {
	return fmt.Sprintf("farm map fails the gates (%d warning(s)); resend with confirm=true to set it anyway", len(e.Warnings))
}

var (
	errOvNotFound = errors.New("no active override with that id")
	errOvExpired  = errors.New("override already expired (now swept)")
)

type aylaOvFile struct {
	Saved     time.Time      `json:"saved"`
	Overrides []AylaOverride `json:"overrides"`
}

// aylaOvAudit: one line of ayla_overrides.jsonl.
type aylaOvAudit struct {
	At        time.Time `json:"at"`
	Action    string    `json:"action"` // set | clear | expire
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Value     string    `json:"value,omitempty"`
	By        string    `json:"by,omitempty"`
	Note      string    `json:"note,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Replaced  string    `json:"replaced,omitempty"`
	Confirm   bool      `json:"confirm,omitempty"`
	Warnings  []string  `json:"warnings,omitempty"`
}

// aylaOvStore holds the overrides in memory and mirrors them to <dir>/ayla_overrides.json.
type aylaOvStore struct {
	mu    sync.Mutex
	dir   string
	rows  []AylaOverride
	mtime time.Time // file stamp of the rows in memory: a change on disk (another instance) is re-read
	size  int64
}

var (
	aylaOvCurMu sync.Mutex
	aylaOvCur   *aylaOvStore
)

// aylaOv: the process-wide store under *dataDir, loaded on first use.
func aylaOv() *aylaOvStore {
	aylaOvCurMu.Lock()
	defer aylaOvCurMu.Unlock()
	if aylaOvCur == nil {
		s, err := loadAylaOverrideStore(*dataDir)
		if err != nil {
			log.Printf("ayla overrides: load: %v", err)
		}
		aylaOvCur = s
	}
	return aylaOvCur
}

func aylaOvPath(dir string) string      { return filepath.Join(dir, "ayla_overrides.json") }
func aylaOvAuditPath(dir string) string { return filepath.Join(dir, "ayla_overrides.jsonl") }

// loadAylaOverrideStore reads the store from dir. A missing file is an empty store. An unreadable file is moved aside
// (never overwritten) and the store starts empty.
func loadAylaOverrideStore(dir string) (*aylaOvStore, error) {
	s := &aylaOvStore{dir: dir}
	raw, err := os.ReadFile(aylaOvPath(dir))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var f aylaOvFile
	if err := json.Unmarshal(raw, &f); err != nil {
		bad := fmt.Sprintf("%s.bad-%d", aylaOvPath(dir), time.Now().Unix())
		os.Rename(aylaOvPath(dir), bad)
		return s, fmt.Errorf("unreadable store moved to %s: %v", bad, err)
	}
	s.rows = f.Overrides
	s.stampLocked()
	return s, nil
}

// stampLocked records the file's mtime and size as the rows in memory now are.
func (s *aylaOvStore) stampLocked() {
	if st, err := os.Stat(aylaOvPath(s.dir)); err == nil {
		s.mtime, s.size = st.ModTime(), st.Size()
	} else {
		s.mtime, s.size = time.Time{}, 0
	}
}

// reloadLocked re-reads the file into memory. A file that cannot be read keeps the rows in memory (logged).
func (s *aylaOvStore) reloadLocked() {
	raw, err := os.ReadFile(aylaOvPath(s.dir))
	if os.IsNotExist(err) {
		s.rows = nil
		s.stampLocked()
		return
	}
	if err != nil {
		log.Printf("ayla overrides: reload: %v", err)
		return
	}
	var f aylaOvFile
	if err := json.Unmarshal(raw, &f); err != nil {
		log.Printf("ayla overrides: reload: unreadable file, keeping memory rows: %v", err)
		return
	}
	s.rows = f.Overrides
	s.stampLocked()
}

// refreshLocked re-reads the file when its mtime or size differs from what was last loaded or saved (another
// instance wrote it). Reads use it; mutations always reload first (reloadLocked), so no row is lost.
func (s *aylaOvStore) refreshLocked() {
	st, err := os.Stat(aylaOvPath(s.dir))
	if err != nil {
		if os.IsNotExist(err) && !s.mtime.IsZero() {
			s.reloadLocked()
		}
		return
	}
	if !st.ModTime().Equal(s.mtime) || st.Size() != s.size {
		s.reloadLocked()
	}
}

// saveLocked writes rows atomically: temp file in the same folder, fsync, rename over the old file.
func (s *aylaOvStore) saveLocked(rows []AylaOverride, now time.Time) error {
	tmp, err := os.CreateTemp(s.dir, "ayla_overrides-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", " ")
	if err := enc.Encode(aylaOvFile{Saved: now, Overrides: rows}); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, aylaOvPath(s.dir)); err != nil {
		os.Remove(name)
		return err
	}
	s.stampLocked()
	return nil
}

func (s *aylaOvStore) auditLocked(a aylaOvAudit) error {
	f, err := os.OpenFile(aylaOvAuditPath(s.dir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(a)
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// trimHistory keeps the newest ovHistoryKeep rows plus every active one.
func trimHistory(rows []AylaOverride) []AylaOverride {
	if len(rows) <= ovHistoryKeep {
		return rows
	}
	out := []AylaOverride{}
	cut := len(rows) - ovHistoryKeep
	for i, r := range rows {
		if i < cut && !r.Active {
			continue
		}
		out = append(out, r)
	}
	return out
}

// cleanText: one line, no control characters, capped.
func cleanText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < ' ' {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > ovMaxText {
		s = s[:ovMaxText]
	}
	return s
}

// activeLocked: the active row of a kind that has not expired at now (index, -1 when none).
func (s *aylaOvStore) activeLocked(kind string, now time.Time) int {
	for i := len(s.rows) - 1; i >= 0; i-- {
		r := s.rows[i]
		if r.Kind == kind && r.Active && now.Before(r.ExpiresAt) {
			return i
		}
	}
	return -1
}

// sweepLocked ends every active row whose time is up and logs one "expire" line for each. The in-memory rows change
// only when the file was written; an expired row is never in force anyway (activeLocked checks the time).
func (s *aylaOvStore) sweepLocked(now time.Time) {
	rows := append([]AylaOverride(nil), s.rows...)
	var ended []AylaOverride
	for i := range rows {
		if rows[i].Active && !now.Before(rows[i].ExpiresAt) {
			rows[i].Active = false
			rows[i].EndReason = "expire"
			rows[i].ClearedAt = now
			ended = append(ended, rows[i])
		}
	}
	if len(ended) == 0 {
		return
	}
	if err := s.saveLocked(rows, now); err != nil {
		log.Printf("ayla overrides: expiry not saved: %v", err)
		return
	}
	s.rows = rows
	for _, r := range ended {
		if err := s.auditLocked(aylaOvAudit{At: now, Action: "expire", ID: r.ID, Kind: r.Kind, Value: r.Value,
			By: r.SetBy, ExpiresAt: r.ExpiresAt}); err != nil {
			log.Printf("ayla overrides: audit: %v", err)
		}
	}
}

// Set stores an owner override for kind (one active per kind: the previous one ends as "replaced"). farm_map is
// checked against the gates first: warnings return *ovNeedsConfirm unless confirm is true. baseLv/maxLv (0 = not
// given) feed the level-based gates. Nothing changes when an error is returned.
func (s *aylaOvStore) Set(kind, value, note string, hours float64, baseLv, maxLv int, confirm bool, now time.Time) (AylaOverride, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if !containsStr(ovKinds, kind) {
		return AylaOverride{}, fmt.Errorf("kind must be one of %s", strings.Join(ovKinds, ", "))
	}
	value = cleanText(value)
	if kind == ovFarmMap {
		value = strings.ToLower(value)
		if !ovMapRe.MatchString(value) {
			return AylaOverride{}, fmt.Errorf("farm_map value must be a map name such as pay_fild08")
		}
		if len(maps) == 0 {
			return AylaOverride{}, fmt.Errorf("engine map table not loaded: farm_map cannot be checked")
		}
		if maps[value] == nil {
			return AylaOverride{}, fmt.Errorf("farm_map %q is not a map the engine knows", value)
		}
	}
	if value == "" {
		return AylaOverride{}, fmt.Errorf("value is required")
	}
	if hours == 0 {
		hours = ovDefaultHours
	}
	if hours < 0 || hours > ovMaxHours {
		return AylaOverride{}, fmt.Errorf("hours must be above 0 and at most %.0f", ovMaxHours)
	}
	if baseLv < 0 || maxLv < 0 {
		return AylaOverride{}, fmt.Errorf("base_lv and max_lv must not be negative")
	}
	var warnings []string
	if kind == ovFarmMap {
		warnings = aylaFarmMapWarnings(value, baseLv, maxLv)
		if len(warnings) > 0 && !confirm {
			return AylaOverride{}, &ovNeedsConfirm{Warnings: warnings}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadLocked() // another instance may have written since the last read: never drop its rows
	rows := append([]AylaOverride(nil), s.rows...)
	replaced := ""
	for i := range rows {
		if rows[i].Kind == kind && rows[i].Active {
			rows[i].Active = false
			rows[i].EndReason = "replaced"
			rows[i].ClearedAt = now
			replaced = rows[i].ID
		}
	}
	o := AylaOverride{ID: fmt.Sprintf("ov%d", now.UnixNano()), Kind: kind, Value: value, SetBy: "owner", SetAt: now,
		ExpiresAt: now.Add(time.Duration(hours * float64(time.Hour))), Note: cleanText(note), Active: true}
	rows = trimHistory(append(rows, o))
	if err := s.saveLocked(rows, now); err != nil {
		return AylaOverride{}, fmt.Errorf("override not stored: %v", err)
	}
	s.rows = rows
	// the override is stored; a failed audit line is logged, not hidden behind an error (the save already happened)
	if err := s.auditLocked(aylaOvAudit{At: now, Action: "set", ID: o.ID, Kind: kind, Value: value, By: "owner",
		Note: o.Note, ExpiresAt: o.ExpiresAt, Replaced: replaced, Confirm: confirm, Warnings: warnings}); err != nil {
		log.Printf("ayla overrides: audit line for %s not written: %v", o.ID, err)
	}
	return o, nil
}

// Clear ends an active override now (reason "clear"). A row whose time is up but not yet swept is swept (its expire
// line is written) and returned with errOvExpired, so the caller sees the expired state. An unknown or already ended
// id is errOvNotFound.
func (s *aylaOvStore) Clear(id string, now time.Time) (AylaOverride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadLocked()
	s.sweepLocked(now)
	for i := range s.rows {
		r := s.rows[i]
		if r.ID != id {
			continue
		}
		if !r.Active {
			if r.EndReason == "expire" { // swept just now (or earlier): the expired state
				return r, errOvExpired
			}
			return AylaOverride{}, errOvNotFound // cleared or replaced before
		}
		if !now.Before(r.ExpiresAt) { // sweep could not save: still expired in memory
			return r, errOvExpired
		}
		rows := append([]AylaOverride(nil), s.rows...)
		rows[i].Active = false
		rows[i].EndReason = "clear"
		rows[i].ClearedAt = now
		if err := s.saveLocked(rows, now); err != nil {
			return AylaOverride{}, fmt.Errorf("clear not saved: %v", err)
		}
		s.rows = rows
		c := rows[i]
		if err := s.auditLocked(aylaOvAudit{At: now, Action: "clear", ID: c.ID, Kind: c.Kind, Value: c.Value,
			By: "owner", ExpiresAt: c.ExpiresAt}); err != nil {
			log.Printf("ayla overrides: audit line for clear %s not written: %v", c.ID, err)
		}
		return c, nil
	}
	return AylaOverride{}, errOvNotFound
}

// Active: the owner override in force for kind at now (expired ones are ended and logged first).
func (s *aylaOvStore) Active(kind string, now time.Time) (AylaOverride, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	s.sweepLocked(now)
	if i := s.activeLocked(kind, now); i >= 0 {
		return s.rows[i], true
	}
	return AylaOverride{}, false
}

// List: every stored row (newest last) after the expiry sweep.
func (s *aylaOvStore) List(now time.Time) []AylaOverride {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	s.sweepLocked(now)
	return append([]AylaOverride(nil), s.rows...)
}

// aylaDirective: the directive in force for kind. The owner's active override wins; else the puppet master's directive
// (puppet, "" = none); else the engine default (def, "" = none). Pure apart from the store's expiry sweep.
func aylaDirective(kind string, now time.Time, puppet, def string) AylaDirective {
	if o, ok := aylaOv().Active(kind, now); ok {
		return AylaDirective{Kind: kind, Value: o.Value, Source: "owner", OverrideID: o.ID, ExpiresAt: o.ExpiresAt}
	}
	if puppet != "" {
		return AylaDirective{Kind: kind, Value: puppet, Source: "puppet"}
	}
	if def != "" {
		return AylaDirective{Kind: kind, Value: def, Source: "default"}
	}
	return AylaDirective{Kind: kind, Source: "none"}
}

// aylaFarmMapWarnings: why a farm map fails the gates for a character of base level baseLv and max hunting level maxLv
// (0 = baseLv+4, as fitShare judges it). Empty = passes. Uses the fair gate (fairgate.go) and the level-fit share
// (data.go fitShare), the same rules the solo pickers use.
func aylaFarmMapWarnings(m string, baseLv, maxLv int) []string {
	var w []string
	if len(spawnsByMap[m]) == 0 {
		return append(w, fmt.Sprintf("map %s has no monster spawn data", m))
	}
	if baseLv <= 0 {
		return append(w, "no base_lv given: the fair and fit gates were not judged")
	}
	if !fairSpotOK(m, baseLv) {
		share, _ := fairBandShare(m, baseLv)
		w = append(w, fmt.Sprintf("fair gate: %.0f%% of the monsters are fair for base level %d (need %.0f%%)",
			share*100, baseLv, fairSpotShare()*100))
	}
	if maxLv <= 0 {
		maxLv = baseLv + 4
	}
	if share := fitShare(m, maxLv); share < minFitShare {
		w = append(w, fmt.Sprintf("fit gate: %.0f%% of the monsters fit max level %d (need %.0f%%)",
			share*100, maxLv, minFitShare*100))
	}
	return w
}

// ovOrderRow: one row of legion/data/ayla_orders.jsonl (written by tools/puppet/orders.py).
type ovOrderRow struct {
	Kind   string  `json:"kind"`
	ID     string  `json:"id"`
	T      float64 `json:"t"`
	Iso    string  `json:"iso"`
	From   string  `json:"from,omitempty"`
	Text   string  `json:"text,omitempty"`
	By     string  `json:"by,omitempty"`
	Result string  `json:"result,omitempty"`
}

// ovOrders: the last owner orders and the last helper results, read-only.
type ovOrders struct {
	Owner  []ovOrderRow `json:"owner"`
	Helper []ovOrderRow `json:"helper"`
}

func aylaOrdersView(dir string) ovOrders {
	var out ovOrders
	f, err := os.Open(filepath.Join(dir, "ayla_orders.jsonl"))
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var r ovOrderRow
		if json.Unmarshal(sc.Bytes(), &r) != nil || r.ID == "" {
			continue
		}
		switch {
		case r.Kind == "order" && r.From == "owner":
			out.Owner = append(out.Owner, r)
		case r.Kind == "result":
			out.Helper = append(out.Helper, r)
		}
	}
	if len(out.Owner) > ovOrdersShow {
		out.Owner = out.Owner[len(out.Owner)-ovOrdersShow:]
	}
	if len(out.Helper) > ovOrdersShow {
		out.Helper = out.Helper[len(out.Helper)-ovOrdersShow:]
	}
	return out
}

// ovView: the GET body.
type ovView struct {
	Now        time.Time                `json:"now"`
	Precedence string                   `json:"precedence"`
	Kinds      []string                 `json:"kinds"`
	Owner      map[string]*AylaOverride `json:"owner"` // the active override per kind, null when none
	Overrides  []AylaOverride           `json:"overrides"`
	Puppet     ovPuppet                 `json:"puppet"`
}

type ovPuppet struct {
	// guild directions in force for Starveil (guildduty.go: leader-set first, autopilot fills free slots); read-only
	StarveilGuild dirView  `json:"starveil_guild"`
	Orders        ovOrders `json:"orders"`
}

func aylaOverridesView(now time.Time) ovView {
	s := aylaOv()
	owner := map[string]*AylaOverride{}
	for _, k := range ovKinds {
		if o, ok := s.Active(k, now); ok {
			c := o
			owner[k] = &c
		} else {
			owner[k] = nil
		}
	}
	return ovView{Now: now, Precedence: "owner > puppet > default", Kinds: ovKinds, Owner: owner,
		Overrides: s.List(now),
		Puppet:    ovPuppet{StarveilGuild: dirViewOf(humanGuildID, now), Orders: aylaOrdersView(*dataDir)}}
}

// registerAylaOverrides: the override endpoints (api.go). Creates the owner token on first start when it is missing.
func registerAylaOverrides(mux *http.ServeMux) {
	if _, err := loadOwnerToken(*dataDir); err != nil {
		log.Printf("ayla overrides: owner token: %v", err)
	}
	mux.HandleFunc("/api/ayla/overrides", aylaOverridesAPI)
	mux.HandleFunc("/api/ayla/overrides/", aylaOverridesAPI)
}

// ovOwnerTokenFile: <data>/owner.token, the per-host secret that POST and DELETE on the override routes require in the
// X-Owner-Token header. Git-ignored. Only the dashboard's owner tool (tools/dashboard/dashserve.py) sends it.
const ovOwnerTokenFile = "owner.token"

// loadOwnerToken returns the owner token in dir, creating it when missing or empty: 32 random bytes as hex, written
// through a temp file with mode 0600 (the OS may ignore the mode, Windows mostly does).
func loadOwnerToken(dir string) (string, error) {
	path := filepath.Join(dir, ovOwnerTokenFile)
	raw, err := os.ReadFile(path)
	if err == nil {
		if t := strings.TrimSpace(string(raw)); t != "" {
			return t, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)
	tmp, err := os.CreateTemp(dir, "owner-*.tmp")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(tok + "\n"); err != nil {
		tmp.Close()
		os.Remove(name)
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	os.Chmod(name, 0600) // best effort: not every OS honours the mode
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return "", err
	}
	return tok, nil
}

// ownerTokenOK: the request carries the owner token (constant-time compare). A missing token file is an error.
func ownerTokenOK(r *http.Request) (bool, error) {
	tok, err := loadOwnerToken(*dataDir)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Owner-Token")), []byte(tok)) == 1, nil
}

func aylaOverridesAPI(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		ok, err := ownerTokenOK(r)
		if err != nil {
			apiErr(w, http.StatusInternalServerError, "owner token unavailable: "+err.Error())
			return
		}
		if !ok {
			apiErr(w, http.StatusForbidden, "owner token required: X-Owner-Token header")
			return
		}
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, aylaOverridesView(now))
	case http.MethodPost:
		var body struct {
			Kind    string  `json:"kind"`
			Value   string  `json:"value"`
			Hours   float64 `json:"hours"`
			Note    string  `json:"note"`
			BaseLv  int     `json:"base_lv"`
			MaxLv   int     `json:"max_lv"`
			Confirm bool    `json:"confirm"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
			apiErr(w, http.StatusBadRequest, "bad json: "+err.Error())
			return
		}
		o, err := aylaOv().Set(body.Kind, body.Value, body.Note, body.Hours, body.BaseLv, body.MaxLv, body.Confirm, now)
		var nc *ovNeedsConfirm
		if errors.As(err, &nc) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error(), "warnings": nc.Warnings, "stored": false})
			return
		}
		if err != nil {
			apiErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, o)
	case http.MethodDelete:
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ayla/overrides"), "/")
		if id == "" {
			id = r.URL.Query().Get("id")
		}
		if id == "" {
			apiErr(w, http.StatusBadRequest, "override id required (DELETE /api/ayla/overrides/{id})")
			return
		}
		o, err := aylaOv().Clear(id, now)
		if errors.Is(err, errOvNotFound) {
			apiErr(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, errOvExpired) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error(), "override": o})
			return
		}
		if err != nil {
			apiErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, o)
	default:
		apiErr(w, http.StatusMethodNotAllowed, "GET, POST or DELETE")
	}
}
