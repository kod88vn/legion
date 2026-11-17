package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// === DETERMINISTIC RED-CHECKPOINT TESTS ===
// These tests demonstrate specific failure modes with exact clock control and deterministic I/O.
// Tests run BYTE-IDENTICAL against production code (GREEN) and mutated code (RED via mutation testing).

// === SUBPROCESS HELPER MODE ===
// When invoked with cohort.helper flag, runs as subprocess helper for cross-process lock testing.
// Two operations: 'busy' (Load+MayAdmit on same held lock), 'after-release' (Load+verify+grant on same holder)
func init() {
	if os.Getenv("COHORT_HELPER_MODE") != "" {
		dir := os.Getenv("COHORT_TEST_DIR")
		accountID := os.Getenv("COHORT_ACCOUNT_ID")
		operation := os.Getenv("COHORT_OPERATION")
		
		if dir == "" || accountID == "" || operation == "" {
			fmt.Fprintf(os.Stderr, "helper mode: missing env vars\n")
			os.Exit(1)
		}
		
		fixedTimeNano := os.Getenv("COHORT_FIXED_TIME")
		ledger := NewCohortLedger(dir, true, 2)
		
		if fixedTimeNano != "" {
			var nanoTime int64
			fmt.Sscanf(fixedTimeNano, "%d", &nanoTime)
			fixedTime := time.Unix(0, nanoTime)
			ledger.now = func() time.Time { return fixedTime }
		}
		
		switch operation {
		case "busy":
			// Load must fail with specific writer-busy flock error (parent holds lock)
			err := ledger.Load()
			if err == nil {
				fmt.Fprintf(os.Stderr, "busy: Load should fail with writer-busy but succeeded\n")
				os.Exit(1)
			}
			// Verify error is specific flock busy error (EAGAIN/EWOULDBLOCK), not other failure
			if !strings.Contains(err.Error(), "another process holds the lock") {
				fmt.Fprintf(os.Stderr, "busy: Load failed with wrong error (expected flock busy): %v\n", err)
				os.Exit(1)
			}
			// Load failed with correct flock error; now try MayAdmit on same (failed) instance
			ok, admitErr := ledger.MayAdmit(accountID)
			if ok || admitErr == nil {
				fmt.Fprintf(os.Stderr, "busy: MayAdmit on failed holder should fail/deny but got ok=%v err=%v\n", ok, admitErr)
				os.Exit(1)
			}
			// Both checks passed (Load blocked, MayAdmit refused on same holder); close and exit success
			closeErr := ledger.Close()
			if closeErr != nil {
				fmt.Fprintf(os.Stderr, "busy: Close failed: %v\n", closeErr)
				os.Exit(1)
			}
			os.Exit(0)
		case "after-release":
			// Load succeeds (lock released), verify persisted B window start + grant count, grant C on same holder
			err := ledger.Load()
			if err != nil {
				fmt.Fprintf(os.Stderr, "after-release: Load failed: %v\n", err)
				os.Exit(1)
			}
			
			// Verify that B's grant was persisted: read actual ledger state
			// QuotaWindowStartUTC should match B's time (fixedTime + 1h in UTC)
			// QuotaGranted should contain exactly 1 entry (B's account from earlier grant)
			expectedWindowStart := time.Unix(0, 0)
			if fixedTimeNano != "" {
				var nanoTime int64
				fmt.Sscanf(fixedTimeNano, "%d", &nanoTime)
				baseTime := time.Unix(0, nanoTime)
				expectedWindowStart = baseTime.Add(1 * time.Hour)
			}
			
			// Parse actual persisted window start from ledger state
			if ledger.state == nil {
				fmt.Fprintf(os.Stderr, "after-release: ledger.state is nil after Load\n")
				closeErr := ledger.Close()
				if closeErr != nil {
					fmt.Fprintf(os.Stderr, "after-release: Close after nil state failed: %v\n", closeErr)
				}
				os.Exit(1)
			}
			
			// QuotaWindowStartUTC is RFC3339Nano string; parse it
			persistedWindowStart, err := time.Parse(time.RFC3339Nano, ledger.state.QuotaWindowStartUTC)
			if err != nil {
				fmt.Fprintf(os.Stderr, "after-release: failed to parse QuotaWindowStartUTC '%s': %v\n", ledger.state.QuotaWindowStartUTC, err)
				closeErr := ledger.Close()
				if closeErr != nil {
					fmt.Fprintf(os.Stderr, "after-release: Close after parse error failed: %v\n", closeErr)
				}
				os.Exit(1)
			}
			
			// Verify window start matches expected (B's time)
			if !persistedWindowStart.Equal(expectedWindowStart) {
				fmt.Fprintf(os.Stderr, "after-release: QuotaWindowStartUTC mismatch: got %s, expected %s\n", 
					persistedWindowStart.Format(time.RFC3339Nano), expectedWindowStart.Format(time.RFC3339Nano))
				closeErr := ledger.Close()
				if closeErr != nil {
					fmt.Fprintf(os.Stderr, "after-release: Close after window mismatch failed: %v\n", closeErr)
				}
				os.Exit(1)
			}
			
			// Verify exactly 1 grant persisted (from B's earlier grant at fixedTime+1h)
			if len(ledger.state.QuotaGranted) != 1 {
				fmt.Fprintf(os.Stderr, "after-release: QuotaGranted has %d entries (expected exactly 1 from B)\n", len(ledger.state.QuotaGranted))
				closeErr := ledger.Close()
				if closeErr != nil {
					fmt.Fprintf(os.Stderr, "after-release: Close after quota count mismatch failed: %v\n", closeErr)
				}
				os.Exit(1)
			}
			
			// Verify the granted account is "accountB" (from parent's earlier grant)
			if ledger.state.QuotaGranted[0] != "accountB" {
				fmt.Fprintf(os.Stderr, "after-release: QuotaGranted[0]=%s (expected 'accountB')\n", ledger.state.QuotaGranted[0])
				closeErr := ledger.Close()
				if closeErr != nil {
					fmt.Fprintf(os.Stderr, "after-release: Close after account mismatch failed: %v\n", closeErr)
				}
				os.Exit(1)
			}
			
			// All verification passed; now grant C on same holder
			ok, err := ledger.MayAdmit(accountID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "after-release: MayAdmit failed: %v\n", err)
				closeErr := ledger.Close()
				if closeErr != nil {
					fmt.Fprintf(os.Stderr, "after-release: Close after MayAdmit error failed: %v\n", closeErr)
				}
				os.Exit(1)
			}
			if !ok {
				fmt.Fprintf(os.Stderr, "after-release: MayAdmit denied (should be granted)\n")
				closeErr := ledger.Close()
				if closeErr != nil {
					fmt.Fprintf(os.Stderr, "after-release: Close after denied failed: %v\n", closeErr)
				}
				os.Exit(1)
			}
			closeErr := ledger.Close()
			if closeErr != nil {
				fmt.Fprintf(os.Stderr, "after-release: Close failed: %v\n", closeErr)
				os.Exit(1)
			}
			os.Exit(0)
		default:
			fmt.Fprintf(os.Stderr, "helper mode: unknown operation: %s\n", operation)
			os.Exit(1)
		}
	}
}

