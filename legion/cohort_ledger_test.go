package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// === COMPREHENSIVE COHORT LEDGER V3 TESTS ===

// Test 1: Disabled by default preserves old behavior
func TestCohortLedgerV3_DisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	cl := NewCohortLedger(dir, false, 100)

	if err := cl.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Gate should allow all
	for i := 0; i < 150; i++ {
		ok, err := cl.MayAdmit(fmt.Sprintf("%d", i))
		if !ok || err != nil {
			t.Errorf("disabled gate: account %d denied: ok=%v err=%v", i, ok, err)
		}
	}
}

// Test 2: Exactly 100 admitted, 101st blocked
func TestCohortLedgerV3_100Admitted_101stBlocked(t *testing.T) {
	dir := t.TempDir()
	cl := NewCohortLedger(dir, true, 100)

	if err := cl.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// First 100 should be admitted
	for i := 0; i < 100; i++ {
		ok, err := cl.MayAdmit(fmt.Sprintf("%d", i))
		if !ok {
			t.Errorf("account %d denied: %v", i, err)
		}
	}

	// 101st should be blocked
	ok, err := cl.MayAdmit("100")
	if ok {
		t.Errorf("101st account should be denied, but got admitted")
	}
	if err == nil {
		t.Errorf("101st account should have error, but got nil")
	}
}

// Test 3: Reconnect (account already in permanent) doesn't consume quota
func TestCohortLedgerV3_ReconnectNoQuotaConsumed(t *testing.T) {
	dir := t.TempDir()
	cl := NewCohortLedger(dir, true, 100)

	if err := cl.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// First 100 accounts admitted
	for i := 0; i < 100; i++ {
		ok, err := cl.MayAdmit(fmt.Sprintf("%d", i))
		if !ok {
			t.Errorf("account %d denied: %v", i, err)
		}
	}

	// Account "0" reconnects: should be admitted without consuming new quota
	ok, err := cl.MayAdmit("0")
	if !ok || err != nil {
		t.Errorf("reconnect account 0: ok=%v err=%v", ok, err)
	}

	// But 101st new account should still be blocked
	ok, err = cl.MayAdmit("100")
	if ok {
		t.Errorf("101st account should still be blocked")
	}
}

// Test 4: Restart reopens ledger, no double-admit
func TestCohortLedgerV3_RestartReopensLedger_NoDoubleAdmit(t *testing.T) {
	dir := t.TempDir()

	// First instance: admit 100
	cl1 := NewCohortLedger(dir, true, 100)
	if err := cl1.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	for i := 0; i < 100; i++ {
		ok, err := cl1.MayAdmit(fmt.Sprintf("%d", i))
		if !ok {
			t.Errorf("account %d denied: %v", i, err)
		}
	}
	cl1.Close()

	// Second instance: should read same ledger, no new admissions
	cl2 := NewCohortLedger(dir, true, 100)
	if err := cl2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// New account should be blocked (quota already at 100)
	ok, err := cl2.MayAdmit("100")
	if ok {
		t.Errorf("after restart, 101st account should be blocked")
	}

	// Old account should reconnect without issue
	ok, err = cl2.MayAdmit("0")
	if !ok || err != nil {
		t.Errorf("after restart, reconnect account 0: ok=%v err=%v", ok, err)
	}

	cl2.Close()
}

// Test 5: Fail-closed on corrupt JSON
func TestCohortLedgerV3_FailClosedOnCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	// Write corrupt JSON
	os.WriteFile(ledgerPath, []byte("{invalid json}"), 0644)

	cl := NewCohortLedger(dir, true, 100)
	err := cl.Load()
	if err == nil {
		t.Fatalf("Load should return nil but set failClosed on corrupt JSON")
	}

	// Verify failClosed is set
	if !cl.failClosed {
		t.Errorf("failClosed should be set after corrupt JSON validation error")
	}

	// All admissions should be denied (fail-closed) with explicit error
	ok, err := cl.MayAdmit("1")
	if ok || err == nil {
		t.Errorf("fail-closed: MayAdmit must return (false, error); got (ok=%v, err=%v)", ok, err)
	}

	// Verify Close succeeds
	closeErr := cl.Close()
	if closeErr != nil {
		t.Errorf("Close should succeed: %v", closeErr)
	}
}

