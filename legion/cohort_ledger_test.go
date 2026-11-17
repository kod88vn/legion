package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCohortLedger_100InitialAdmitted_101stBlocked(t *testing.T) {
	dir := t.TempDir()
	ledger := NewCohortLedger(dir, true, 100)
	if err := ledger.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// First 100 should be admitted (new grants).
	for i := 1; i <= 100; i++ {
		accountID := "2100" + padInt(i, 3)
		admitted, newGrant, err := ledger.MayAdmit(accountID)
		if err != nil {
			t.Fatalf("MayAdmit(%s) failed: %v", accountID, err)
		}
		if !admitted || !newGrant {
			t.Fatalf("Account %d expected admitted=true, newGrant=true, got %v, %v",
				i, admitted, newGrant)
		}
	}

	// 101st should be blocked.
	admitted, newGrant, err := ledger.MayAdmit("2101000")
	if err != nil {
		t.Fatalf("MayAdmit(2101000) failed: %v", err)
	}
	if admitted || newGrant {
		t.Fatalf("Account 101 expected admitted=false, newGrant=false, got %v, %v",
			admitted, newGrant)
	}
}

func TestCohortLedger_Before1h_Blocked_At1h_NextEligible(t *testing.T) {
	dir := t.TempDir()

	// Use a fake clock by directly manipulating the ledger's window start.
	ledger := NewCohortLedger(dir, true, 100)
	if err := ledger.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Fill quota in this window.
	for i := 1; i <= 100; i++ {
		accountID := "2100" + padInt(i, 3)
		admitted, _, err := ledger.MayAdmit(accountID)
		if err != nil || !admitted {
			t.Fatalf("Failed to admit account %d", i)
		}
	}

	// 101st attempt before 1h should still be blocked.
	admitted, _, err := ledger.MayAdmit("2101000")
	if err != nil {
		t.Fatalf("MayAdmit failed: %v", err)
	}
	if admitted {
		t.Fatalf("Before 1h: 101st account should be blocked")
	}

	// Simulate 1 hour passing by modifying the persisted ledger on disk.
	ledger.mu.Lock()
	ledger.CurrentWindowStart = time.Now().UTC().Add(-time.Hour)
	ledger.mu.Unlock()

	// Save the modified state to disk so the new instance can reload it.
	ledger.mu.Lock()
	data, err := json.Marshal(ledger)
	ledger.mu.Unlock()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	tmp := ledger.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.Rename(tmp, ledger.filePath); err != nil {
		t.Fatalf("Rename failed: %v", err)
	}

	// Now reload from disk to trigger the window rollover logic.
	ledger2 := NewCohortLedger(filepath.Dir(ledger.filePath), true, 100)
	if err := ledger2.Load(); err != nil {
		t.Fatalf("Load after rollover failed: %v", err)
	}

	// First new account after rollover should be admitted.
	admitted, newGrant, err := ledger2.MayAdmit("2101000")
	if err != nil {
		t.Fatalf("MayAdmit failed: %v", err)
	}
	if !admitted || !newGrant {
		t.Fatalf("After 1h: 101st account should be admitted as new grant, got %v, %v",
			admitted, newGrant)
	}
}

func TestCohortLedger_ReconnectNoQuotaConsumed(t *testing.T) {
	dir := t.TempDir()
	ledger := NewCohortLedger(dir, true, 2)
	if err := ledger.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Admit 2 accounts.
	ledger.MayAdmit("2100001")
	ledger.MayAdmit("2100002")

	// 3rd attempt should be blocked.
	admitted, _, err := ledger.MayAdmit("2100003")
	if err != nil || admitted {
		t.Fatalf("3rd account should be blocked")
	}

	// Reconnect with account 1 (already in ledger): should be admitted, no quota consumed.
	admitted, newGrant, err := ledger.MayAdmit("2100001")
	if err != nil {
		t.Fatalf("Reconnect failed: %v", err)
	}
	if !admitted || newGrant {
		t.Fatalf("Reconnect: expected admitted=true, newGrant=false, got %v, %v",
			admitted, newGrant)
	}

	// 3rd should still be blocked (quota not released).
	admitted, _, err = ledger.MayAdmit("2100003")
	if err != nil || admitted {
		t.Fatalf("3rd account should still be blocked after reconnect")
	}
}

func TestCohortLedger_RestartReopensLedger_NoDoubleAdmit(t *testing.T) {
	dir := t.TempDir()

	// First process: admit 100 accounts.
	ledger1 := NewCohortLedger(dir, true, 100)
	if err := ledger1.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	for i := 1; i <= 100; i++ {
		accountID := "2100" + padInt(i, 3)
		admitted, _, err := ledger1.MayAdmit(accountID)
		if err != nil {
			t.Fatalf("MayAdmit %s failed: %v", accountID, err)
		}
		if !admitted {
			t.Fatalf("MayAdmit %s returned admitted=false", accountID)
		}
	}

	// Simulate process restart: new ledger instance reads the same file.
	ledger2 := NewCohortLedger(dir, true, 100)
	if err := ledger2.Load(); err != nil {
		t.Fatalf("Load after restart failed: %v", err)
	}

	// Confirm ledger2 has the same 100 accounts.
	granted, _ := ledger2.Stats()
	if granted != 100 {
		t.Fatalf("After restart: expected 100 granted, got %d", granted)
	}

	// 101st attempt should still be blocked (no fresh quota).
	admitted, _, err := ledger2.MayAdmit("2101000")
	if err != nil || admitted {
		t.Fatalf("After restart: 101st should still be blocked")
	}

	// Existing account (2100001) should reconnect without consuming quota.
	admitted, newGrant, err := ledger2.MayAdmit("2100001")
	if err != nil {
		t.Fatalf("Reconnect after restart failed: %v", err)
	}
	if !admitted || newGrant {
		t.Fatalf("Reconnect after restart: expected admitted=true, newGrant=false, got %v, %v",
			admitted, newGrant)
	}
}