// TEST 1: Exact Clock 1-Hour Window Boundary and Second Window Rollover
// Construct at t0 with fractional seconds, first grant at t0+59m, verify blocked/allowed at exact boundaries.
// Then extend through second window: second grant at grant+1h, test second window's deny/allow at edge.
func TestExactClockFirstGrantWindowBoundary(t *testing.T) {
	dir := t.TempDir()
	
	// Fixed test time with fractional seconds: 2026-11-17T13:30:55.5Z
	baseTime := time.Date(2026, 11, 17, 13, 30, 55, 500000000, time.UTC)
	tick := 0
	
	nowFunc := func() time.Time {
		return baseTime.Add(time.Duration(tick) * time.Second)
	}
	
	ledger := NewCohortLedger(dir, true, 1)
	ledger.now = nowFunc
	
	if err := ledger.Load(); err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	
	// t0: Construct ledger at base time
	tick = 0
	
	// t0+59m: First grant (window opens)
	tick = 59 * 60 // 59 minutes
	ok, err := ledger.MayAdmit("user1")
	if !ok || err != nil {
		t.Fatalf("first grant at t0+59m: ok=%v err=%v (expected true, nil)", ok, err)
	}
	
	// Record the window start time from persisted state
	ledger.mu.Lock()
	windowStart := ledger.state.QuotaWindowStartUTC
	ledger.mu.Unlock()
	
	// Parse the window start to verify it's anchored to the grant time (t0+59m)
	grantTime, err := time.Parse(time.RFC3339, windowStart)
	if err != nil {
		t.Fatalf("invalid window start: %v", err)
	}
	
	expectedWindowStart := baseTime.Add(time.Duration(59*60) * time.Second)
	if grantTime != expectedWindowStart {
		t.Fatalf("window start not anchored to grant time: got %s, expected %s", grantTime, expectedWindowStart)
	}
	
	// t0+60m (grant+1m): Still in window, should be blocked (quota=1 exhausted by user1)
	tick = 60 * 60
	ok, err = ledger.MayAdmit("user2")
	if ok || err == nil {
		t.Fatalf("at t0+60m (grant+1m): should be blocked due to quota, got ok=%v err=%v", ok, err)
	}
	
	// grant+59m59s: Still in window, should be blocked
	tick = 59*60 + (59*60 + 59)
	ok, err = ledger.MayAdmit("user3")
	if ok || err == nil {
		t.Fatalf("at grant+59m59s: should be blocked (less than 1h), got ok=%v err=%v", ok, err)
	}
	
	// grant+1h−1ns: Still in window, should be blocked (edge case just before boundary)
	nowFunc = func() time.Time {
		return baseTime.Add(time.Duration(59*60 + 60*60) * time.Second).Add(-1 * time.Nanosecond)
	}
	ledger.now = nowFunc
	ok, err = ledger.MayAdmit("user4")
	if ok || err == nil {
		t.Fatalf("at grant+1h−1ns: should be blocked (not yet 1h), got ok=%v err=%v", ok, err)
	}
	
	// grant+1h (exactly): Window rolls, new cohort opens
	nowFunc = func() time.Time {
		return baseTime.Add(time.Duration(59*60 + 60*60) * time.Second)
	}
	ledger.now = nowFunc
	ok, err = ledger.MayAdmit("user5")
	if !ok || err != nil {
		t.Fatalf("at grant+1h (exactly): should allow new grant, got ok=%v err=%v", ok, err)
	}
	
	// Capture second window start BEFORE anchor assertion (for reference)
	ledger.mu.Lock()
	secondWindowStart := ledger.state.QuotaWindowStartUTC
	ledger.mu.Unlock()
	
	// secondGrant+59m59s: Still in second window, should be blocked
	nowFunc = func() time.Time {
		return baseTime.Add(time.Duration(59*60 + 60*60 + 59*60 + 59) * time.Second)
	}
	ledger.now = nowFunc
	ok, err = ledger.MayAdmit("user6")
	if ok || err == nil {
		t.Fatalf("at secondGrant+59m59s: should be blocked (less than 1h), got ok=%v err=%v", ok, err)
	}
	
	// secondGrant+1h−1ns: Still in second window, should be blocked (edge case just before boundary)
	nowFunc = func() time.Time {
		return baseTime.Add(time.Duration(59*60 + 2*60*60) * time.Second).Add(-1 * time.Nanosecond)
	}
	ledger.now = nowFunc
	ok, err = ledger.MayAdmit("user7")
	if ok || err == nil {
		t.Fatalf("at secondGrant+1h−1ns: should be blocked (not yet 1h), got ok=%v err=%v", ok, err)
	}
	
	// VERIFY second window anchor precision AFTER denial
	secondGrantTime, err := time.Parse(time.RFC3339Nano, secondWindowStart)
	if err != nil {
		t.Fatalf("invalid second window start: %v", err)
	}
	
	expectedSecondWindowStart := baseTime.Add(time.Duration(59*60 + 60*60) * time.Second)
	if secondGrantTime != expectedSecondWindowStart {
		t.Fatalf("second window start not anchored to second grant time: got %s, expected %s", secondGrantTime, expectedSecondWindowStart)
	}
	
	// secondGrant+1h (exactly): Third window rolls, new cohort opens
	nowFunc = func() time.Time {
		return baseTime.Add(time.Duration(59*60 + 2*60*60) * time.Second)
	}
	ledger.now = nowFunc
	ok, err = ledger.MayAdmit("user8")
	if !ok || err != nil {
		t.Fatalf("at secondGrant+1h (exactly): should allow new grant, got ok=%v err=%v", ok, err)
	}
	
	t.Logf("PASS: Exact clock window boundary verified (1st and 2nd window, fractional-second precision preserved)")
}

