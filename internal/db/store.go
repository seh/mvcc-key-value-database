package db

import (
	"context"
	"errors"
	"fmt"
	"hash/maphash"
)

// A KeyShardProjection is a projection function from a given database key to an opaque value with
// which to assign the key to a storage shard.
type KeyShardProjection func(Key) uint64

type shardedStoreOptions struct {
	initialRecordMapCapacity int
	keyShardProjection       KeyShardProjection
}

// ShardedStoreOption is a potential customization of a ShardedStore's behavior.
type ShardedStoreOption func(*shardedStoreOptions) error

// WithInitialRecordMapCapacity establishes the positive number of records per shard for which to
// allocate sufficient capacity initially.
func WithInitialRecordMapCapacity(n int) ShardedStoreOption {
	return func(o *shardedStoreOptions) error {
		if n < 1 {
			return errors.New("initial record map capacity must be positive")
		}
		o.initialRecordMapCapacity = n
		return nil
	}
}

// WithKeyShardProjection establishes a projection function from a given database key to an opaque
// value with which to assign the key to a storage shard.
//
// The function must be deterministic, should produce an even distribution of output values for
// keys, and should complete quickly.
func WithKeyShardProjection(p KeyShardProjection) ShardedStoreOption {
	return func(o *shardedStoreOptions) error {
		if p == nil {
			return errors.New("key shard projection must be non-nil")
		}
		o.keyShardProjection = p
		return nil
	}
}

type recordMap struct {
	lock         rwMutex
	recordsByKey map[string]*versionedRecord
}

// TODO(seh): Consider accepting this as a parameter, though we then can't fix the array size, and
// must work with a slice.
const shardDegree = 512

// ShardedStore is a database that stores records in a set of maps relating each key to a history of
// versions. All reading and mutation of the database occurs within transactions that allow readers
// to observe a consistent snapshot while writers propose and commit transactions concurrently.
type ShardedStore struct {
	keyShardProjection KeyShardProjection
	txState            transactionState
	recordMaps         [shardDegree]recordMap
}

// MakeShardedStore creates an empty ShardedStore ready to accept records.
func MakeShardedStore(opts ...ShardedStoreOption) (*ShardedStore, error) {
	seed := maphash.MakeSeed()
	options := shardedStoreOptions{
		keyShardProjection: func(k Key) uint64 {
			// TODO(seh): Consider using MurmurHash2, or MurmurHash3 if we can use 128 bits.
			return maphash.Bytes(seed, k)
		},
		initialRecordMapCapacity: 50,
	}
	for _, o := range opts {
		if err := o(&options); err != nil {
			return nil, err
		}
	}
	s := ShardedStore{
		keyShardProjection: options.keyShardProjection,
	}
	for i := range s.recordMaps {
		s.recordMaps[i].lock = makeLock()
		s.recordMaps[i].recordsByKey = make(map[string]*versionedRecord, options.initialRecordMapCapacity)
	}
	return &s, nil
}

func (s *ShardedStore) recordMapFor(k Key) *recordMap {
	return &s.recordMaps[s.keyShardProjection(k)%shardDegree]
}

// shardedStoreTransaction represents the database starting at a point in time, isolated both from
// observing and interfering with operations in other transactions.
type shardedStoreTransaction struct {
	store         *ShardedStore
	id            transactionID
	pendingWrites map[string]struct{} // NB: Initilized lazily
}

func (t *shardedStoreTransaction) recordFor(ctx context.Context, k Key) (*recordMap, *versionedRecord, bool) {
	rm := t.store.recordMapFor(k)
	if !rm.lock.TryRLockUntil(ctx) {
		return nil, nil, false
	}
	record, ok := rm.recordsByKey[string(k)]
	rm.lock.RUnlock()
	return rm, record, ok
}

func (t *shardedStoreTransaction) notePendingWriteAgainst(k Key) {
	_, ok := t.pendingWrites[string(k)]
	if ok {
		return
	}
	if t.pendingWrites == nil {
		t.pendingWrites = make(map[string]struct{}, 3)
	}
	t.pendingWrites[string(k)] = struct{}{}
}

func (t *shardedStoreTransaction) hasPendingWriteAgainst(k Key) bool {
	_, ok := t.pendingWrites[string(k)]
	return ok
}