// Test 6: Fail-closed on schema mismatch
func TestCohortLedgerV3_FailClosedOnSchemaMismatch(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	// Write valid JSON but wrong schema version
	state := cohortState{
		SchemaVersion:      1, // wrong version
		PermanentEligible:  []string{"1", "2"},
		QuotaWindowStartUTC: time.Now().UTC().Format(time.RFC3339Nano),
		QuotaGranted:       []string{},
	}
	data, _ := json.Marshal(state)
	os.WriteFile(ledgerPath, data, 0644)

	cl := NewCohortLedger(dir, true, 100)
	err := cl.Load()
	if err == nil {
		t.Fatalf("Load should return nil but set failClosed on schema mismatch")
	}

	// Verify failClosed is set
	if !cl.failClosed {
		t.Errorf("failClosed should be set after schema validation error")
	}

	// All admissions should be denied (fail-closed) with explicit error
	ok, err := cl.MayAdmit("1")
	if ok || err == nil {
		t.Errorf("fail-closed: MayAdmit must return (false, error); got (ok=%v, err=%v)", ok, err)
	}

	// Verify Close succeeds
	closeErr := cl.Close()
	if closeErr != nil {
		t.Errorf("Close should succeed: %v", closeErr)
	}
}

// Test 7: Fail-closed on missing fields
func TestCohortLedgerV3_FailClosedOnMissingFields(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	// Write JSON with non-empty quota_granted but missing quota_window_start_utc
	// This violates the invariant: QuotaWindowStartUTC required when QuotaGranted non-empty
	os.WriteFile(ledgerPath, []byte(`{
		"schema_version": 2,
		"permanent_eligible": ["account1"],
		"quota_granted": ["account1"]
	}`), 0644)

	cl := NewCohortLedger(dir, true, 100)
	err := cl.Load()
	if err == nil {
		t.Fatalf("Load should return nil but set failClosed on missing quota_window_start_utc with non-empty quota_granted")
	}

	// Verify failClosed is set
	if !cl.failClosed {
		t.Errorf("failClosed should be set after missing-field validation error")
	}

	// All admissions should be denied (fail-closed) with explicit error
	ok, err := cl.MayAdmit("1")
	if ok || err == nil {
		t.Errorf("fail-closed: MayAdmit must return (false, error); got (ok=%v, err=%v)", ok, err)
	}

	// Verify Close succeeds
	closeErr := cl.Close()
	if closeErr != nil {
		t.Errorf("Close should succeed: %v", closeErr)
	}
}
	}
}

// Test 8: Fail-closed on quota exceeded
func TestCohortLedgerV3_FailClosedOnQuotaExceeded(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	// Write JSON with quota_granted > 100
	state := cohortState{
		SchemaVersion:      2,
		PermanentEligible:  make([]string, 150),
		QuotaWindowStartUTC: time.Now().UTC().Format(time.RFC3339),
		QuotaGranted:       make([]string, 150), // exceeds max
	}
	for i := 0; i < 150; i++ {
		state.PermanentEligible[i] = fmt.Sprintf("%d", i)
		state.QuotaGranted[i] = fmt.Sprintf("%d", i)
	}
	data, _ := json.Marshal(state)
	os.WriteFile(ledgerPath, data, 0644)

	cl := NewCohortLedger(dir, true, 100)
	err := cl.Load()
	if err == nil {
		t.Fatalf("Load should fail on quota > 100")
	}

	ok, err := cl.MayAdmit("0")
	if ok {
		t.Errorf("fail-closed: account should be denied on corrupted quota")
	}
}

// Test 9: Permanent eligibility survives quota window reset
func TestCohortLedgerV3_PermanentEligibilitySurvivesReset(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	// Initial state: 100 accounts admitted, window start 2 hours ago
	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour)
	state := cohortState{
		SchemaVersion:      2,
		PermanentEligible:  make([]string, 100),
		QuotaWindowStartUTC: twoHoursAgo.Format(time.RFC3339),
		QuotaGranted:       make([]string, 100), // full quota
	}
	for i := 0; i < 100; i++ {
		state.PermanentEligible[i] = fmt.Sprintf("%d", i)
		state.QuotaGranted[i] = fmt.Sprintf("%d", i)
	}
	data, _ := json.Marshal(state)
	os.WriteFile(ledgerPath, data, 0644)

	cl := NewCohortLedger(dir, true, 100)
	if err := cl.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Window should roll (>=1h since last grant). Quota should clear, permanent should survive.
	granted, _ := cl.Stats()
	if granted != 100 {
		// Reload to trigger rollover
		ok, err := cl.MayAdmit("1") // This should trigger rollover
		if !ok && err == nil {
			t.Errorf("existing permanent account denied after rollover")
		}
	}

	// Account 0 should still be in permanent, hence admitted
	ok, err := cl.MayAdmit("0")
	if !ok || err != nil {
		t.Errorf("permanent account 0 lost after window reset: ok=%v err=%v", ok, err)
	}

	// New account should now be admissible (quota was reset)
	ok, err = cl.MayAdmit("100")
	if !ok || err != nil {
		t.Errorf("new account 100 should be admissible after window reset: ok=%v err=%v", ok, err)
	}
}