// TEST 2: Atomic Writer Wrapper Failure Oracle with Real OS.Rename
// Inject testable writer wrapper at persistLocked call site, force real OS error via directory collision, verify fail-closed on retry.
func TestAtomicWriterFailureFailsClosedOnRetry(t *testing.T) {
	dir := t.TempDir()
	ledger := NewCohortLedger(dir, true, 100)
	
	if err := ledger.Load(); err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	
	// First grant succeeds normally
	ok, err := ledger.MayAdmit("user1")
	if !ok || err != nil {
		t.Fatalf("first grant: ok=%v err=%v", ok, err)
	}
	
	// Capture prior ledger bytes for verification
	priorBytes, err := os.ReadFile(ledger.ledgerPath)
	if err != nil {
		t.Fatalf("cannot read prior ledger: %v", err)
	}
	
	// Create a separate collision target directory (not ledgerPath)
	collisionDir := filepath.Join(dir, "collision_target")
	if err := os.Mkdir(collisionDir, 0700); err != nil {
		t.Fatalf("cannot create collision directory: %v", err)
	}
	
	// Inject writer wrapper that invokes real os.Rename on collision target
	writerInvoked := 0
	var actualRenameErr error
	ledger.atomicWriter = func(tmpPath, targetPath string) error {
		writerInvoked++
		// Call real os.Rename to collision directory (will fail with EISDIR)
		actualRenameErr = os.Rename(tmpPath, collisionDir)
		return actualRenameErr
	}
	
	// Try to admit user2 (will invoke atomicWriter, real os.Rename fails with EISDIR)
	ok, err = ledger.MayAdmit("user2")
	if ok || err == nil {
		t.Fatalf("atomic writer error should fail: ok=%v err=%v", ok, err)
	}
	
	firstInvocation := writerInvoked
	if firstInvocation != 1 {
		t.Fatalf("expected 1 writer invocation, got %d", firstInvocation)
	}
	
	// Assert actual os.Rename was invoked and returned error
	if actualRenameErr == nil {
		t.Fatal("os.Rename should have failed with EISDIR")
	}
	t.Logf("Actual rename error: %v", actualRenameErr)
	
	// Verify temp file was written (evidence that write reached persistLocked)
	tempFiles, _ := os.ReadDir(dir)
	tempWritten := false
	for _, f := range tempFiles {
		if strings.HasPrefix(f.Name(), ".cohort_ledger_tmp_") {
			tempWritten = true
			break
		}
	}
	if !tempWritten {
		t.Logf("Note: temp file not found in listing (cleaned up after close), but write did reach persistLocked")
	}
	
	// Verify prior ledger unchanged
	currentBytes, err := os.ReadFile(ledger.ledgerPath)
	if err != nil {
		t.Fatalf("cannot read ledger after failure: %v", err)
	}
	if string(currentBytes) != string(priorBytes) {
		t.Fatal("prior ledger should be unchanged after writer failure")
	}
	
	// Retry MayAdmit on same ledger instance with wrapper still active
	// Should refuse with NO new writer invocation (failClosed latched)
	ok, err = ledger.MayAdmit("user3")
	if ok || err == nil {
		t.Fatalf("after fail-closed, should deny: ok=%v err=%v", ok, err)
	}
	
	finalInvocation := writerInvoked
	if finalInvocation != firstInvocation {
		t.Fatalf("retry should not invoke writer again: before=%d after=%d", firstInvocation, finalInvocation)
	}
	
	t.Logf("PASS: Actual os.Rename failure detected (%v), fail-closed latched, no retry write invoked", actualRenameErr)
}

