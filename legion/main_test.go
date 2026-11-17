package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveCohortDirDefaultsToDataDir verifies that an empty cohort directory
// defaults to dataDir, preserving legacy behavior when no override is given.
func TestResolveCohortDirDefaultsToDataDir(t *testing.T) {
	tempDataDir := t.TempDir()
	
	// Empty cohortDir, enabled=true: should return dataDir
	result, err := resolveCohortDir(tempDataDir, "", true)
	if err != nil {
		t.Fatalf("resolveCohortDir(dataDir, \"\", true) failed: %v", err)
	}
	if result != tempDataDir {
		t.Fatalf("expected %q, got %q", tempDataDir, result)
	}
}

// TestResolveCohortDirDisabledReturnsDataDirWithoutIO verifies that when the feature
// is disabled, the function returns dataDir immediately without inspecting legacy history.
func TestResolveCohortDirDisabledReturnsDataDirWithoutIO(t *testing.T) {
	tempDataDir := t.TempDir()
	
	// Create a legacy ledger in dataDir to verify it is NOT inspected when disabled
	ledgerPath := filepath.Join(tempDataDir, "cohort_ledger.json")
	if err := os.WriteFile(ledgerPath, []byte(`{"state":{}}`), 0644); err != nil {
		t.Fatalf("failed to create test ledger: %v", err)
	}
	
	// Disabled (enabled=false) with explicit redirect attempt: should NOT inspect and NOT error
	tempCohortDir := t.TempDir()
	result, err := resolveCohortDir(tempDataDir, tempCohortDir, false)
	if err != nil {
		t.Fatalf("resolveCohortDir(dataDir, explicit, false) failed: %v", err)
	}
	if result != tempDataDir {
		t.Fatalf("expected %q (dataDir fallback when disabled), got %q", tempDataDir, result)
	}
}

// TestResolveCohortDirExplicitCleanDirectory verifies that an explicit override to a clean
// directory (no legacy artifacts) is allowed.
func TestResolveCohortDirExplicitCleanDirectory(t *testing.T) {
	tempDataDir := t.TempDir()
	tempCohortDir := t.TempDir()
	
	// Explicit redirect to clean directory, enabled=true: should succeed
	result, err := resolveCohortDir(tempDataDir, tempCohortDir, true)
	if err != nil {
		t.Fatalf("resolveCohortDir(dataDir, cleanDir, true) failed: %v", err)
	}
	if result != tempCohortDir {
		t.Fatalf("expected %q, got %q", tempCohortDir, result)
	}
}

// TestResolveCohortDirRedirectionWithExistingLedger verifies that redirection is refused
// when a legacy cohort ledger already exists in the original dataDir.
func TestResolveCohortDirRedirectionWithExistingLedger(t *testing.T) {
	tempDataDir := t.TempDir()
	tempCohortDir := t.TempDir()
	
	// Create a legacy ledger in dataDir
	ledgerPath := filepath.Join(tempDataDir, "cohort_ledger.json")
	if err := os.WriteFile(ledgerPath, []byte(`{"state":{}}`), 0644); err != nil {
		t.Fatalf("failed to create test ledger: %v", err)
	}
	
	// Attempt redirect away from dataDir with existing ledger: should fail
	_, err := resolveCohortDir(tempDataDir, tempCohortDir, true)
	if err == nil {
		t.Fatal("expected error when redirecting with legacy ledger, got nil")
	}
	
	// Error message must mention the ledger path
	if errMsg := err.Error(); errMsg == "" || !stringContains(errMsg, "cohort_ledger") {
		t.Fatalf("error message does not mention ledger: %v", err)
	}
}

