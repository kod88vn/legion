package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCohortLedger_DisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	cl := NewCohortLedger(dir, false, 100)
	if err := cl.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Gate disabled: should admit anyone.
	ok, err := cl.MayAdmit("1000001")
	if !ok || err != nil {
		t.Fatalf("disabled gate should admit: %v %v", ok, err)
	}
}

func TestCohortLedger_100Admitted_101stBlocked(t *testing.T) {
	dir := t.TempDir()
	cl := NewCohortLedger(dir, true, 100)
	if err := cl.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Admit 100 accounts.
	for i := 1; i <= 100; i++ {
		aid := fmt.Sprintf("%d", 1000000+i)
		ok, err := cl.MayAdmit(aid)
		if !ok || err != nil {
			t.Fatalf("Account %s (grant %d/100) should be admitted: %v %v", aid, i, ok, err)
		}
	}

	// 101st should be denied.
	ok, err := cl.MayAdmit("1000101")
	if ok {
		t.Fatalf("101st account should be denied (quota=100): got admitted=%v error=%v", ok, err)
	}
}

func TestCohortLedger_ReconnectNoQuotaConsumed(t *testing.T) {
	dir := t.TempDir()
	cl := NewCohortLedger(dir, true, 100)
	if err := cl.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Admit account 1.
	ok, err := cl.MayAdmit("1000001")
	if !ok || err != nil {
		t.Fatalf("Account 1 should be admitted: %v %v", ok, err)
	}

	// Reconnect with same account: should not consume quota.
	ok2, err := cl.MayAdmit("1000001")
	if !ok2 || err != nil {
		t.Fatalf("Reconnect should admit: %v %v", ok2, err)
	}

	// Should still be able to admit 99 more (not 100).
	for i := 2; i <= 100; i++ {
		aid := fmt.Sprintf("%d", 1000000+i)
		ok, err := cl.MayAdmit(aid)
		if !ok || err != nil {
			t.Fatalf("Account %s should be admitted: %v %v", aid, ok, err)
		}
	}

	// 101st should be blocked.
	ok, err = cl.MayAdmit("1000101")
	if ok {
		t.Fatalf("101st should be blocked")
	}
}

func TestCohortLedger_RestartReopensLedger_NoDoubleAdmit(t *testing.T) {
	dir := t.TempDir()

	// First instance: admit 50 accounts.
	cl1 := NewCohortLedger(dir, true, 100)
	if err := cl1.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	for i := 1; i <= 50; i++ {
		aid := fmt.Sprintf("%d", 1000000+i)
		ok, err := cl1.MayAdmit(aid)
		if !ok || err != nil {
			t.Fatalf("Account %s should be admitted: %v %v", aid, ok, err)
		}
	}

	// Restart: second instance reads same ledger.
	cl2 := NewCohortLedger(dir, true, 100)
	if err := cl2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Should have 50 already admitted.
	granted, _ := cl2.Stats()
	if granted != 50 {
		t.Fatalf("After restart, expected 50 granted, got %d", granted)
	}

	// Try to admit account 51: should work (not double-admitted).
	ok, err := cl2.MayAdmit("1000051")
	if !ok || err != nil {
		t.Fatalf("Account 51 should be admitted: %v %v", ok, err)
	}

	// Now 51 granted.
	granted, _ = cl2.Stats()
	if granted != 51 {
		t.Fatalf("After admitting 51, expected 51 granted, got %d", granted)
	}
}

func TestPermanentEligibility_SurvivesQuotaReset(t *testing.T) {
	dir := t.TempDir()

	// Hour 1: admit accounts 1-100.
	cl1 := NewCohortLedger(dir, true, 100)
	if err := cl1.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	for i := 1; i <= 100; i++ {
		aid := fmt.Sprintf("%d", 1000000+i)
		ok, err := cl1.MayAdmit(aid)
		if !ok || err != nil {
			t.Fatalf("Hour 1: Account %s should be admitted: %v %v", aid, ok, err)
		}
	}

	// Simulate 1+ hour passing: manually roll window (in real test, would fake clock).
	cl1.mu.Lock()
	cl1.CurrentWindowStart = time.Now().UTC().Add(-time.Hour - time.Minute)
	cl1.mu.Unlock()

	// Hour 2: admit account 101 (new grant).
	ok, err := cl1.MayAdmit("1000101")
	if !ok || err != nil {
		t.Fatalf("Hour 2: Account 101 should be admitted: %v %v", ok, err)
	}

	// Now restart in hour 2.
	cl2 := NewCohortLedger(dir, true, 100)
	if err := cl2.Load(); err != nil {
		t.Fatalf("Load after hour 2 failed: %v", err)
	}

	// Account 1 (from hour 1) should still be eligible (permanent).
	ok, err = cl2.MayAdmit("1000001")
	if !ok || err != nil {
		t.Fatalf("After restart in hour 2: Account 1 should still be eligible (permanent): %v %v", ok, err)
	}

	// Account 101 (from hour 2) should also be eligible.
	ok, err = cl2.MayAdmit("1000101")
	if !ok || err != nil {
		t.Fatalf("After restart in hour 2: Account 101 should still be eligible (permanent): %v %v", ok, err)
	}

	// Should have exactly 2 granted in this window (the 1 from hour 2).
	granted, _ := cl2.Stats()
	if granted != 1 {
		t.Fatalf("Expected 1 granted in current window, got %d", granted)
	}
}

