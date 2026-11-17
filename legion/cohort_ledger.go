package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// CohortLedger manages the durable 100-new-bots-per-hour admission gate.
// It persists permanent eligibility (never erased) and rolling quota window state.
// A single process acquires a nonblocking advisory lock at startup and holds it
// for the lifetime of the engine, ensuring single-writer consistency.
type CohortLedger struct {
	dataDir             string
	enabled             bool
	maxPerHour          int
	ledgerPath          string
	lockPath            string
	lock                *os.File // held for process lifetime; advisory flock
	mu                  sync.Mutex
	state               *cohortState
	loadAttempted       bool
	failClosed          bool // if true, all admissions denied (corrupt state or write failure)
	ledgerExistedOnLoad bool // true if ledger file existed when we loaded (used to detect deletion/corruption)
	now                 func() time.Time // optional: for testing. if nil, uses time.Now().UTC()
}

// cohortState is the persistent JSON structure on disk.
// NEVER includes any field not explicitly versioned.
type cohortState struct {
	SchemaVersion       int      `json:"schema_version"`
	PermanentEligible   []string `json:"permanent_eligible"`
	QuotaWindowStartUTC string   `json:"quota_window_start_utc"`
	QuotaGranted        []string `json:"quota_granted"`
}

// NewCohortLedger creates a new ledger manager (does not load from disk yet).
func NewCohortLedger(dataDir string, enabled bool, maxPerHour int) *CohortLedger {
	cl := &CohortLedger{
		dataDir:             dataDir,
		enabled:             enabled,
		maxPerHour:          maxPerHour,
		ledgerPath:          filepath.Join(dataDir, "cohort_ledger.json"),
		lockPath:            filepath.Join(dataDir, "cohort_ledger.lock"),
		state: &cohortState{
			SchemaVersion:      2,
			PermanentEligible:  []string{},
			QuotaWindowStartUTC: "",
			QuotaGranted:        []string{},
		},
		failClosed:          false,
		ledgerExistedOnLoad: false,
		now:                 nil, // will use time.Now().UTC() by default
	}
	return cl
}

// Load initializes the ledger at engine startup.
// It acquires a nonblocking advisory lock and loads persistent state.
// If disabled, lock is not acquired and all admissions pass.
// If lock is held by another process, fails closed (denies all).
// Any validation error or write failure latches fail-closed for process lifetime.
func (cl *CohortLedger) Load() error {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	if cl.loadAttempted {
		return nil // already loaded
	}
	cl.loadAttempted = true

	if !cl.enabled {
		return nil // feature disabled, no lock needed
	}

	// Acquire nonblocking advisory lock. If another process holds it, fail closed.
	lockFile, err := os.OpenFile(cl.lockPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		cl.failClosed = true
		return fmt.Errorf("cohort: cannot open lock file: %w", err)
	}
	cl.lock = lockFile

	// Try nonblocking flock. If locked by another process, fail closed.
	err = syscall.FcntlFlock(uintptr(lockFile.Fd()), syscall.F_SETLK, &syscall.Flock_t{
		Type:   syscall.F_WRLCK,
		Whence: 0,
		Start:  0,
		Len:    0,
	})
	if err != nil {
		cl.failClosed = true
		lockFile.Close()
		cl.lock = nil
		return fmt.Errorf("cohort: another process holds the lock (nonblocking flock): %w", err)
	}

	// Lock acquired. Now load and validate persistent state.
	err = cl.loadAndValidate()
	if err != nil {
		// Validation failed. Set fail-closed state but don't error Load() itself,
		// so the engine can start. MayAdmit() will deny all requests while failClosed is true.
		cl.failClosed = true
		// Don't return error; failClosed will cause MayAdmit() to deny everything
	}

	return nil
}

