package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrCompetingWriter = errors.New("competing writer detected: disk state differs from memory")

// CohortLedger tracks which account IDs have been admitted in the current hourly window.
// It is persisted to disk atomically and survives process restarts.
type CohortLedger struct {
	// File path where the ledger is persisted
	filePath string

	// Mutex protects in-memory state
	mu sync.Mutex

	// CurrentWindowStart is when the current hourly window began (UTC).
	CurrentWindowStart time.Time `json:"current_window_start"`

	// AdmittedAccountIDs is the set of distinct account IDs granted admission in this window.
	// Stored as slice in JSON for deterministic ordering.
	AdmittedAccountIDs []string `json:"admitted_account_ids"`

	// RotationStart is a timestamp for observability (when was the window opened).
	RotationStart time.Time `json:"rotation_start"`

	// Enabled determines if the cohort gate is active.
	Enabled bool `json:"-"`

	// QuotaPerHour is the max number of distinct new admissions per window (default 100).
	QuotaPerHour int `json:"-"`
}

// NewCohortLedger creates a new ledger instance. It does not load from disk yet.
func NewCohortLedger(dataDir string, enabled bool, quotaPerHour int) *CohortLedger {
	return &CohortLedger{
		filePath:    filepath.Join(dataDir, "cohort_ledger.json"),
		Enabled:     enabled,
		QuotaPerHour: quotaPerHour,
	}
}

// Load reads the ledger from disk and handles window rollover.
// If the current window has expired (>= 1h old), resets to a new empty window and persists it.
// Returns error only if file exists but is corrupt; missing file starts a fresh window.
func (cl *CohortLedger) Load() error {
	if !cl.Enabled {
		return nil
	}

	cl.mu.Lock()
	defer cl.mu.Unlock()

	now := time.Now().UTC()

	// If file doesn't exist, start a fresh window.
	if _, err := os.Stat(cl.filePath); os.IsNotExist(err) {
		cl.CurrentWindowStart = now
		cl.AdmittedAccountIDs = []string{}
		cl.RotationStart = now
		return nil
	}

	// Try to read existing ledger.
	data, err := os.ReadFile(cl.filePath)
	if err != nil {
		return err
	}

	var stored CohortLedger
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}

	cl.CurrentWindowStart = stored.CurrentWindowStart
	cl.AdmittedAccountIDs = stored.AdmittedAccountIDs
	cl.RotationStart = stored.RotationStart

	// Check if window has expired (>= 1 hour).
	if now.Sub(cl.CurrentWindowStart) >= time.Hour {
		cl.CurrentWindowStart = now
		cl.AdmittedAccountIDs = []string{}
		cl.RotationStart = now
		// CRITICAL: Persist the rolled window immediately so subsequent calls see the new state.
		if err := cl.writeLocked(); err != nil {
			return err
		}
	}

	return nil
}

// MayAdmit checks if an account ID is eligible to log in this window.
// - Rolls window if >= 1 hour has passed since CurrentWindowStart
// - If already admitted in this window, returns (true, false, nil) [no quota consumed]
// - If new and quota available, adds it, persists, returns (true, true, nil) [quota consumed]
// - If new and quota exhausted, returns (false, false, nil) [blocked]
// - If write fails or competing writer detected, returns (false, false, error) [fail closed]
func (cl *CohortLedger) MayAdmit(accountID string) (admitted, newGrant bool, err error) {
	if !cl.Enabled {
		return true, false, nil // gate disabled: allow all
	}

	cl.mu.Lock()
	defer cl.mu.Unlock()

	now := time.Now().UTC()

	// CRITICAL: Roll window if >= 1 hour has passed.
	// This must happen EVERY time MayAdmit is called, not just on Load().
	if now.Sub(cl.CurrentWindowStart) >= time.Hour {
		cl.CurrentWindowStart = now
		cl.AdmittedAccountIDs = []string{}
		cl.RotationStart = now
		// Persist the rolled window before proceeding.
		if err := cl.writeLocked(); err != nil {
			return false, false, err
		}
	}

	// Check if already admitted.
	for _, id := range cl.AdmittedAccountIDs {
		if id == accountID {
			return true, false, nil // reconnect, no quota consumed
		}
	}

	// Check if quota available.
	if len(cl.AdmittedAccountIDs) >= cl.QuotaPerHour {
		return false, false, nil // blocked, no error
	}

	// Before adding, detect competing writer: re-read file and validate it matches memory.
	// If file differs, another instance wrote while we held the lock (not safe).
	if err := cl.detectCompetingWriter(); err != nil {
		return false, false, err
	}

	// Add to ledger and persist.
	cl.AdmittedAccountIDs = append(cl.AdmittedAccountIDs, accountID)
	if err := cl.writeLocked(); err != nil {
		// Fail closed: revert the admission.
		cl.AdmittedAccountIDs = cl.AdmittedAccountIDs[:len(cl.AdmittedAccountIDs)-1]
		return false, false, err
	}

	return true, true, nil
}

// writeLocked persists the ledger to disk atomically. Caller must hold mu.
// Uses atomic write (temp file + rename) to avoid corruption.
func (cl *CohortLedger) writeLocked() error {
	data, err := json.Marshal(map[string]interface{}{
		"current_window_start": cl.CurrentWindowStart,
		"admitted_account_ids": cl.AdmittedAccountIDs,
		"rotation_start":       cl.RotationStart,
	})
	if err != nil {
		return err
	}

	// Atomic write: temp file + rename.
	tmp := cl.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}

	if err := os.Rename(tmp, cl.filePath); err != nil {
		os.Remove(tmp)
		return err
	}

	return nil
}

// detectCompetingWriter checks if another instance wrote to disk since we loaded.
// Caller must hold mu. Returns error if file doesn't match our in-memory state.
func (cl *CohortLedger) detectCompetingWriter() error {
	// If file doesn't exist, no competing write (we haven't persisted yet).
	if _, err := os.Stat(cl.filePath); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(cl.filePath)
	if err != nil {
		return err // read error is fatal
	}

	var stored CohortLedger
	if err := json.Unmarshal(data, &stored); err != nil {
		return err // corrupt file is fatal
	}

	// Check if window start matches. If not, another instance rolled it.
	if !stored.CurrentWindowStart.Equal(cl.CurrentWindowStart) {
		return ErrCompetingWriter
	}

	// Check if admitted count matches. If not, another instance granted an account.
	if len(stored.AdmittedAccountIDs) != len(cl.AdmittedAccountIDs) {
		return ErrCompetingWriter
	}

	// Check if the granted IDs match in order. If not, another instance changed them.
	for i, id := range cl.AdmittedAccountIDs {
		if stored.AdmittedAccountIDs[i] != id {
			return ErrCompetingWriter
		}
	}

	return nil
}

// Stats returns observable counts: granted (admitted in ledger), next eligible time.
func (cl *CohortLedger) Stats() (granted int, nextEligible time.Time) {
	if !cl.Enabled {
		return 0, time.Time{}
	}

	cl.mu.Lock()
	defer cl.mu.Unlock()

	granted = len(cl.AdmittedAccountIDs)
	nextEligible = cl.CurrentWindowStart.Add(time.Hour)
	return
}

// IsNewWindow returns true if a new hourly window has begun (for observability).
func (cl *CohortLedger) IsNewWindow() bool {
	if !cl.Enabled {
		return false
	}

	cl.mu.Lock()
	defer cl.mu.Unlock()

	now := time.Now().UTC()
	return now.Sub(cl.CurrentWindowStart) >= time.Hour
}
