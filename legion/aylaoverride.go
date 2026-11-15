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

var errOvNotFound = errors.New("no active override with that id")

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
	mu   sync.Mutex
	dir  string
	rows []AylaOverride
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
	return s, nil
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
	if err := s.auditLocked(aylaOvAudit{At: now, Action: "set", ID: o.ID, Kind: kind, Value: value, By: "owner",
		Note: o.Note, ExpiresAt: o.ExpiresAt, Replaced: replaced, Confirm: confirm, Warnings: warnings}); err != nil {
		return o, fmt.Errorf("override stored but the audit line failed: %v", err)
	}
	return o, nil
}

// Clear ends an active override now (reason "clear").
func (s *aylaOvStore) Clear(id string, now time.Time) (AylaOverride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := append([]AylaOverride(nil), s.rows...)
	for i := range rows {
		if rows[i].ID == id && rows[i].Active && now.Before(rows[i].ExpiresAt) {
			rows[i].Active = false
			rows[i].EndReason = "clear"
			rows[i].ClearedAt = now
			if err := s.saveLocked(rows, now); err != nil {
				return AylaOverride{}, fmt.Errorf("clear not saved: %v", err)
			}
			s.rows = rows
			r := rows[i]
			if err := s.auditLocked(aylaOvAudit{At: now, Action: "clear", ID: r.ID, Kind: r.Kind, Value: r.Value,
				By: "owner", ExpiresAt: r.ExpiresAt}); err != nil {
				return r, fmt.Errorf("cleared but the audit line failed: %v", err)
			}
			return r, nil
		}
	}
	return AylaOverride{}, errOvNotFound
}

// Active: the owner override in force for kind at now (expired ones are ended and logged first).
func (s *aylaOvStore) Active(kind string, now time.Time) (AylaOverride, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
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

// registerAylaOverrides: the override endpoints (api.go).
func registerAylaOverrides(mux *http.ServeMux) {
	mux.HandleFunc("/api/ayla/overrides", aylaOverridesAPI)
	mux.HandleFunc("/api/ayla/overrides/", aylaOverridesAPI)
}

func aylaOverridesAPI(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
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
		if err != nil {
			apiErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, o)
	default:
		apiErr(w, http.StatusMethodNotAllowed, "GET, POST or DELETE")
	}
}