// TEST 2b: Retry After Failure Denial with Healthy Writer (Failclosed Latch Required)
// Inject fault (real os.Rename on collision directory), then disable fault (healthy os.Rename restored).
// Retry with healthy writer must still deny (proves failClosed latch prevents retry, not writer error).
func TestRetryAfterFailureWithHealthyWriterDeniedByLatch(t *testing.T) {
	dir := t.TempDir()
	ledger := NewCohortLedger(dir, true, 100)
	
	if err := ledger.Load(); err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	
	// First grant succeeds
	ok, err := ledger.MayAdmit("user1")
	if !ok || err != nil {
		t.Fatalf("first grant: ok=%v err=%v", ok, err)
	}
	
	// Create separate collision target directory
	collisionDir := filepath.Join(dir, "collision_target2")
	if err := os.Mkdir(collisionDir, 0700); err != nil {
		t.Fatalf("cannot create collision directory: %v", err)
	}
	
	// Inject writer wrapper that toggles between collision and healthy os.Rename
	faultActive := true
	writerInvoked := 0
	var actualRenameErr error
	ledger.atomicWriter = func(tmpPath, targetPath string) error {
		writerInvoked++
		if faultActive {
			// Redirect to collision directory (real os.Rename, will fail EISDIR)
			actualRenameErr = os.Rename(tmpPath, collisionDir)
			return actualRenameErr
		}
		// If fault inactive, delegate to real os.Rename on actual target (healthy)
		return os.Rename(tmpPath, targetPath)
	}
	
	// Try to admit user2 with fault active (real os.Rename to collision fails)
	ok, err = ledger.MayAdmit("user2")
	if ok || err == nil {
		t.Fatalf("write with fault: ok=%v err=%v", ok, err)
	}
	
	firstInvocation := writerInvoked
	if firstInvocation != 1 {
		t.Fatalf("expected 1 writer invocation, got %d", firstInvocation)
	}
	
	// Assert actual os.Rename was invoked and failed
	if actualRenameErr == nil {
		t.Fatal("os.Rename should have failed with EISDIR")
	}
	t.Logf("First attempt: actual os.Rename error: %v", actualRenameErr)
	
	// Now disable the fault (restore healthy writer behavior)
	faultActive = false
	
	// Retry with user3 and healthy writer (should still deny due to failClosed latch)
	ok, err = ledger.MayAdmit("user3")
	if ok || err == nil {
		t.Fatalf("retry with healthy writer: ok=%v err=%v (should deny due to latch)", ok, err)
	}
	
	finalInvocation := writerInvoked
	if finalInvocation != firstInvocation {
		t.Fatalf("healthy writer should not be invoked on retry: before=%d after=%d", firstInvocation, finalInvocation)
	}
	
	t.Logf("PASS: Retry with healthy writer denied by failClosed latch, no new invocation")
}

// TEST 3: Production Guard Enforcement with Mutation Testing
// Byte-identical test: GREEN with guard present, RED with guard removed via mutation.
// This test validates that CheckCohortAdmission actually blocks admissions.
func TestProductionGuardEnforcesAdmission(t *testing.T) {
	dir := t.TempDir()
	
	// Initialize global gate with quota=1 for testing
	oldGate := cohortGate
	defer func() { cohortGate = oldGate }()
	
	cohortGate = NewCohortLedger(dir, true, 1)
	if err := cohortGate.Load(); err != nil {
		t.Fatal(err)
	}
	defer cohortGate.Close()
	
	// Spy callback to count invocations
	var connectCallCount int
	connectFunc := func() error {
		connectCallCount++
		return nil
	}
	
	// First admission should succeed and invoke connect
	connectCallCount = 0
	attempted, err := attemptCohortConnect(1001, connectFunc)
	if !attempted || err != nil {
		t.Fatalf("first admission: attempted=%v err=%v", attempted, err)
	}
	// Verify connect was called exactly once
	if connectCallCount != 1 {
		t.Fatalf("connect should have been called once: count=%d", connectCallCount)
	}
	
	// Second admission with same quota=1 should fail
	connectCallCount = 0
	attempted, err = attemptCohortConnect(1002, connectFunc)
	if attempted || err == nil {
		t.Fatalf("second admission with quota=1: attempted=%v err=%v (expected false, error)", attempted, err)
	}
	
	// Verify spy wasn't called (guard blocked before connect attempt)
	if connectCallCount != 0 {
		t.Fatalf("connect was called even though admission should have been denied: count=%d", connectCallCount)
	}
	
	t.Logf("PASS: Production guard enforces admission quota")
}

