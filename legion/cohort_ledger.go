package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

var ErrFailClosed = errors.New("ledger validation failed; admission denied")

// CohortLedger v2: Separate permanent eligibility from current quota window.
// 
// Design:
// - PermanentEligible: accounts admitted in ANY cohort (never erased)
// - CurrentQuotaWindow: grants in current UTC hour (reset at boundary)
// 
// Serialization:
// - Single versioned JSON file with both sections
// - Mutex + file lock (flock) for cross-process serialization
// - Atomic writes: temp → rename
type CohortLedger struct {
	filePath string
	lockPath string

	mu sync.Mutex

	// Permanent eligibility: account IDs admitted in any cohort (never erased).
	PermanentEligible map[string]bool `json:"-"`

	// Current quota window state.
	CurrentWindowStart   time.Time      `json:"-"`
	GrantedThisWindow    map[string]bool `json:"-"`

	// Enabled and quota settings.
	Enabled      bool
	QuotaPerHour int
}

// Persistent on-disk schema (versioned).
type ledgerSchema struct {
	SchemaVersion   int      `json:"schema_version"`
	PermanentEligible []string `json:"permanent_eligible"`
	PermanentModified string   `json:"permanent_modified_utc"`
	QuotaWindowStart  string   `json:"quota_window_start_utc"`
	QuotaGranted      []string `json:"quota_granted_this_window"`
	QuotaModified     string   `json:"quota_modified_utc"`
}

const ledgerSchemaVersion = 1

func NewCohortLedger(dataDir string, enabled bool, quotaPerHour int) *CohortLedger {
	filePath := filepath.Join(dataDir, "cohort_ledger.json")
	return &CohortLedger{
		filePath:          filePath,
		lockPath:          filePath + ".lock",
		Enabled:           enabled,
		QuotaPerHour:      quotaPerHour,
		PermanentEligible: make(map[string]bool),
		GrantedThisWindow: make(map[string]bool),
	}
}

// Load reads and validates the ledger from disk.
// Returns error if file is corrupt, schema mismatch, or other validation fails (fail-closed).
func (cl *CohortLedger) Load() error {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	data, err := os.ReadFile(cl.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// First run: no ledger yet. Initialize empty state.
			cl.PermanentEligible = make(map[string]bool)
			cl.GrantedThisWindow = make(map[string]bool)
			cl.CurrentWindowStart = time.Now().UTC()
			return nil
		}
		return err // read error is fatal
	}

	// Parse JSON and validate schema.
	var schema ledgerSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return ErrFailClosed // malformed JSON: fail-closed
	}

	if schema.SchemaVersion != ledgerSchemaVersion {
		return ErrFailClosed // schema mismatch: fail-closed
	}

	// Validate permanent eligibility array.
	if schema.PermanentEligible == nil {
		return ErrFailClosed // missing required field
	}
	perm := make(map[string]bool)
	for _, aid := range schema.PermanentEligible {
		if aid == "" {
			return ErrFailClosed // empty account ID
		}
		perm[aid] = true
	}

	// Validate quota window start.
	windowStart, err := time.Parse(time.RFC3339, schema.QuotaWindowStart)
	if err != nil {
		return ErrFailClosed // invalid timestamp
	}

	// Validate quota granted array.
	if schema.QuotaGranted == nil {
		return ErrFailClosed // missing required field
	}
	granted := make(map[string]bool)
	for _, aid := range schema.QuotaGranted {
		if aid == "" {
			return ErrFailClosed // empty account ID
		}
		granted[aid] = true
	}
	if len(granted) > cl.QuotaPerHour {
		return ErrFailClosed // quota exceeded
	}

	cl.PermanentEligible = perm
	cl.CurrentWindowStart = windowStart
	cl.GrantedThisWindow = granted

	return nil
}

