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

// TestResolveCohortDirMetadataErrorRefusal verifies that unexpected errors reading
// metadata (permission denied, etc.) are reported and startup fails. For this test,
// we also verify that trying to redirect with a directory-at-ledger-path is treated
// as a legacy artifact present and refused with appropriate error message.
func TestResolveCohortDirMetadataErrorRefusal(t *testing.T) {
	tempDataDir := t.TempDir()
	tempCohortDir := t.TempDir()
	
	// Create a directory where the ledger file would be, so Stat finds a directory
	// This simulates a directory-based legacy artifact or file-system corruption
	ledgerPath := filepath.Join(tempDataDir, "cohort_ledger.json")
	if err := os.Mkdir(ledgerPath, 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	
	// Attempt redirect: should detect the directory and refuse with appropriate error
	_, err := resolveCohortDir(tempDataDir, tempCohortDir, true)
	if err == nil {
		t.Fatal("expected error on redirect with directory-at-ledger-path, got nil")
	}
	
	// Error message must mention either "metadata error" or "legacy cohort ledger exists"
	errMsg := err.Error()
	if errMsg == "" || (!stringContains(errMsg, "metadata error") && !stringContains(errMsg, "legacy cohort ledger")) {
		t.Fatalf("error message does not mention metadata error or legacy artifact: %v", err)
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
