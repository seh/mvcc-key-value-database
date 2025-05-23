package db

import (
	"sync/atomic"
)

type transactionID uint64

const (
	// NB: The first valid transaction ID is one.
	noSuchTransaction    transactionID = 0
	guardAgainstOverflow               = true
)

type transactionState struct {
	latestID         atomic.Uint64
	oldestFinishedID atomic.Uint64
}

func (s *transactionState) claimNext() transactionID {
	next := transactionID(s.latestID.Add(1))
	if guardAgainstOverflow && next == noSuchTransaction {
		// TODO(seh): Consider a better way to handle this situation.
		panic("database transaction ID sequence overflowed")
	}
	return next
}

func (s *transactionState) recordFinished(id transactionID) bool {
	if id == noSuchTransaction {
		return false
	}
	for {
		// TODO(seh): With this inequality, we'll wind up getting "stuck" here, where no
		// newer/greater IDs can advance this value. We can more easily track the newest finished
		// ID, but it's not clear yet whether that's what we'll need to determine which record
		// versions are safe for vacuuming.
		if oldest := s.oldestFinishedID.Load(); transactionID(oldest) < id {
			if s.oldestFinishedID.CompareAndSwap(oldest, uint64(id)) {
				return true
			}
		} else {
			return false
		}
	}
}
