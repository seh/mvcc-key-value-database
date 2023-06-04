package db

import "sync/atomic"

type recordVersion struct {
	value                  Value
	next                   *recordVersion
	validAsOfTransaction   atomic.Uint64
	validBeforeTransaction atomic.Uint64
	// TODO(seh): Do we need to indicate whether this version is still formative, being worked on by
	// a writer in a transaction.
}

func (v *recordVersion) validAsOfTransactionID() transactionID {
	return transactionID(v.validAsOfTransaction.Load())
}

func (v *recordVersion) validBeforeTransactionID() transactionID {
	return transactionID(v.validBeforeTransaction.Load())
}

type versionedRecord struct {
	newest atomic.Pointer[recordVersion]
	// TODO(seh): What else do we need here?
}