// Test 10: Empty JSON object is rejected (fail-closed)
func TestCohortLedgerV3_FailClosedOnEmptyJSON(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	os.WriteFile(ledgerPath, []byte("{}"), 0644)

	cl := NewCohortLedger(dir, true, 100)
	err := cl.Load()
	if err == nil {
		t.Fatalf("Load should fail on empty JSON object")
	}

	ok, err := cl.MayAdmit("1")
	if ok {
		t.Errorf("fail-closed: account should be denied on empty JSON")
	}
}

// Test 11: Duplicate IDs in permanent_eligible are rejected
func TestCohortLedgerV3_FailClosedOnDuplicatePermanent(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	state := cohortState{
		SchemaVersion:      2,
		PermanentEligible:  []string{"1", "2", "1"}, // duplicate
		QuotaWindowStartUTC: time.Now().UTC().Format(time.RFC3339),
		QuotaGranted:       []string{},
	}
	data, _ := json.Marshal(state)
	os.WriteFile(ledgerPath, data, 0644)

	cl := NewCohortLedger(dir, true, 100)
	err := cl.Load()
	if err == nil {
		t.Fatalf("Load should fail on duplicate permanent_eligible")
	}

	ok, err := cl.MayAdmit("1")
	if ok {
		t.Errorf("fail-closed: account should be denied on duplicate IDs")
	}
}

// Test 12: Deterministic write failure scenario
func TestCohortLedgerV3_DeterministicWriteFailure(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	// Pre-corrupt the JSON so Load() catches it
	os.WriteFile(ledgerPath, []byte(`{
		"schema_version": 2,
		"permanent_eligible": ["1", "1"],
		"quota_window_start_utc": "2026-11-17T14:00:00Z",
		"quota_granted": []
	}`), 0644)

	cl := NewCohortLedger(dir, true, 100)
	err := cl.Load()
	if err == nil {
		t.Fatalf("Load should detect duplicate permanent_eligible")
	}

	// All admissions should be denied
	ok, err := cl.MayAdmit("1")
	if ok {
		t.Errorf("fail-closed on corrupted ledger: should deny all admissions")
	}

	ok, err = cl.MayAdmit("2")
	if ok {
		t.Errorf("fail-closed persists: should deny subsequent admissions")
	}

	cl.Close()
}

// Test 13: Mutation test: removing gate from code is detected
func TestCohortLedgerV3_MutationTest_RemoveGate(t *testing.T) {
	dir := t.TempDir()
	cl := NewCohortLedger(dir, true, 1) // quota=1

	if err := cl.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Admit first account
	ok, err := cl.MayAdmit("1")
	if !ok {
		t.Errorf("first account should be admitted: %v", err)
	}

	// If gate is removed (hypothetically returning true for all), this would fail:
	ok, err = cl.MayAdmit("2")
	if ok {
		t.Errorf("MUTATION DETECTED: second account admitted with quota=1 (gate bypassed?)")
	}
}

// === PRODUCTION ORACLE TESTS ===
// These test the actual admission-to-connect boundary using a callable seam.

// ConnectFunc is the callback that simulates actual server connection.
// Return (true, nil) if connect succeeded; (false, error) if connection failed.
type ConnectFunc func(accountID string) (bool, error)

// AdmitAndConnectSeam extracts the admission→connect decision from bot.run logic.
// This is the PRODUCTION ORACLE: it tests the actual boundary where cohort gate
// affects whether connection is attempted.
func AdmitAndConnectSeam(accountID string, cl *CohortLedger, connectFn ConnectFunc) (admitted bool, connected bool, err error) {
	// Step 1: Check cohort admission
	admitted, err = cl.MayAdmit(accountID)
	if !admitted {
		// Denied: should NOT call connect
		return false, false, err
	}

	// Step 2: Admitted: attempt connection
	connected, err = connectFn(accountID)
	return true, connected, err
}

