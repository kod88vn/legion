package main

import (
 "os"
 "path/filepath"
 "testing"
 "time"
)

func TestAtOneHourWithoutRestartOpensNextCohort(t *testing.T) {
 l := NewCohortLedger(t.TempDir(), true, 1)
 if err := l.Load(); err != nil { t.Fatal(err) }
 if ok, _, err := l.MayAdmit("2100001"); !ok || err != nil { t.Fatalf("first grant: %v %v", ok, err) }
 l.mu.Lock()
 l.CurrentWindowStart = time.Now().UTC().Add(-time.Hour)
 if err := l.writeLocked(); err != nil { t.Fatal(err) }
 l.mu.Unlock()
 if ok, _, err := l.MayAdmit("2100002"); !ok || err != nil { t.Fatalf("after >=1h without restart, next cohort refused: admitted=%v error=%v", ok, err) }
}

func TestConcurrentInstancesDoNotOverwriteGrants(t *testing.T) {
 dir := t.TempDir()
 a, b := NewCohortLedger(dir, true, 1), NewCohortLedger(dir, true, 1)
 if err := a.Load(); err != nil { t.Fatal(err) }; if err := b.Load(); err != nil { t.Fatal(err) }
 if ok, _, err := a.MayAdmit("2100001"); !ok || err != nil { t.Fatalf("first: %v %v", ok, err) }
 if ok, _, err := b.MayAdmit("2100002"); ok || err == nil { t.Fatalf("competing writer admitted second account with quota=1: admitted=%v error=%v", ok, err) }
}

func TestCorruptDiskAfterLoadFailsClosed(t *testing.T) {
 l := NewCohortLedger(t.TempDir(), true, 2)
 if err := l.Load(); err != nil { t.Fatal(err) }
 if ok, _, err := l.MayAdmit("2100001"); !ok || err != nil { t.Fatalf("first: %v %v", ok, err) }
 if err := os.WriteFile(filepath.Join(filepath.Dir(l.filePath), "cohort_ledger.json"), []byte("{bad"), 0600); err != nil { t.Fatal(err) }
 if ok, _, err := l.MayAdmit("2100002"); ok || err == nil { t.Fatalf("corrupt ledger overwritten rather than failing closed: admitted=%v error=%v", ok, err) }
}

func TestUTCBoundaryExactly1hOpensNewCohort(t *testing.T) {
 l := NewCohortLedger(t.TempDir(), true, 2)
 if err := l.Load(); err != nil { t.Fatal(err) }
 if ok, _, err := l.MayAdmit("2100001"); !ok || err != nil { t.Fatalf("first grant before boundary: %v %v", ok, err) }
 
 l.mu.Lock()
 // Set window to exactly 1 hour ago (not 1h+1ms, exactly 1h).
 l.CurrentWindowStart = time.Now().UTC().Add(-time.Hour)
 if err := l.writeLocked(); err != nil { t.Fatal(err) }
 l.mu.Unlock()
 
 // At exactly 1 hour, next admission should open new cohort and succeed.
 if ok, _, err := l.MayAdmit("2100002"); !ok || err != nil { t.Fatalf("second grant at exactly 1h boundary: %v %v", ok, err) }
}

func TestPathTypeWriteFailureFailsClosed(t *testing.T) {
 // Create a file at the ledger path to cause os.Rename to fail (can't rename file over file without special handling).
 // Then ensure admission fails.
 dir := t.TempDir()
 ledgerPath := filepath.Join(dir, "cohort_ledger.json")
 
 // Pre-create the ledger with one grant.
 l := NewCohortLedger(dir, true, 2)
 if err := l.Load(); err != nil { t.Fatal(err) }
 if ok, _, err := l.MayAdmit("2100001"); !ok || err != nil { t.Fatalf("first grant: %v %v", ok, err) }
 
 // Now create a directory at the ledger path to block subsequent writes.
 os.Remove(ledgerPath)
 if err := os.Mkdir(ledgerPath, 0755); err != nil { t.Fatal(err) }
 defer os.RemoveAll(ledgerPath)
 
 // Fresh instance: Load() will fail because the path is a directory, so we can't load the ledger.
 // Instead, test by directly manipulating the ledger to force a write error scenario.
 // Create a ledger that thinks it has the data but the path is now a directory.
 l2 := NewCohortLedger(dir, true, 2)
 // Don't call Load() since it will error. Manually set state.
 l2.CurrentWindowStart = time.Now().UTC()
 l2.AdmittedAccountIDs = []string{}
 l2.RotationStart = time.Now().UTC()
 
 // Try to admit: write should fail because path is a directory, not a file.
 if ok, _, err := l2.MayAdmit("2100001"); ok || err == nil { t.Fatalf("write should have failed with path-type error: admitted=%v error=%v", ok, err) }
}

func TestProductionBoundary_DeniedAdmissionsCannotConnect(t *testing.T) {
 // This test verifies that the production CheckCohortAdmission API
 // correctly enforces quota: exactly 100 new admissions, then 101st is denied.
 // This is not just a ledger test; it exercises the actual admission boundary
 // that bot.go will check before calling connect().
 
 dir := t.TempDir()
 
 // Initialize the global cohort gate for this test.
 if err := InitCohortGate(dir, true); err != nil {
  t.Fatalf("InitCohortGate failed: %v", err)
 }
 
 // Admit 100 accounts.
 for i := 1; i <= 100; i++ {
  aid := uint32(1000000 + i)
  admitted, err := CheckCohortAdmission(aid)
  if !admitted || err != nil {
   t.Fatalf("Account %d should be admitted (grant %d/100), got %v %v", aid, i, admitted, err)
  }
 }
 
 // 101st account should be denied.
 aidDenied := uint32(1000101)
 admitted, err := CheckCohortAdmission(aidDenied)
 if admitted || err != nil {
  t.Fatalf("Account %d should be DENIED (quota exhausted), got admitted=%v error=%v", aidDenied, admitted, err)
 }
}