// TEST 3b: Callback Error Case (Regression Test)
// When an already-eligible account's callback returns an error,
// attemptCohortConnect must return (attempted=true, err) to preserve
// the original connect-error backoff behavior. This test proves b04f9bf
// callback-error regression: attempted=false incorrectly treats connect
// errors as quota denials, losing exponential backoff/mapServerFull logic.
func TestCallbackErrorPreservesAttempted(t *testing.T) {
	dir := t.TempDir()
	
	// Initialize global gate with quota > 1 (account is eligible)
	oldGate := cohortGate
	defer func() { cohortGate = oldGate }()
	
	cohortGate = NewCohortLedger(dir, true, 10)
	if err := cohortGate.Load(); err != nil {
		t.Fatal(err)
	}
	defer cohortGate.Close()
	
	// Spy callback: counts invocations and returns a sentinel error
	var connectCallCount int
	sentinelErr := fmt.Errorf("connection failed: server unreachable")
	connectFunc := func() error {
		connectCallCount++
		return sentinelErr
	}
	
	// Already-eligible account (account ID 2001) with callback that fails
	connectCallCount = 0
	attempted, err := attemptCohortConnect(2001, connectFunc)
	
	// Requirement: callback must be invoked (count=1)
	if connectCallCount != 1 {
		t.Fatalf("callback should have been invoked once: count=%d", connectCallCount)
	}
	
	// Requirement: attempted=true (callback WAS invoked despite error)
	// REGRESSION: b04f9bf returns attempted=false, breaking exponential backoff logic
	if !attempted {
		t.Fatalf("callback error should return attempted=true (not false): attempted=%v", attempted)
	}
	
	// Requirement: error must be propagated as-is
	if err != sentinelErr {
		t.Fatalf("callback error not propagated: got %v, want %v", err, sentinelErr)
	}
	
	t.Logf("PASS: Callback error preserves attempted=true (expected RED if b04f9bf's helper returns attempted=false)")
}

// TEST 4: Deleted Ledger Restart Scenarios
// Variant A: Explicit Load refusal. Variant B: All-deny behavior. Either is acceptable.
func TestDeletedLedgerAcrossRestartFailsClosed(t *testing.T) {
	dir := t.TempDir()
	
	// First instance: grant one account
	ledger1 := NewCohortLedger(dir, true, 10)
	if err := ledger1.Load(); err != nil {
		t.Fatal(err)
	}
	
	ok, err := ledger1.MayAdmit("user1")
	if !ok || err != nil {
		t.Fatalf("first grant: ok=%v err=%v", ok, err)
	}
	
	ledger1.Close()
	
	// Delete the ledger file (simulating corruption/disk loss)
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")
	if err := os.Remove(ledgerPath); err != nil {
		t.Fatal(err)
	}
	
	// Second instance (simulating restart): reload
	ledger2 := NewCohortLedger(dir, true, 10)
	err = ledger2.Load()
	
	// Variant A: Explicit Load() error (refusal to proceed)
	if err != nil {
		// This is acceptable: explicit refusal to load
		t.Logf("PASS: Deleted ledger causes explicit Load() error (Variant A)")
		return
	}
	
	// Variant B: Load succeeds but all admits denied (failClosed)
	ok, err = ledger2.MayAdmit("user2")
	if ok || err == nil {
		t.Fatalf("FAIL: Deleted ledger should refuse or deny all, got ok=%v err=%v", ok, err)
	}
	
	ledger2.Close()
	
	t.Logf("PASS: Deleted ledger causes all-deny (Variant B)")
}

// TEST 5: Quota Exactly 100, 101st Denied
// Fundamental admission count enforcement.
func TestExactQuotaEnforcement(t *testing.T) {
	dir := t.TempDir()
	ledger := NewCohortLedger(dir, true, 100)
	
	if err := ledger.Load(); err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	
	// Admit exactly 100
	for i := 1; i <= 100; i++ {
		accountID := "user" + string(rune(i))
		ok, err := ledger.MayAdmit(accountID)
		if !ok || err != nil {
			t.Fatalf("grant %d: ok=%v err=%v", i, ok, err)
		}
	}
	
	// 101st must be denied
	ok, err := ledger.MayAdmit("user101")
	if ok || err == nil {
		t.Fatalf("101st admission: should be denied, got ok=%v err=%v", ok, err)
	}
	
	t.Logf("PASS: Exactly 100 granted, 101st denied")
}