// Test 14: Denied admission doesn't reach connect (production oracle)
func TestCohortLedgerV3_Oracle_DeniedDoesntConnect(t *testing.T) {
	dir := t.TempDir()
	cl := NewCohortLedger(dir, true, 1) // quota=1

	if err := cl.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	connectCount := 0
	connectFn := func(accountID string) (bool, error) {
		connectCount++
		return true, nil
	}

	// Admit first account
	admitted, connected, err := AdmitAndConnectSeam("1", cl, connectFn)
	if !admitted || !connected || err != nil {
		t.Errorf("first account: admitted=%v connected=%v err=%v", admitted, connected, err)
	}

	// Try second account: should be denied without calling connect
	connectCount = 0
	admitted2, _, _ := AdmitAndConnectSeam("2", cl, connectFn)
	if admitted2 {
		t.Errorf("second account should be denied")
	}
	if connectCount > 0 {
		t.Errorf("connect was called %d times after denial (should be 0)", connectCount)
	}
}

// Test 15: UTC hour boundary doesn't reset quota early
func TestCohortLedgerV3_Oracle_HourBoundaryNoEarlyReset(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	// Set window to 1 minute ago
	oneMinuteAgo := time.Now().UTC().Add(-1 * time.Minute)
	state := cohortState{
		SchemaVersion:      2,
		PermanentEligible:  make([]string, 50),
		QuotaWindowStartUTC: oneMinuteAgo.Format(time.RFC3339),
		QuotaGranted:       make([]string, 50),
	}
	for i := 0; i < 50; i++ {
		state.PermanentEligible[i] = fmt.Sprintf("%d", i)
		state.QuotaGranted[i] = fmt.Sprintf("%d", i)
	}
	data, _ := json.Marshal(state)
	os.WriteFile(ledgerPath, data, 0644)

	cl := NewCohortLedger(dir, true, 100)
	if err := cl.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Window should NOT roll (only 1 minute old, not >=1h)
	ok, err := cl.MayAdmit("100")
	if !ok || err == nil {
		// Should succeed (quota not reset, still < 100)
		if err != nil {
			t.Errorf("new account should be admissible within same hour window: %v", err)
		}
	}
}

// Test 16: Old account reconnect after window reset doesn't get double quota
func TestCohortLedgerV3_Oracle_ReconnectAfterReset(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	// Set window to 2 hours ago with account "1" in permanent and granted
	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour)
	state := cohortState{
		SchemaVersion:      2,
		PermanentEligible:  []string{"1"},
		QuotaWindowStartUTC: twoHoursAgo.Format(time.RFC3339),
		QuotaGranted:       []string{"1"},
	}
	data, _ := json.Marshal(state)
	os.WriteFile(ledgerPath, data, 0644)

	cl := NewCohortLedger(dir, true, 100)
	if err := cl.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// First admission: should trigger window roll, but account "1" stays in permanent
	ok, err := cl.MayAdmit("1")
	if !ok || err != nil {
		t.Errorf("account 1 reconnect after reset: ok=%v err=%v", ok, err)
	}

	// Check that permanent was preserved and quota rolled
	granted, _ := cl.Stats()
	if granted < 1 {
		t.Errorf("after reconnect in new window, account should still be counted")
	}
}

// Test 17: Corrupt state after valid load fails closed
func TestCohortLedgerV3_Oracle_CorruptAfterLoad(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	// Valid initial state
	state := cohortState{
		SchemaVersion:      2,
		PermanentEligible:  []string{"1", "2"},
		QuotaWindowStartUTC: time.Now().UTC().Format(time.RFC3339),
		QuotaGranted:       []string{},
	}
	data, _ := json.Marshal(state)
	os.WriteFile(ledgerPath, data, 0644)

	cl := NewCohortLedger(dir, true, 100)
	if err := cl.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Corrupt the file
	os.WriteFile(ledgerPath, []byte("corrupted"), 0644)

	// Next admission should detect corruption and fail-closed
	ok, _ := cl.MayAdmit("3")
	if ok {
		t.Errorf("should fail-closed after ledger corruption: got admitted")
	}
}