// loadAndValidate reads and validates the persisted JSON.
// On any error (corrupt JSON, schema mismatch, missing fields, invalid timestamps),
// it fails closed and returns error.
// If file doesn't exist on FIRST load, initializes fresh state (first startup).
// If file doesn't exist on SUBSEQUENT loads, fails closed (ledger was deleted/corrupted).
func (cl *CohortLedger) loadAndValidate() error {
	data, err := os.ReadFile(cl.ledgerPath)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		// File missing
		initMarkerPath := filepath.Join(filepath.Dir(cl.ledgerPath), ".cohort_initialized")
		_, markerErr := os.Stat(initMarkerPath)
		
		if !cl.loadAttempted {
			// First load: file never existed
			if markerErr == nil {
				// Marker exists but ledger doesn't: corruption/deletion
				return fmt.Errorf("ledger file missing but initialization marker exists (ledger was deleted or corrupted)")
			}
			// Marker also doesn't exist: true first load, initialize fresh
			cl.state = &cohortState{
				SchemaVersion:      2,
				PermanentEligible:  []string{},
				QuotaWindowStartUTC: "", // Will be set on first grant
				QuotaGranted:        []string{},
			}
			cl.ledgerExistedOnLoad = false
			return nil
		} else if cl.ledgerExistedOnLoad {
			// Subsequent load: ledger existed before but now missing
			return fmt.Errorf("ledger file missing (was present at startup, now deleted or corrupted)")
		} else if markerErr == nil {
			// Ledger never existed but marker exists: suspicious corruption
			return fmt.Errorf("ledger file missing but initialization marker exists (corruption)")
		} else {
			// Ledger never existed and marker also doesn't exist: OK
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("read ledger: %w", err)
	}

	// File exists; mark that we've seen it
	cl.ledgerExistedOnLoad = true

	// Parse JSON
	var loaded cohortState
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("corrupt JSON: %w", err)
	}

	// Reject empty object or partial state
	if data != nil && len(data) > 0 {
		if string(data) == "{}" {
			return fmt.Errorf("empty JSON object")
		}
	}

	// Validate schema version
	if loaded.SchemaVersion != 2 {
		return fmt.Errorf("unknown schema version: %d", loaded.SchemaVersion)
	}

	// Reject missing/empty required fields
	if loaded.PermanentEligible == nil {
		return fmt.Errorf("missing permanent_eligible array")
	}
	if loaded.QuotaGranted == nil {
		return fmt.Errorf("missing quota_granted array")
	}

	// QuotaWindowStartUTC can be empty only if QuotaGranted is empty (no grants yet)
	if loaded.QuotaWindowStartUTC == "" && len(loaded.QuotaGranted) > 0 {
		return fmt.Errorf("missing quota_window_start_utc with non-empty quota_granted")
	}

	// Validate timestamp format if present
	if loaded.QuotaWindowStartUTC != "" {
		_, err = time.Parse(time.RFC3339, loaded.QuotaWindowStartUTC)
		if err != nil {
			return fmt.Errorf("invalid quota_window_start_utc: %w", err)
		}
	}

	// Reject duplicates in permanent_eligible
	seen := make(map[string]bool)
	for _, id := range loaded.PermanentEligible {
		if seen[id] {
			return fmt.Errorf("duplicate in permanent_eligible: %s", id)
		}
		seen[id] = true
	}

	// Reject duplicates in quota_granted
	seen = make(map[string]bool)
	for _, id := range loaded.QuotaGranted {
		if seen[id] {
			return fmt.Errorf("duplicate in quota_granted: %s", id)
		}
		seen[id] = true
	}

	// Reject quota > maxPerHour
	if len(loaded.QuotaGranted) > cl.maxPerHour {
		return fmt.Errorf("quota_granted exceeds max: %d > %d", len(loaded.QuotaGranted), cl.maxPerHour)
	}

	cl.state = &loaded

	return nil
}