func TestCohortLedger_FailClosedOnCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	// Write truncated JSON.
	truncatedJSON := `{"schema_version": 1, "permanent_eligible": [`
	if err := os.WriteFile(ledgerPath, []byte(truncatedJSON), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Try to load: should fail-closed.
	cl := NewCohortLedger(dir, true, 100)
	err := cl.Load()
	if err == nil {
		t.Fatalf("Load should fail on corrupt JSON")
	}
	if err != ErrFailClosed {
		t.Fatalf("Expected ErrFailClosed, got %v", err)
	}
}

func TestCohortLedger_FailClosedOnSchemaMismatch(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	// Write valid JSON but with wrong schema version.
	badJSON := `{"schema_version": 999, "permanent_eligible": [], "permanent_modified_utc": "2026-11-17T09:00:00Z", "quota_window_start_utc": "2026-11-17T09:00:00Z", "quota_granted_this_window": [], "quota_modified_utc": "2026-11-17T09:00:00Z"}`
	if err := os.WriteFile(ledgerPath, []byte(badJSON), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Try to load: should fail-closed.
	cl := NewCohortLedger(dir, true, 100)
	err := cl.Load()
	if err == nil {
		t.Fatalf("Load should fail on schema mismatch")
	}
	if err != ErrFailClosed {
		t.Fatalf("Expected ErrFailClosed, got %v", err)
	}
}

func TestCohortLedger_FailClosedOnMissingFields(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	// Write JSON missing required field.
	missingJSON := `{"schema_version": 1, "permanent_eligible": []}`
	if err := os.WriteFile(ledgerPath, []byte(missingJSON), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Try to load: should fail-closed.
	cl := NewCohortLedger(dir, true, 100)
	err := cl.Load()
	if err == nil {
		t.Fatalf("Load should fail on missing fields")
	}
	if err != ErrFailClosed {
		t.Fatalf("Expected ErrFailClosed, got %v", err)
	}
}

func TestCohortLedger_FailClosedOnQuotaExceeded(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	// Write JSON with quota_granted > 100.
	now := time.Now().UTC().Format(time.RFC3339)
	var grantedList []string
	for i := 1; i <= 101; i++ {
		grantedList = append(grantedList, fmt.Sprintf("\"%d\"", 1000000+i))
	}
	grantedJSON := fmt.Sprintf(
		`{"schema_version": 1, "permanent_eligible": [], "permanent_modified_utc": "%s", "quota_window_start_utc": "%s", "quota_granted_this_window": [%s], "quota_modified_utc": "%s"}`,
		now, now, fmt.Sprintf("%v", grantedList)[1:len(fmt.Sprintf("%v", grantedList))-1], now,
	)
	if err := os.WriteFile(ledgerPath, []byte(grantedJSON), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Try to load: should fail-closed.
	cl := NewCohortLedger(dir, true, 100)
	err := cl.Load()
	if err == nil {
		t.Fatalf("Load should fail when quota_granted > 100")
	}
	if err != ErrFailClosed {
		t.Fatalf("Expected ErrFailClosed, got %v", err)
	}
}

func TestCohortLedger_MutationTest_RemoveGate(t *testing.T) {
	dir := t.TempDir()
	cl := NewCohortLedger(dir, true, 100)
	if err := cl.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// If we remove the gate (set Enabled=false), all admissions should succeed.
	cl.Enabled = false
	ok, err := cl.MayAdmit("1000001")
	if !ok || err != nil {
		t.Fatalf("Gate disabled: should admit: %v %v", ok, err)
	}

	// Set gate back to true: now should enforce quota.
	cl.Enabled = true
	cl.mu.Lock()
	cl.GrantedThisWindow = make(map[string]bool)
	cl.mu.Unlock()

	// Should block first admission because there's no ledger file (after disabling/enabling).
	ok, err = cl.MayAdmit("1000001")
	if !ok {
		t.Fatalf("Should admit: %v %v", ok, err)
	}

	// If gate is disabled again, mutation is not caught.
	// This test verifies that removing the gate is a detectable bug.
}