// MayAdmit checks if an account can be admitted.
// Returns (true, nil) if admitted; (false, error) if denied or error.
func (cl *CohortLedger) MayAdmit(accountID string) (bool, error) {
	if !cl.Enabled {
		return true, nil // gate disabled
	}

	if accountID == "" {
		return false, errors.New("empty account ID")
	}

	// Acquire file lock (single writer across processes).
	lockFile, err := os.OpenFile(cl.lockPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return false, err
	}
	defer lockFile.Close()

	// Blocking exclusive lock.
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return false, err
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	// Critical section: load, validate, potentially roll window, check quota, write.
	cl.mu.Lock()
	defer cl.mu.Unlock()

	// Reload from disk (another instance may have changed state).
	if err := cl.loadLocked(); err != nil {
		return false, err // fail-closed
	}

	now := time.Now().UTC()

	// Roll window if >= 1 hour has passed.
	// Crucially: preserve PermanentEligible, only clear GrantedThisWindow.
	if now.Sub(cl.CurrentWindowStart) >= time.Hour {
		cl.CurrentWindowStart = now
		cl.GrantedThisWindow = make(map[string]bool)
	}

	// Check if already in permanent eligibility → admit (reconnect).
	if cl.PermanentEligible[accountID] {
		return true, nil
	}

	// Check if already granted this window → admit (reconnect).
	if cl.GrantedThisWindow[accountID] {
		return true, nil
	}

	// Check if quota available → grant new.
	if len(cl.GrantedThisWindow) < cl.QuotaPerHour {
		// New grant: add to both permanent and this window.
		cl.PermanentEligible[accountID] = true
		cl.GrantedThisWindow[accountID] = true

		// Persist atomically.
		if err := cl.persistLocked(); err != nil {
			return false, err // persist failed: fail-closed, don't admit
		}

		return true, nil
	}

	// Quota exhausted.
	return false, nil
}

// loadLocked reads from disk without holding the outer mutex (assumed already held).
// Called from MayAdmit after lock acquired.
func (cl *CohortLedger) loadLocked() error {
	data, err := os.ReadFile(cl.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// File was deleted; reinitialize.
			cl.PermanentEligible = make(map[string]bool)
			cl.GrantedThisWindow = make(map[string]bool)
			cl.CurrentWindowStart = time.Now().UTC()
			return nil
		}
		return err
	}

	var schema ledgerSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return ErrFailClosed
	}

	if schema.SchemaVersion != ledgerSchemaVersion {
		return ErrFailClosed
	}

	if schema.PermanentEligible == nil || schema.QuotaGranted == nil {
		return ErrFailClosed
	}

	perm := make(map[string]bool)
	for _, aid := range schema.PermanentEligible {
		if aid == "" {
			return ErrFailClosed
		}
		perm[aid] = true
	}

	windowStart, err := time.Parse(time.RFC3339, schema.QuotaWindowStart)
	if err != nil {
		return ErrFailClosed
	}

	granted := make(map[string]bool)
	for _, aid := range schema.QuotaGranted {
		if aid == "" {
			return ErrFailClosed
		}
		granted[aid] = true
	}

	if len(granted) > cl.QuotaPerHour {
		return ErrFailClosed
	}

	cl.PermanentEligible = perm
	cl.CurrentWindowStart = windowStart
	cl.GrantedThisWindow = granted

	return nil
}

// persistLocked writes the current state to disk atomically (temp → rename).
func (cl *CohortLedger) persistLocked() error {
	// Convert maps to sorted arrays for deterministic JSON.
	var permArray []string
	for aid := range cl.PermanentEligible {
		permArray = append(permArray, aid)
	}

	var grantedArray []string
	for aid := range cl.GrantedThisWindow {
		grantedArray = append(grantedArray, aid)
	}

	schema := ledgerSchema{
		SchemaVersion:     ledgerSchemaVersion,
		PermanentEligible: permArray,
		PermanentModified: time.Now().UTC().Format(time.RFC3339),
		QuotaWindowStart:  cl.CurrentWindowStart.Format(time.RFC3339),
		QuotaGranted:      grantedArray,
		QuotaModified:     time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(schema)
	if err != nil {
		return err
	}

	// Write to temp file.
	tmpPath := cl.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}

	// Atomic rename.
	if err := os.Rename(tmpPath, cl.filePath); err != nil {
		os.Remove(tmpPath) // cleanup temp on error
		return err
	}

	return nil
}

// Stats returns observable statistics.
func (cl *CohortLedger) Stats() (grantedThisWindow int, nextEligibleTime time.Time) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	grantedThisWindow = len(cl.GrantedThisWindow)
	nextEligibleTime = cl.CurrentWindowStart.Add(time.Hour)
	return
}