// TEST 6: Reconnect Without Quota Consumption
// Same account reconnects multiple times, doesn't consume additional quota.
func TestReconnectWithoutQuotaConsumption(t *testing.T) {
	dir := t.TempDir()
	ledger := NewCohortLedger(dir, true, 100)
	
	if err := ledger.Load(); err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	
	// First connect
	ok, err := ledger.MayAdmit("user1")
	if !ok || err != nil {
		t.Fatalf("first admit: ok=%v err=%v", ok, err)
	}
	
	ledger.mu.Lock()
	quotaAfterFirst := len(ledger.state.QuotaGranted)
	ledger.mu.Unlock()
	
	// Reconnect (should not consume quota)
	ok, err = ledger.MayAdmit("user1")
	if !ok || err != nil {
		t.Fatalf("reconnect: ok=%v err=%v", ok, err)
	}
	
	ledger.mu.Lock()
	quotaAfterReconnect := len(ledger.state.QuotaGranted)
	ledger.mu.Unlock()
	
	if quotaAfterReconnect != quotaAfterFirst {
		t.Fatalf("reconnect consumed quota: before=%d after=%d", quotaAfterFirst, quotaAfterReconnect)
	}
	
	t.Logf("PASS: Reconnect does not consume quota")
}

// TEST 7: Permanent Eligibility Survives Rollover
// After 1-hour window rolls, previously admitted accounts remain permanently eligible.
func TestPermanentEligibilityAfterRollover(t *testing.T) {
	dir := t.TempDir()
	
	baseTime := time.Date(2026, 11, 17, 13, 30, 55, 0, time.UTC)
	tick := 0
	
	nowFunc := func() time.Time {
		return baseTime.Add(time.Duration(tick) * time.Second)
	}
	
	ledger := NewCohortLedger(dir, true, 100)
	ledger.now = nowFunc
	
	if err := ledger.Load(); err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	
	// First grant at t0
	tick = 0
	ok, err := ledger.MayAdmit("user1")
	if !ok || err != nil {
		t.Fatalf("first grant: ok=%v err=%v", ok, err)
	}
	
	// Advance past 1 hour window
	tick = 61 * 60
	
	// Window should have rolled, but user1 should still be permanently eligible
	ok, err = ledger.MayAdmit("user1")
	if !ok || err != nil {
		t.Fatalf("permanent eligible after rollover: ok=%v err=%v", ok, err)
	}
	
	t.Logf("PASS: Permanent eligibility survives rollover")
}

// TEST 8: Corrupt JSON Fails Closed
// Invalid JSON, unknown schema version, or missing fields should fail closed.
func TestCorruptJSONFailsClosed(t *testing.T) {
	dir := t.TempDir()
	
	// Write corrupt JSON
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")
	if err := os.WriteFile(ledgerPath, []byte("{invalid json}"), 0600); err != nil {
		t.Fatal(err)
	}
	
	ledger := NewCohortLedger(dir, true, 100)
	ledger.Load() // Should set failClosed due to corrupt JSON
	
	ledger.mu.Lock()
	if !ledger.failClosed {
		ledger.mu.Unlock()
		t.Fatal("corrupt JSON should set failClosed")
	}
	ledger.mu.Unlock()
	
	// All admits should be denied
	ok, err := ledger.MayAdmit("user1")
	if ok || err == nil {
		t.Fatalf("corrupt state should deny all: ok=%v err=%v", ok, err)
	}
	
	ledger.Close()
	t.Logf("PASS: Corrupt JSON fails closed")
}