func TestCohortLedger_ConcurrentRequestsNeverExceed100(t *testing.T) {
	dir := t.TempDir()
	ledger := NewCohortLedger(dir, true, 100)
	if err := ledger.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Fire 200 concurrent requests.
	var admitted, newGrants int64
	var wg sync.WaitGroup
	for i := 1; i <= 200; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			accountID := "2200" + padInt(idx, 3)
			ok, newGrant, err := ledger.MayAdmit(accountID)
			if err == nil && ok {
				atomic.AddInt64(&admitted, 1)
			}
			if err == nil && newGrant {
				atomic.AddInt64(&newGrants, 1)
			}
		}(i)
	}
	wg.Wait()

	// newGrants must be exactly 100 (quota).
	if newGrants != 100 {
		t.Fatalf("Concurrent test: expected exactly 100 new grants, got %d", newGrants)
	}

	// admitted should also be 100 (all new grants were admitted).
	if admitted != 100 {
		t.Fatalf("Concurrent test: expected 100 admitted, got %d", admitted)
	}
}

func TestCohortLedger_CorruptLedgerFailsClosed(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")

	// Write corrupt JSON.
	if err := os.WriteFile(ledgerPath, []byte("{invalid json"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	ledger := NewCohortLedger(dir, true, 100)
	err := ledger.Load()
	if err == nil {
		t.Fatalf("Load should have failed on corrupt JSON")
	}

	// Confirm MayAdmit would fail (ledger not loaded).
	// Actually, we need to test what happens if ledger is loaded but corrupt,
	// or we can test that an error from writeLocked is handled.
	// Let's test that a failed write reverts the admission.

	// Fresh ledger, manually corrupt the file after load.
	ledger2 := NewCohortLedger(dir, true, 100)
	ledger2.Load() // Clears the corrupt file, starts fresh.

	// Make the file read-only to force a write error.
	if err := os.Chmod(dir, 0444); err != nil {
		t.Skipf("Cannot chmod dir to read-only: %v", err)
	}
	defer os.Chmod(dir, 0755)

	// Attempt to admit: write should fail, admission reverted.
	admitted, _, err := ledger2.MayAdmit("2100001")
	if err == nil {
		// In some environments (Docker, certain filesystems), chmod might not prevent writes.
		// Skip rather than fail.
		t.Skipf("os.Chmod did not prevent write (environment limitation)")
	}
	if admitted {
		t.Fatalf("MayAdmit failed but returned admitted=true (should fail closed)")
	}

	// Confirm ledger state is unchanged (reverted).
	granted, _ := ledger2.Stats()
	if granted != 0 {
		t.Fatalf("After failed write: granted should be 0, got %d", granted)
	}
}

func TestCohortLedger_DisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	ledger := NewCohortLedger(dir, false, 100) // disabled
	if err := ledger.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// All requests should be admitted (gate disabled).
	for i := 1; i <= 150; i++ {
		accountID := "2300" + padInt(i, 3)
		admitted, _, err := ledger.MayAdmit(accountID)
		if err != nil || !admitted {
			t.Fatalf("Account %d: gate disabled, expected admitted=true", i)
		}
	}

	// No ledger file should have been created.
	if _, err := os.Stat(filepath.Join(dir, "cohort_ledger.json")); !os.IsNotExist(err) {
		t.Fatalf("Gate disabled: ledger file should not exist")
	}
}

func TestCohortLedger_MutationTest_RemoveGateFails(t *testing.T) {
	// This test verifies that the gate logic is actually being checked.
	// If we comment out the gate check, this test fails.
	dir := t.TempDir()
	ledger := NewCohortLedger(dir, true, 2)
	if err := ledger.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	ledger.MayAdmit("2100001")
	ledger.MayAdmit("2100002")

	// 3rd should be blocked; if gate is removed, this fails.
	admitted, _, _ := ledger.MayAdmit("2100003")
	if admitted {
		t.Fatalf("Mutation: gate was removed (3rd admitted when should be blocked)")
	}
}

func TestCohortLedger_MutationTest_RemovePersistenceFails(t *testing.T) {
	// If we don't persist to disk, restart will show the quota reset incorrectly.
	dir := t.TempDir()
	ledger1 := NewCohortLedger(dir, true, 100)
	if err := ledger1.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	for i := 1; i <= 100; i++ {
		accountID := "2100" + padInt(i, 3)
		ledger1.MayAdmit(accountID)
	}

	// If persistence is removed (e.g., writeLocked is stubbed), the file won't exist.
	// New instance should read empty ledger, resetting quota.
	ledger2 := NewCohortLedger(dir, true, 100)
	if err := ledger2.Load(); err != nil {
		t.Fatalf("Load after restart failed: %v", err)
	}

	granted, _ := ledger2.Stats()
	if granted == 0 {
		t.Fatalf("Mutation: persistence was removed (ledger reset on restart)")
	}
	if granted != 100 {
		t.Fatalf("Mutation test: expected 100 granted after restart, got %d", granted)
	}
}

// Helper: pad integer to N digits with leading zeros.
func padInt(n, width int) string {
	s := fmt.Sprintf("%d", n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}