func (t *shardedStoreTransaction) Get(ctx context.Context, k Key) (Value, error) {
	rm, record, ok := t.recordFor(ctx, k)
	if rm == nil {
		return nil, ctx.Err()
	}
	if !ok {
		return nil, recordDoesNotExistError(k)
	}
	// Record already exists, even if it's only a tombstone.
walkBackwards:
	for r := record.newest.Load(); r != nil; r = r.next {
		switch validAsOf := r.validAsOfTransactionID(); {
		case validAsOf == noSuchTransaction:
			if !t.hasPendingWriteAgainst(k) {
				// A different transaction is trying to write to this record.
				continue
			}
			// We're trying to write to this same record.
			switch validBefore := r.validBeforeTransactionID(); {
			case validBefore == noSuchTransaction:
				// We're writing a new value, which we'll observe here.
				return r.value, nil
			case validBefore <= t.id:
				// We're deleting this record.
				break walkBackwards
			}
		case validAsOf <= t.id:
			if validBefore := r.validBeforeTransactionID(); validBefore == noSuchTransaction || validBefore > t.id {
				return r.value, nil
			}
			break walkBackwards
		}
	}
	return nil, recordDoesNotExistError(k)
}

func (t *shardedStoreTransaction) Insert(ctx context.Context, k Key, v Value) error {
	rm, record, ok := t.recordFor(ctx, k)
	if rm == nil {
		return ctx.Err()
	}
	useExistingRecord := func(record *versionedRecord) error {
		tryInsertPlaceholderVersion := func(expectedNewest *recordVersion) error {
			proposedVersion := recordVersion{
				next: expectedNewest,
			}
			proposedVersion.value.CopyFrom(v)
			if !record.newest.CompareAndSwap(expectedNewest, &proposedVersion) {
				// Someone else stored a new version before us.
				return transactionInConflictError(k)
			}
			t.notePendingWriteAgainst(k)
			return nil
		}
		var sawNewerVersion bool
		for r := record.newest.Load(); r != nil; r = r.next {
			switch validAsOf := r.validAsOfTransactionID(); {
			case validAsOf == noSuchTransaction:
				if !t.hasPendingWriteAgainst(k) {
					// A different transaction is trying to write to this record.
					return transactionInConflictError(k)
				}
				switch validBefore := r.validBeforeTransactionID(); {
				case validBefore == noSuchTransaction:
					// It looks like we already inserted this record during this transaction.
					return recordExistsError(k)
				case validBefore == t.id:
					// It looks like we deleted this record during this transaction.
					r.value.CopyFrom(v)
					r.validBeforeTransaction.Store(uint64(noSuchTransaction))
					return nil
				default:
					// For some reason, the pending record version has an unexpected validity horizon.
					return fmt.Errorf("transaction with ID %d found pending record version for %q with unexpected validity period ending with transaction %d", t.id, k, validBefore)
				}
			case validAsOf > t.id:
				// A later transaction changed this record. Still, we keep walking back to see if a
				// version that was active earlier (covering our transaction) would have indicated
				// that the record already existed. If there is such a version, but the record
				// didn't exist (because it had been deleted then), then we'd have to report a
				// conflict, because we can't write by leapfrogging a later transaction.
				sawNewerVersion = true
				continue
			default:
				switch validBefore := r.validBeforeTransactionID(); {
				case validBefore == noSuchTransaction:
					// This version is still valid.
					return recordExistsError(k)
				case validBefore <= t.id:
					// Someone else deleted the record by marking it as a tombstone.
					if sawNewerVersion {
						// Even though the record didn't exist at this version, we can't proceed
						// with inserting it because newer versions already exist.
						return transactionInConflictError(k)
					}
					return tryInsertPlaceholderVersion(r)
				default:
					// This version was deleted in later transaction, so from our perspective it
					// still exists.
					return recordExistsError(k)
				}
			}
		}
		if sawNewerVersion {
			return transactionInConflictError(k)
		}
		// Try to insert a placeholder record, but only if there are no other versions available now.
		return tryInsertPlaceholderVersion(nil)
	}
	if ok {
		// Fast path: record already exists, even if it's only a tombstone.
		return useExistingRecord(record)
	}
	// Slow path: record does not exist.
	if !rm.lock.TryLockUntil(ctx) {
		return ctx.Err()
	}
	// It's possible that someone else got in and added this record already.
	if record, ok := rm.recordsByKey[string(k)]; ok {
		rm.lock.Unlock()
		return useExistingRecord(record)
	}
	var proposedVersion recordVersion
	proposedVersion.value.CopyFrom(v)
	var proposedRecord versionedRecord
	proposedRecord.newest.Store(&proposedVersion)
	rm.recordsByKey[string(k)] = &proposedRecord
	rm.lock.Unlock()
	t.notePendingWriteAgainst(k)
	return nil
}

