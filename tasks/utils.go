package cstasks

import (
	"time"
)

const (
	// The fraction of the timeout period to trigger minipool stakes
	stakeTimeoutSafetyFactor time.Duration = 2
)

// True if a transaction is due and needs to bypass the gas threshold
func isTransactionDue(startTime time.Time, minipoolLaunchTimeout time.Duration) (bool, time.Duration) {
	dueTime := minipoolLaunchTimeout / stakeTimeoutSafetyFactor
	isDue := time.Since(startTime) > dueTime
	timeUntilDue := time.Until(startTime.Add(dueTime))
	return isDue, timeUntilDue
}
