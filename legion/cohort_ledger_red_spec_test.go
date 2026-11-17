package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// === DETERMINISTIC RED-CHECKPOINT TESTS ===
// These tests demonstrate specific failure modes with exact clock control and deterministic I/O.
// Tests run BYTE-IDENTICAL against production code (GREEN) and mutated code (RED via mutation testing).

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

// TEST 2: Atomic Write Collision (Rename Target Already Exists as Directory)
// Demonstrates fail-closed on deterministic I/O failure, prior state preserved.
func TestAtomicWriteCollisionFailsClosed(t *testing.T) {
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
	
	// Now create a directory at the ledger path to cause rename collision
	ledgerPath := ledger.ledgerPath
	if err := os.Mkdir(ledgerPath, 0700); err != nil && !os.IsExist(err) {
		t.Fatalf("cannot create collision directory: %v", err)
	}
	
	// Try to admit another user (will attempt atomic write, rename will fail with EISDIR)
	ok, err = ledger.MayAdmit("user2")
	if ok || err == nil {
		t.Fatalf("write collision should fail: ok=%v err=%v (expected false, error)", ok, err)
	}
	
	// Verify fail-closed state is latched
	ledger.mu.Lock()
	if !ledger.failClosed {
		t.Fatal("FAIL: failClosed not latched after write collision")
	}
	ledger.mu.Unlock()
	
	// All subsequent admits must be denied
	ok, err = ledger.MayAdmit("user3")
	if ok || err == nil {
		t.Fatalf("after fail-closed, should deny all: ok=%v err=%v", ok, err)
	}
	
	// Verify persisted state unchanged (directory present means no write succeeded)
	_, err = os.ReadFile(filepath.Join(dir, "cohort_ledger.json"))
	if err == nil {
		// If file exists under directory, that's an error in test setup
		t.Fatal("ledger file should not exist under collision directory")
	}
	
	t.Logf("PASS: Atomic write collision detected, fail-closed latched, prior state preserved")
}

// TEST 2b: Atomic Writer Wrapper Failure Oracle
// Inject testable writer wrapper at persistLocked call site, force OS error, verify fail-closed on retry.
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
	priorBytes, _ := os.ReadFile(ledger.ledgerPath)
	
	// Inject writer wrapper that counts invocations and forces error on first call
	writerInvoked := 0
	ledger.atomicWriter = func(tmpPath, targetPath string) error {
		writerInvoked++
		// Force OS error: create directory at target to cause EISDIR on rename
		os.Mkdir(targetPath, 0700)
		return fmt.Errorf("simulated rename error: target is directory")
	}
	
	// Try to admit user2 (will invoke atomicWriter, fail with injected error)
	ok, err = ledger.MayAdmit("user2")
	if ok || err == nil {
		t.Fatalf("atomic writer error should fail: ok=%v err=%v", ok, err)
	}
	
	firstInvocation := writerInvoked
	if firstInvocation != 1 {
		t.Fatalf("expected 1 writer invocation, got %d", firstInvocation)
	}
	
	// Verify prior ledger unchanged
	currentBytes, _ := os.ReadFile(ledger.ledgerPath)
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
	
	t.Logf("PASS: Atomic writer error detected, fail-closed latched, no retry write invoked")
}

// TEST 2c: Retry After Failure Denial with Healthy Writer (Failclosed Latch Required)
// Same ledger, same writer wrapper. Inject fault for first attempt, then remove fault.
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
	
	// Inject writer wrapper that fails (simulates I/O error)
	faultActive := true
	writerInvoked := 0
	ledger.atomicWriter = func(tmpPath, targetPath string) error {
		writerInvoked++
		if faultActive {
			os.Mkdir(targetPath, 0700)
			return fmt.Errorf("simulated rename error: target is directory")
		}
		// If fault inactive, delegate to os.Rename (healthy)
		return os.Rename(tmpPath, targetPath)
	}
	
	// Try to admit user2 with fault active (will fail, latch failClosed)
	ok, err = ledger.MayAdmit("user2")
	if ok || err == nil {
		t.Fatalf("write with fault: ok=%v err=%v", ok, err)
	}
	
	firstInvocation := writerInvoked
	if firstInvocation != 1 {
		t.Fatalf("expected 1 writer invocation, got %d", firstInvocation)
	}
	
	// Now remove the fault (restore healthy writer)
	faultActive = false
	
	// Retry with user3 and healthy writer
	// Should STILL deny because failClosed latch prevents any retry
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