func (t *shardedStoreTransaction) Update(ctx context.Context, k Key, v Value) error {
	rm, record, ok := t.recordFor(ctx, k)
	if rm == nil {
		return ctx.Err()
	}
	if !ok {
		return recordDoesNotExistError(k)
	}
	r := record.newest.Load()
	if r == nil {
		return recordDoesNotExistError(k)
	}
	switch validAsOf := r.validAsOfTransactionID(); {
	case validAsOf == noSuchTransaction:
		if !t.hasPendingWriteAgainst(k) {
			// A different transaction is trying to write to this record.
			return transactionInConflictError(k)
		}
		switch validBefore := r.validBeforeTransactionID(); {
		case validBefore == noSuchTransaction:
			// Update the previously proposed value in place.
			r.value.CopyFrom(v)
			return nil
		case validBefore <= t.id:
			// Someone else already deleted the record by marking it as a tombstone.
			return recordDoesNotExistError(k)
		default:
			// For some reason, the pending record version would be valid for ours and maybe
			// even for later transactions, even though our transaction is supposedly
			// working on this record. Preclude further interference by giving up.
			return fmt.Errorf("transaction with ID %d found pending record version for %q with later validity period ending with transaction %d", t.id, k, validBefore)
		}
	case validAsOf <= t.id:
		proposeUpdate := func() bool {
			proposedNewest := recordVersion{
				next: r,
			}
			proposedNewest.value.CopyFrom(v)
			if record.newest.CompareAndSwap(r, &proposedNewest) {
				t.notePendingWriteAgainst(k)
				return true
			}
			return false
		}
		for {
			switch validBefore := r.validBeforeTransactionID(); {
			case validBefore == noSuchTransaction:
				if proposeUpdate() {
					return nil
				}
				// Someone else added a newer version.
				return transactionInConflictError(k)
			case validBefore <= t.id:
				// Someone else deleted the record by marking it as a tombstone.
				return recordDoesNotExistError(k)
			default:
				// A later transaction deleted or invalidated this version. Since it's possible
				// that intervening transactions have observed this version being valid and made
				// decisions based upon that finding, we can't just pull back the validity
				// horizon here.
				return transactionInConflictError(k)
			}
		}
	default:
		// NB: We don't walk backward through versions to try to find one that covers our
		// transaction. If we do, and we find one, we allow an update when subsequent
		// transactions have changed this record, violating the "snapshot" isolation protocol.
		return transactionInConflictError(k)
	}
}

func (t *shardedStoreTransaction) Upsert(ctx context.Context, k Key, v Value) error {
	// TODO(seh): The proper implementation requires a blend between the Insert and Update
	// methods. Perhaps try first to update, but if the record does not exist yet, try to insert it.
	for {
		err := t.Update(ctx, k, v)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrRecordDoesNotExist) {
			err = t.Insert(ctx, k, v)
			if err == nil {
				return nil
			}
			if errors.Is(err, ErrRecordExists) {
				continue
			}
		}
		return err
	}
}

func (t *shardedStoreTransaction) Delete(ctx context.Context, k Key) (error, bool) {
	rm, record, ok := t.recordFor(ctx, k)
	if rm == nil {
		return ctx.Err(), false
	}
	if !ok {
		return nil, false
	}
	r := record.newest.Load()
	if r == nil {
		return nil, false
	}
	switch validAsOf := r.validAsOfTransactionID(); {
	case validAsOf == noSuchTransaction:
		if !t.hasPendingWriteAgainst(k) {
			// A different transaction is trying to write to this record.
			return transactionInConflictError(k), false
		}
		for {
			switch validBefore := r.validBeforeTransactionID(); {
			case validBefore == noSuchTransaction:
				if r.validBeforeTransaction.CompareAndSwap(uint64(noSuchTransaction), uint64(t.id)) {
					return nil, true
				}
				// Someone else changed the validity horizon. We'll try again.
			case validBefore <= t.id:
				// Someone else already deleted the record by marking it as a tombstone.
				return nil, false
			default:
				// For some reason, the pending record version would be valid for ours and maybe
				// even for later transactions, even though our transaction is supposedly
				// working on this record. Preclude further interference by giving up.
				return fmt.Errorf("transaction with ID %d found pending record version for %q with later validity period ending with transaction %d", t.id, k, validBefore), false
			}
		}
	case validAsOf <= t.id:
		for {
			switch validBefore := r.validBeforeTransactionID(); {
			case validBefore == noSuchTransaction:
				// We can't modify this active version in place: if we were to roll back the
				// transaction, we'd need to undo this, and we don't want other transactions
				// reading this record to observe this deletion yet. Insert a placeholder
				// version here instead that we'll resolve later when committing.
				proposedNewest := recordVersion{
					value: r.value,
					next:  r,
				}
				proposedNewest.validBeforeTransaction.Store(uint64(t.id))
				if record.newest.CompareAndSwap(r, &proposedNewest) {
					t.notePendingWriteAgainst(k)
					return nil, true
				}
				// Someone else added a newer version.
				return transactionInConflictError(k), false
			case validBefore <= t.id:
				// Someone else already deleted the record by marking it as a tombstone.
				return nil, false
			default:
				// A later transaction deleted or invalidated this version. Since it's possible
				// that intervening transactions have observed this version being valid and made
				// decisions based upon that finding, we can't just pull back the validity
				// horizon here.
				return transactionInConflictError(k), false
			}
		}
	default:
		// A later transaction changed this record, but we should not inspect the record's state
		// further here.
		return transactionInConflictError(k), false
	}
}

