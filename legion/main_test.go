package main

import (
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
	if _, err := os.Stat(ledgerPath); err == nil {
		t.Fatal("symlink fixture did not produce metadata error")
	}
	
	// Attempt redirect: metadata error should be reported with explicit error context
	_, err := resolveCohortDir(tempDataDir, tempCohortDir, true)
	if err == nil {
		t.Fatal("expected error on metadata error, got nil")
	}
	
	// Error message must explicitly mention "metadata error" context
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
	
	// Verify ledger artifacts are in selectedCohortDir, not tempDataDir
	ledgerPath := filepath.Join(resolvedDir, "cohort_ledger.json")
	lockPath := filepath.Join(resolvedDir, "cohort_ledger.lock")
	
	// After Load() in InitCohortGate, lock file should exist in resolvedDir
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file not found at expected path %q: %v", lockPath, err)
	}
	
	// Verify no artifacts were created in tempDataDir
	oldLedgerPath := filepath.Join(tempDataDir, "cohort_ledger.json")
	oldLockPath := filepath.Join(tempDataDir, "cohort_ledger.lock")
	if _, err := os.Stat(oldLedgerPath); err == nil {
		t.Fatalf("unexpected ledger in original dataDir: %q", oldLedgerPath)
	}
	if _, err := os.Stat(oldLockPath); err == nil {
		t.Fatalf("unexpected lock in original dataDir: %q", oldLockPath)
	}
	
	// Grant an account via CheckCohortAdmission (uses cohortGate.MayAdmit)
	accountID1 := uint32(10001)
	admitted, err := CheckCohortAdmission(accountID1)
	if err != nil {
		t.Fatalf("CheckCohortAdmission failed: %v", err)
	}
	if !admitted {
		t.Fatalf("first account should be admitted, got false")
	}
	
	// Close the gate to persist state
	if err := cohortGate.Close(); err != nil {
		t.Fatalf("cohortGate.Close failed: %v", err)
	}
	
	// Verify ledger file exists and contains the grant
	ledgerData, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("failed to read ledger file: %v", err)
	}
	ledgerStr := string(ledgerData)
	acctStr := formatAccountID(accountID1)
	if !stringContains(ledgerStr, acctStr) {
		t.Fatalf("ledger does not contain granted account %q: %s", acctStr, ledgerStr)
	}
	
	// Reopen gate with same selected directory (simulates restart)
	cohortGate = nil
	if err := InitCohortGate(resolvedDir, true); err != nil {
		t.Fatalf("second InitCohortGate failed: %v", err)
	}
	
	// Verify persisted eligibility: same account should not consume quota
	admitted2, err := CheckCohortAdmission(accountID1)
	if err != nil {
		t.Fatalf("second CheckCohortAdmission failed: %v", err)
	}
	if !admitted2 {
		t.Fatalf("persisted account should still be admitted, got false")
	}
	
	// Clean up: Close the second instance
	if err := cohortGate.Close(); err != nil {
		t.Fatalf("second cohortGate.Close failed: %v", err)
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
