package main

import (
	"fmt"
)

// Cohort admission gate: limits new bot logins to 100 per hour.
// This is the in-memory gate that uses the persistent CohortLedger.

var cohortGate *CohortLedger

// InitCohortGate initializes the admission gate.
// Called during engine startup.
func InitCohortGate(dataDir string, enabled bool) error {
	cohortGate = NewCohortLedger(dataDir, enabled, 100)
	return cohortGate.Load()
}

// CheckCohortAdmission checks if a bot's account can log in under the hourly cohort gate.
// Returns:
//   - (true, nil) if admitted (either already in quota or reconnecting)
//   - (false, error) if denied for quota exhaustion or other error
// The caller must already hold a slot from onlineSlots before calling this.
func CheckCohortAdmission(accountID uint32) (admitted bool, err error) {
	if cohortGate == nil {
		return true, nil // gate not initialized (backward compat)
	}

	accIDStr := formatAccountID(accountID)
	ok, _, err := cohortGate.MayAdmit(accIDStr)
	return ok, err
}

// formatAccountID converts a numeric account ID to string for ledger lookup.
func formatAccountID(accountID uint32) string {
	return fmt.Sprintf("%d", accountID)
}

// CohortStats returns observable statistics: admitted count and next eligible time.
func CohortStats() (grantedThisHour int, nextEligible string) {
	if cohortGate == nil {
		return 0, ""
	}
	granted, nextElig := cohortGate.Stats()
	return granted, nextElig.Format("2006-01-02T15:04:05Z")
}