// Transaction allows observing and mutating the database tentatively, such that it's possible to
// roll back or preclude committing pending mutations.
type Transaction interface {
	// Get retrieves an existing record from the database for the given key, if any such record
	// exists.
	//
	// If the database does not contain a record with the given key. Get returns
	// ErrRecordDoesNotExist.
	Get(ctx context.Context, k Key) (Value, error)
	// Insert adds a new record to the database for the given key, storing the given value.
	//
	// If the database already contains a record for the given key, Insert returns ErrRecordExists.
	Insert(ctx context.Context, k Key, v Value) error
	// Update modifies an existing record in the database with the given key to store the given
	// value.
	//
	// If the database does not contain a record with the given key. Update returns
	// ErrRecordDoesNotExist.
	Update(ctx context.Context, k Key, v Value) error
	// Upsert ensures that a record exists in the database for the given key storing the given
	// value.
	//
	// If no record for the given key already exists, Upsert behaves like Insert. Conversely, if a
	// record for the given key already exists, Upsert behaves like Update.
	Upsert(ctx context.Context, k Key, v Value) error
	// Delete ensures that no record exists in the database for the given key, removing an existing
	// record if need be.
	//
	// Delete returns true if it removed an existing record, or false if either no such record
	// existed or an error arose.
	Delete(ctx context.Context, k Key) (error, bool)
}

var _ Transaction = (*shardedStoreTransaction)(nil)

func (s *ShardedStore) WithinTransaction(ctx context.Context, f func(context.Context, Transaction) (commit bool, err error)) error {
	if f == nil {
		return errors.New("transaction-consuming function must be non-nil")
	}
	tx := shardedStoreTransaction{
		store: s,
		id:    s.txState.claimNext(),
	}
	defer s.txState.recordFinished(tx.id)
	// TODO(seh): Consider recovering from panics here and rolling back the transaction.
	commit, err := f(ctx, &tx)
	// In order to avoid leaving the database in an inconsistent state, we don't want to give up
	// this effort due to the governing Context having been canceled.
	ctxFinalize := context.Background()
	if commit {
		for key := range tx.pendingWrites {
			_, record, ok := tx.recordFor(ctxFinalize, Key(key))
			if !ok {
				continue
			}
			for newest := record.newest.Load(); newest != nil &&
				newest.validAsOfTransactionID() == noSuchTransaction; newest = record.newest.Load() {
				prev := newest.next
				// If the newest record version has its "before transaction" value set indicating
				// deletion, attempt to collapse it into the previous record version by copying down
				// the "before transaction value".
				if prev != nil && prev.validBeforeTransaction.CompareAndSwap(uint64(noSuchTransaction), uint64(tx.id)) {
					if newest.validBeforeTransactionID() != noSuchTransaction &&
						record.newest.CompareAndSwap(newest, prev) {
						break
					}
				}
				if newest.validAsOfTransaction.CompareAndSwap(uint64(noSuchTransaction), uint64(tx.id)) {
					break
				}
			}
		}
	} else {
		for key := range tx.pendingWrites {
			_, record, ok := tx.recordFor(ctxFinalize, Key(key))
			if !ok {
				continue
			}
			for newest := record.newest.Load(); newest != nil && newest.validAsOfTransactionID() == noSuchTransaction; newest = record.newest.Load() {
				// No other writers should be contending with us here, but defend against the
				// possibility until we're more sure that this won't occur.
				if record.newest.CompareAndSwap(newest, newest.next) {
					break
				}
			}
		}
	}
	return err
}

// TODO(seh): Implement "vacuum" garbage collector procedure, running either periodically or upon
// detecting that the record and version count has passed some threshold. This may require another
// bookkeeping value on the recordMap struct.