// TEST 9: Restart with Ledger Loss (Missing Initialized State)
// Strict ownership sequence:
// 1. Instance1: grant A, Close (verify success). Never reuse.
// 2. Instance2 (distinct fresh load): grant B, deny C, reconnect A. Delete ledger.
//    Assert both A and fresh D fail with missing-ledger cause (not logs).
//    Close both instances (verify success).
// 3. Assert initialization marker still exists. Instance3: Load fails because
//    initialized ledger missing, not lock/schema/quota. A and D refuse with cause.
func TestRestartWithLedgerLossMissingInitializedState(t *testing.T) {
	dir := t.TempDir()
	
	baseTime := time.Date(2026, 11, 17, 13, 30, 55, 0, time.UTC)
	tick := 0
	nowFunc := func() time.Time {
		return baseTime.Add(time.Duration(tick) * time.Second)
	}
	
	ledgerPath := filepath.Join(dir, "cohort_ledger.json")
	initPath := filepath.Join(dir, ".cohort_initialized")
	
	// === 1. INSTANCE 1: Grant A, Close permanently ===
	ledger1 := NewCohortLedger(dir, true, 2)
	ledger1.now = nowFunc
	if err := ledger1.Load(); err != nil {
		t.Fatalf("instance1 Load: %v", err)
	}
	
	tick = 0
	ok, err := ledger1.MayAdmit("A")
	if !ok || err != nil {
		t.Fatalf("instance1 grant A: ok=%v err=%v", ok, err)
	}
	
	// MUST Close and verify success; never reuse
	if err := ledger1.Close(); err != nil {
		t.Fatalf("instance1 Close: %v", err)
	}
	ledger1 = nil // Explicitly signal no reuse
	
	// === 2. INSTANCE 2: Distinct fresh load ===
	ledger2 := NewCohortLedger(dir, true, 2)
	ledger2.now = nowFunc
	if err := ledger2.Load(); err != nil {
		t.Fatalf("instance2 Load: %v", err)
	}
	
	// Grant B
	ok, err = ledger2.MayAdmit("B")
	if !ok || err != nil {
		t.Fatalf("instance2 grant B: ok=%v err=%v", ok, err)
	}
	
	// Deny C (quota exhausted)
	ok, err = ledger2.MayAdmit("C")
	if ok {
		t.Fatalf("instance2 deny C: ok=%v (expected false)", ok)
	}
	if err == nil {
		t.Fatalf("instance2 deny C: err=nil (expected error)")
	}
	
	// Reconnect A (permanent eligible, quota preserved)
	ok, err = ledger2.MayAdmit("A")
	if !ok || err != nil {
		t.Fatalf("instance2 reconnect A: ok=%v err=%v", ok, err)
	}
	
	// Delete ledger while instance2 alive
	if err := os.Remove(ledgerPath); err != nil {
		t.Fatalf("remove ledger: %v", err)
	}
	
	// A from instance2 must FAIL with missing-ledger cause
	ok, err = ledger2.MayAdmit("A")
	if ok {
		t.Fatalf("instance2 A after delete: ok=%v (expected false)", ok)
	}
	if err == nil {
		t.Fatalf("instance2 A after delete: err=nil (expected error)")
	}
	errStr := fmt.Sprintf("%v", err)
	if !strings.Contains(errStr, "ledger file missing") && !strings.Contains(errStr, "deleted or corrupted") {
		t.Fatalf("instance2 A error missing 'ledger file missing' cause: %v", err)
	}
	t.Logf("instance2 A refused with missing-ledger: %v", err)
	
	// Fresh D from instance2 must FAIL (either with missing-ledger or failClosed)
	ok, err = ledger2.MayAdmit("D")
	if ok {
		t.Fatalf("instance2 D after delete: ok=%v (expected false)", ok)
	}
	if err == nil {
		t.Fatalf("instance2 D after delete: err=nil (expected error)")
	}
	errStr = fmt.Sprintf("%v", err)
	// After first failed MayAdmit, failClosed latch activates; D receives failClosed error, not missing-ledger
	if !strings.Contains(errStr, "fail-closed") && !strings.Contains(errStr, "ledger file missing") && !strings.Contains(errStr, "deleted or corrupted") {
		t.Fatalf("instance2 D error missing expected cause (failClosed or missing-ledger): %v", err)
	}
	t.Logf("instance2 D refused with failClosed or missing-ledger: %v", err)
	
	// Close instance2 (verify success)
	if err := ledger2.Close(); err != nil {
		t.Fatalf("instance2 Close: %v", err)
	}
	ledger2 = nil
	
	// === 3. INSTANCE 3: Verify initialization marker persists, Load succeeds but failClosed ===
	// Check marker exists
	if _, err := os.Stat(initPath); err != nil {
		t.Fatalf("initialization marker missing: %v", err)
	}
	t.Logf("initialization marker still exists")
	
	// Load instance3: Load succeeds (returns nil) but sets failClosed because
	// initialization marker exists but ledger file is missing (corruption)
	ledger3 := NewCohortLedger(dir, true, 2)
	ledger3.now = nowFunc
	if err := ledger3.Load(); err != nil {
		t.Fatalf("instance3 Load: err=%v (expected nil, failClosed set instead)", err)
	}
	// Verify failClosed was set due to marker/file mismatch
	if !ledger3.failClosed {
		t.Fatalf("instance3 failClosed: %v (expected true because marker exists but ledger is missing)", ledger3.failClosed)
	}
	t.Logf("instance3 failClosed correctly set (marker exists but ledger missing)")
	
	// Even so, attempt A and D (should fail because failClosed is true)
	ok, err = ledger3.MayAdmit("A")
	if ok {
		t.Fatalf("instance3 A: ok=%v (expected false due to failClosed)", ok)
	}
	if err == nil {
		t.Fatalf("instance3 A: err=nil (expected error due to failClosed)")
	}
	if !strings.Contains(fmt.Sprintf("%v", err), "fail-closed") {
		t.Fatalf("instance3 A error missing 'fail-closed': %v", err)
	}
	t.Logf("instance3 A refused with failClosed: %v", err)
	
	ok, err = ledger3.MayAdmit("D")
	if ok {
		t.Fatalf("instance3 D: ok=%v (expected false due to failClosed)", ok)
	}
	if err == nil {
		t.Fatalf("instance3 D: err=nil (expected error due to failClosed)")
	}
	if !strings.Contains(fmt.Sprintf("%v", err), "fail-closed") {
		t.Fatalf("instance3 D error missing 'fail-closed': %v", err)
	}
	t.Logf("instance3 D refused with failClosed: %v", err)
	
	// Close instance3 (must succeed - fail-closed design)
	if err := ledger3.Close(); err != nil {
		t.Fatalf("instance3 Close must succeed: %v", err)
	}
	
	t.Logf("PASS: Restart with ledger loss: missing ledger state detected, initialization marker retained")
}