// MayAdmit checks if an account can be admitted.
// Returns (true, nil) if admitted.
// Returns (false, error) if denied (quota exhausted, corrupt state, etc.).
// Reconnects (account already in permanent or quota_granted) are admitted without consuming quota.
// New grants trigger window rollover if >=1h since first grant in this window.
func (cl *CohortLedger) MayAdmit(accountID string) (bool, error) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	if !cl.enabled {
		return true, nil // feature disabled
	}

	if !cl.loadAttempted {
		return false, fmt.Errorf("cohort: not loaded")
	}

	if cl.failClosed {
		return false, fmt.Errorf("cohort: fail-closed state (prior validation/write error)")
	}

	if cl.state == nil {
		return false, fmt.Errorf("cohort: state is nil")
	}

	// Reload from disk to get latest state (another process may have written)
	if err := cl.loadAndValidate(); err != nil {
		cl.failClosed = true
		return false, fmt.Errorf("cohort: reload validation failed: %w", err)
	}

	// Check if already in permanent eligible
	for _, id := range cl.state.PermanentEligible {
		if id == accountID {
			return true, nil // permanent, reconnect without quota consumption
		}
	}

	// Check if already in quota granted (this window)
	for _, id := range cl.state.QuotaGranted {
		if id == accountID {
			return true, nil // already admitted this window, reconnect without quota consumption
		}
	}

	// Not admitted yet. Check if we can grant a new quota slot.

	// Use injected clock for testing, or time.Now().UTC() for production
	var now time.Time
	if cl.now != nil {
		now = cl.now()
	} else {
		now = time.Now().UTC()
	}

	// If quota window is empty (no grants yet), this grant starts a new window.
	// Update window start to NOW so the next 1 hour is measured from this grant time.
	if len(cl.state.QuotaGranted) == 0 {
		cl.state.QuotaWindowStartUTC = now.Format(time.RFC3339)
	}

	// If >=1h since first grant in current window, roll: clear quota, start new window.
	if cl.state.QuotaWindowStartUTC != "" {
		windowStart, err := time.Parse(time.RFC3339, cl.state.QuotaWindowStartUTC)
		if err != nil {
			cl.failClosed = true
			return false, fmt.Errorf("cohort: cannot parse window start: %w", err)
		}

		if now.Sub(windowStart) >= 1*time.Hour {
			// Time to roll: clear quota_granted, preserve permanent_eligible, reset window start to NOW
			cl.state.QuotaWindowStartUTC = now.Format(time.RFC3339)
			cl.state.QuotaGranted = []string{}
		}
	}

	// Check quota
	if len(cl.state.QuotaGranted) >= cl.maxPerHour {
		return false, fmt.Errorf("cohort: quota exhausted (%d/%d)", len(cl.state.QuotaGranted), cl.maxPerHour)
	}

	// Grant new quota: add to both permanent_eligible and quota_granted
	cl.state.PermanentEligible = append(cl.state.PermanentEligible, accountID)
	cl.state.QuotaGranted = append(cl.state.QuotaGranted, accountID)

	// Persist atomically
	if err := cl.persistLocked(); err != nil {
		cl.failClosed = true
		return false, fmt.Errorf("cohort: persist failed: %w", err)
	}

	return true, nil
}

// persistLocked writes state atomically to disk: temp file → fsync → atomic rename → dirSync.
// Must be called with cl.mu held.
// On any error, state is NOT changed on disk (prior valid state preserved).
// Any write failure latches fail-closed.
func (cl *CohortLedger) persistLocked() error {
	if !cl.enabled {
		return nil
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(cl.state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Write to temp file in same directory (guaranteed atomic rename on same filesystem)
	dir := filepath.Dir(cl.ledgerPath)
	tmpFile, err := os.CreateTemp(dir, ".cohort_ledger_tmp_")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // best effort cleanup if rename succeeds

	// Write all data
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp: %w", err)
	}

	// Fsync to ensure data is written to disk
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return fmt.Errorf("fsync temp: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, cl.ledgerPath); err != nil {
		return fmt.Errorf("atomic rename: %w", err)
	}

	// Fsync directory to ensure rename is durable
	dirFile, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open dir for sync: %w", err)
	}
	defer dirFile.Close()

	if err := dirFile.Sync(); err != nil {
		return fmt.Errorf("dirSync: %w", err)
	}

	// Create marker file indicating this cohort ledger has been initialized.
	// This allows detection of deleted ledger files (corruption/disaster).
	initMarkerPath := filepath.Join(dir, ".cohort_initialized")
	if err := os.WriteFile(initMarkerPath, []byte("true"), 0600); err != nil {
		return fmt.Errorf("write init marker: %w", err)
	}

	return nil
}

// Stats returns observable statistics for monitoring.
// Returns: (number of accounts granted this window, next window start time).
func (cl *CohortLedger) Stats() (grantedThisWindow int, nextEligible time.Time) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	if !cl.enabled || cl.state == nil {
		return 0, time.Time{}
	}

	grantedThisWindow = len(cl.state.QuotaGranted)

	windowStart, err := time.Parse(time.RFC3339, cl.state.QuotaWindowStartUTC)
	if err != nil {
		return grantedThisWindow, time.Time{}
	}

	nextEligible = windowStart.Add(1 * time.Hour)
	return grantedThisWindow, nextEligible
}

// Close releases the advisory lock (should be called at shutdown).
// Not strictly necessary (OS releases on process exit), but good practice.
func (cl *CohortLedger) Close() error {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	if cl.lock != nil {
		// Explicitly release lock (advisory, so this is best-effort)
		syscall.FcntlFlock(uintptr(cl.lock.Fd()), syscall.F_SETLK, &syscall.Flock_t{
			Type:   syscall.F_UNLCK,
			Whence: 0,
			Start:  0,
			Len:    0,
		})
		cl.lock.Close()
		cl.lock = nil
	}

	return nil
}