// TestResolveCohortDirRedirectionWithExistingMarker verifies that redirection is refused
// when a legacy cohort initialization marker already exists in the original dataDir.
func TestResolveCohortDirRedirectionWithExistingMarker(t *testing.T) {
	tempDataDir := t.TempDir()
	tempCohortDir := t.TempDir()
	
	// Create a legacy initialization marker in dataDir
	markerPath := filepath.Join(tempDataDir, ".cohort_initialized")
	if err := os.WriteFile(markerPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create test marker: %v", err)
	}
	
	// Attempt redirect away from dataDir with existing marker: should fail
	_, err := resolveCohortDir(tempDataDir, tempCohortDir, true)
	if err == nil {
		t.Fatal("expected error when redirecting with legacy marker, got nil")
	}
	
	// Error message must mention the marker path
	if errMsg := err.Error(); errMsg == "" || !stringContains(errMsg, ".cohort_initialized") {
		t.Fatalf("error message does not mention marker: %v", err)
	}
}

// TestResolveCohortDirSameDirectoryAllowed verifies that specifying the same directory
// as both dataDir and cohortDir is allowed (no actual redirection).
func TestResolveCohortDirSameDirectoryAllowed(t *testing.T) {
	tempDataDir := t.TempDir()
	
	// Same directory override (no redirection): should succeed even with artifacts
	ledgerPath := filepath.Join(tempDataDir, "cohort_ledger.json")
	if err := os.WriteFile(ledgerPath, []byte(`{"state":{}}`), 0644); err != nil {
		t.Fatalf("failed to create test ledger: %v", err)
	}
	
	// Explicitly pass the same directory: should succeed (no actual redirection)
	result, err := resolveCohortDir(tempDataDir, tempDataDir, true)
	if err != nil {
		t.Fatalf("resolveCohortDir(dataDir, sameDir, true) failed: %v", err)
	}
	if result != tempDataDir {
		t.Fatalf("expected %q, got %q", tempDataDir, result)
	}
}

// TestResolveCohortDirMetadataErrorRefusal verifies that unexpected metadata errors
// (such as ELOOP from symlink loops) are reported and startup fails. This test proves
// that actual metadata-error handling code path is exercised; a compiled bypass mutant
// that skips metadata error checks would fail this test.
func TestResolveCohortDirMetadataErrorRefusal(t *testing.T) {
	tempDataDir := t.TempDir()
	tempCohortDir := t.TempDir()
	
	// Create a self-referential symlink at the ledger path to trigger ELOOP.
	// This produces an actual metadata error, not just a directory-exists condition.
	ledgerPath := filepath.Join(tempDataDir, "cohort_ledger.json")
	if err := os.Symlink(ledgerPath, ledgerPath); err != nil {
		t.Fatalf("failed to create self-referential symlink: %v", err)
	}
	
	// Verify that os.Stat on the symlink produces an error (ELOOP or similar)
	statErr := error(nil)
	if _, err := os.Stat(ledgerPath); err == nil {
		t.Fatal("symlink fixture did not produce metadata error")
	} else {
		statErr = err
	}
	
	// Verify fixture actually produces a symbolic link loop error
	errStr := statErr.Error()
	if !stringContains(errStr, "too many levels") && !stringContains(errStr, "loop") && !stringContains(errStr, "ELOOP") {
		t.Fatalf("symlink fixture must produce loop error, got: %v", statErr)
	}
	
	// Attempt redirect: metadata error should be reported with explicit error context
	_, err := resolveCohortDir(tempDataDir, tempCohortDir, true)
	if err == nil {
		t.Fatal("expected error on metadata error, got nil")
	}
	
	// Error message must explicitly mention "metadata error" context (production error contract)
	errMsg := err.Error()
	if errMsg == "" || !stringContains(errMsg, "metadata error") {
		t.Fatalf("error message does not contain 'metadata error': %v", err)
	}
}