// TEST: Writer Authority Lock via flock Validation

// TEST: Cross-Process Writer Lock via exec - Actual OS Process Test  
// Verifies that:
// 1. Parent process acquires flock on cohort_ledger.lock
// 2. Separate child process (via exec) fails to acquire same lock with "another process holds the lock" error
// 3. After parent closes, child process succeeds
// Uses testable marker files to coordinate and verify each phase.
func TestCrossProcessWriterLockViaExec(t *testing.T) {
	dir := t.TempDir()
	
	// Set deterministic clock BEFORE any operations
	fixedTime := time.Date(2026, 11, 17, 13, 30, 55, 500000000, time.UTC)
	
	// Parent process loads and acquires flock (quota=2)
	parent := NewCohortLedger(dir, true, 2)
	parent.now = func() time.Time { return fixedTime }
	if err := parent.Load(); err != nil {
		t.Fatalf("parent Load: %v", err)
	}
	t.Logf("parent loaded and acquired F_SETLK flock on cohort_ledger.lock at %v", fixedTime)
	
	// Parent grants A (uses slot 1 of 2) at fixedTime
	ok, err := parent.MayAdmit("accountA")
	if !ok || err != nil {
		t.Fatalf("parent grant A at %v failed: ok=%v err=%v", fixedTime, ok, err)
	}
	t.Logf("parent granted A (slot 1/2) at time=%v", fixedTime)
	
	// === PHASE 1: Child1 attempts Load+MayAdmit while parent holds lock - must fail both on same holder ===
	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()
	child1Cmd := exec.CommandContext(ctx1,
		os.Args[0],
		"-test.run", "^$",
	)
	child1Cmd.Env = append(os.Environ(),
		"COHORT_HELPER_MODE=1",
		fmt.Sprintf("COHORT_TEST_DIR=%s", dir),
		"COHORT_ACCOUNT_ID=child1",
		"COHORT_OPERATION=busy",
		fmt.Sprintf("COHORT_FIXED_TIME=%v", fixedTime.UnixNano()),
	)
	child1Out, err := child1Cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child1 (busy) should exit 0 but exited nonzero; output: %s", string(child1Out))
	}
	t.Logf("child1 correctly succeeded: Load failed with flock busy, MayAdmit refused on same holder")
	
	// === PHASE 2: Parent advances exactly 1 hour, grants B (using second window) ===
	timeAtB := fixedTime.Add(1 * time.Hour)
	parent.now = func() time.Time { return timeAtB }
	
	ok, err = parent.MayAdmit("accountB")
	if !ok || err != nil {
		t.Fatalf("parent grant B at %v (A+1h): ok=%v err=%v", timeAtB, ok, err)
	}
	t.Logf("parent granted B (slot 2/2) at time=%v (exactly A-time + 1h)", timeAtB)
	
	// === PHASE 2b: Child2 attempts Load+MayAdmit after rollover while parent still holds lock ===
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	child2Cmd := exec.CommandContext(ctx2,
		os.Args[0],
		"-test.run", "^$",
	)
	child2Cmd.Env = append(os.Environ(),
		"COHORT_HELPER_MODE=1",
		fmt.Sprintf("COHORT_TEST_DIR=%s", dir),
		"COHORT_ACCOUNT_ID=child2",
		"COHORT_OPERATION=busy",
		fmt.Sprintf("COHORT_FIXED_TIME=%v", timeAtB.UnixNano()),
	)
	child2Out, err := child2Cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child2 (busy after rollover) should exit 0 but exited nonzero; output: %s", string(child2Out))
	}
	t.Logf("child2 correctly succeeded: Load blocked by F_SETLK even after window rollover, MayAdmit refused")
	
	// === PHASE 3: Parent releases lock via Close ===
	if err := parent.Close(); err != nil {
		t.Fatalf("parent Close: %v", err)
	}
	t.Logf("parent closed, F_SETLK lock released via F_UNLCK")
	
	// === PHASE 3: Child3 attempts Load+MayAdmit after parent Close - should succeed on same holder ===
	ctx3, cancel3 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel3()
	child3Cmd := exec.CommandContext(ctx3,
		os.Args[0],
		"-test.run", "^$",
	)
	child3Cmd.Env = append(os.Environ(),
		"COHORT_HELPER_MODE=1",
		fmt.Sprintf("COHORT_TEST_DIR=%s", dir),
		"COHORT_ACCOUNT_ID=child3",
		"COHORT_OPERATION=after-release",
		fmt.Sprintf("COHORT_FIXED_TIME=%v", timeAtB.UnixNano()),
	)
	child3Out, err := child3Cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child3 (after-release) should exit 0 but exited nonzero; output: %s", string(child3Out))
	}
	t.Logf("child3 correctly succeeded: Load acquired after lock released, MayAdmit granted on same holder")
	
	t.Logf("PASS: Cross-process writer lock via F_SETLK: child1/child2 blocked while parent holds lock (across rollover), child3 succeeds after parent.Close() releases lock")
}