// TestCohortDirectoryIntegrationWithRealGate verifies that the resolver works with
// actual InitCohortGate, persists eligibility/quota across restart, and keeps all
// artifacts under the selected directory (not dataDir).
func TestCohortDirectoryIntegrationWithRealGate(t *testing.T) {
	tempDataDir := t.TempDir()
	selectedCohortDir := t.TempDir()
	
	// Save and restore global cohortGate to avoid test isolation issues
	oldGate := cohortGate
	defer func() { cohortGate = oldGate }()
	
	// Resolve directory (selected is clean, no redirection)
	resolvedDir, err := resolveCohortDir(tempDataDir, selectedCohortDir, true)
	if err != nil {
		t.Fatalf("resolveCohortDir failed: %v", err)
	}
	if resolvedDir != selectedCohortDir {
		t.Fatalf("expected %q, got %q", selectedCohortDir, resolvedDir)
	}
	
	// Initialize cohort gate with resolved directory (enabled, 100 per hour)
	if err := InitCohortGate(resolvedDir, true); err != nil {
		t.Fatalf("InitCohortGate failed: %v", err)
	}
	
	// Verify cohortGate is set
	if cohortGate == nil {
		t.Fatal("cohortGate not initialized")
	}
	
	// Lock file path - exists after Load()
	lockPath := filepath.Join(resolvedDir, "cohort_ledger.lock")
	
	// After Load() in InitCohortGate, lock should exist in resolvedDir
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file not found at expected path %q: %v", lockPath, err)
	}
	
	// Verify no lock in tempDataDir (original data root)
	oldLockPath := filepath.Join(tempDataDir, "cohort_ledger.lock")
	if _, err := os.Stat(oldLockPath); err == nil {
		t.Fatalf("unexpected lock in original dataDir: %q", oldLockPath)
	}
	
	// Grant an account via CheckCohortAdmission (uses cohortGate.MayAdmit)
	accountID1 := uint32(10001)
	
	ledgerPath := filepath.Join(resolvedDir, "cohort_ledger.json")
	markerPath := filepath.Join(resolvedDir, ".cohort_initialized")
	admitted, err := CheckCohortAdmission(accountID1)
	if err != nil {
		t.Fatalf("CheckCohortAdmission failed: %v", err)
	}
	if !admitted {
		t.Fatalf("first account should be admitted, got false")
	}
	
	// After first grant, ledger and marker files should exist in resolvedDir
	if _, err := os.Stat(ledgerPath); err != nil {
		t.Fatalf("ledger file not created at expected path %q after first grant: %v", ledgerPath, err)
	}
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("marker file not created at expected path %q after first grant: %v", markerPath, err)
	}
	
	// Verify ledger and marker are NOT in tempDataDir (original data root)
	oldLedgerPath := filepath.Join(tempDataDir, "cohort_ledger.json")
	oldMarkerPath := filepath.Join(tempDataDir, ".cohort_initialized")
	if _, err := os.Stat(oldLedgerPath); err == nil {
		t.Fatalf("unexpected ledger in original dataDir: %q", oldLedgerPath)
	}
	if _, err := os.Stat(oldMarkerPath); err == nil {
		t.Fatalf("unexpected marker in original dataDir: %q", oldMarkerPath)
	}
	
	// Close the gate to persist state
	if err := cohortGate.Close(); err != nil {
		t.Fatalf("cohortGate.Close failed: %v", err)
	}
	
	// Verify ledger file exists and contains the grant, capture persisted state
	ledgerData, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("failed to read ledger file: %v", err)
	}
	ledgerStr := string(ledgerData)
	acctStr := formatAccountID(accountID1)
	if !stringContains(ledgerStr, acctStr) {
		t.Fatalf("ledger does not contain granted account %q: %s", acctStr, ledgerStr)
	}
	
	// Extract persisted window start and grants from JSON (use production field names)
	var persistedState struct {
		SchemaVersion        int      `json:"schema_version"`
		QuotaWindowStartUTC  string   `json:"quota_window_start_utc"`
		QuotaGranted         []string `json:"quota_granted"`
		PermanentEligible    []string `json:"permanent_eligible"`
	}
	if err := json.Unmarshal(ledgerData, &persistedState); err != nil {
		t.Fatalf("failed to parse persisted ledger JSON: %v", err)
	}
	capturedWindowStart := persistedState.QuotaWindowStartUTC
	capturedGranted := persistedState.QuotaGranted
	
	// Verify captured state has the first grant (accountID1)
	foundAccount1 := false
	for _, grant := range capturedGranted {
		if grant == acctStr {
			foundAccount1 = true
			break
		}
	}
	if !foundAccount1 {
		t.Fatalf("persisted grants do not contain account %q: %v", acctStr, capturedGranted)
	}
	
	// Check if capturedGranted is not empty (should have at least 1)
	if len(capturedGranted) < 1 {
		t.Fatalf("expected at least 1 grant after first admission, got %d: %v", len(capturedGranted), capturedGranted)
	}
	
	// Fill remaining quota (99 more accounts to reach 100 cap)
	cohortGate = nil
	if err := InitCohortGate(resolvedDir, true); err != nil {
		t.Fatalf("second InitCohortGate (reopen for fill) failed: %v", err)
	}
	
	// Grant 99 more accounts to reach cap (account1 already granted, so grant 2-100)
	for i := 2; i <= 100; i++ {
		accountID := uint32(10000 + i)
		admitted, err := CheckCohortAdmission(accountID)
		if err != nil {
			t.Fatalf("CheckCohortAdmission %d failed: %v", accountID, err)
		}
		if !admitted {
			t.Fatalf("account %d should be admitted (quota not yet full at grant #%d), got false", accountID, i)
		}
	}
	
	// Close after filling (should have 100 total grants)
	if err := cohortGate.Close(); err != nil {
		t.Fatalf("cohortGate.Close (after fill) failed: %v", err)
	}
	
	// Reopen gate with same selected directory (simulates restart after fill)
	cohortGate = nil
	if err := InitCohortGate(resolvedDir, true); err != nil {
		t.Fatalf("third InitCohortGate (after restart) failed: %v", err)
	}
	
	// Verify persisted window start and grants unchanged after restart
	ledgerData2, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("failed to read ledger file after restart: %v", err)
	}
	var persistedState2 struct {
		SchemaVersion        int      `json:"schema_version"`
		QuotaWindowStartUTC  string   `json:"quota_window_start_utc"`
		QuotaGranted         []string `json:"quota_granted"`
		PermanentEligible    []string `json:"permanent_eligible"`
	}
	if err := json.Unmarshal(ledgerData2, &persistedState2); err != nil {
		t.Fatalf("failed to parse persisted ledger JSON after restart: %v", err)
	}
	
	// Verify window start unchanged
	if persistedState2.QuotaWindowStartUTC != capturedWindowStart {
		t.Fatalf("window start changed after restart: before %q, after %q", capturedWindowStart, persistedState2.QuotaWindowStartUTC)
	}
	
	// Verify grants unchanged (still at least 100)
	if len(persistedState2.QuotaGranted) < 100 {
		t.Fatalf("expected at least 100 grants after restart, got %d", len(persistedState2.QuotaGranted))
	}
	
	// Verify accountID1 reconnect consumes no quota (uses permanent eligibility)
	admitted1, err := CheckCohortAdmission(accountID1)
	if err != nil {
		t.Fatalf("CheckCohortAdmission (reconnect) failed: %v", err)
	}
	if !admitted1 {
		t.Fatalf("persisted account should still be admitted, got false")
	}
	
	// Verify fresh account is denied at cap with proper quota exhaustion error
	freshAccountID := uint32(20000)
	admitted3, err := CheckCohortAdmission(freshAccountID)
	if admitted3 {
		t.Fatal("fresh account should be denied at cap, got true")
	}
	if err == nil {
		t.Fatal("fresh account denial must include error, got nil")
	}
	errMsg := err.Error()
	if !stringContains(errMsg, "quota exhausted") {
		t.Fatalf("denial must cite 'quota exhausted', got: %v", err)
	}
	
	// Clean up: Close the final instance
	if err := cohortGate.Close(); err != nil {
		t.Fatalf("final cohortGate.Close failed: %v", err)
	}
}

// stringContains is a helper to check if a string contains a substring.
func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
