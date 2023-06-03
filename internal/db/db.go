package db

import "context"

type (
	// Key is TODO(seh).
	Key []byte
	// Value is TODO(seh).
	Value []byte
)

// Handle allows manipulating records in the database.
type Handle struct {
	// TODO(seh): Populate this.
}

// Insert adds a new record to the database for the given key, storing the given value.
//
// If the database already contains a record for the given key, Insert returns ErrRecordExists.
func (db *Handle) Insert(ctx context.Context, k Key, v Value) error {
	return nil
}

// Update modifies an existing record in the database with the given key to store the given value.
//
// If the database does not contain a record with the given key. Update returns
// ErrRecordDoesNotExist.
func (db *Handle) Update(ctx context.Context, k Key, v Value) error {
	return nil
}

// Upsert ensures that a record exists in the database for the given key storing the given value.
//
// If no record for the given key already exists, Upsert behaves like Insert. Conversely, if a
// record for the given key already exists, Upsert behaves like Update.
func (db *Handle) Upsert(ctx context.Context, k Key, v Value) error {
	return nil
}

// Delete ensures that no record exists in the database for the given key, removing an existing
// record if need be.
//
// Delete returns true if it removed an existing record, or false if no such record existed.
func (db *Handle) Delete(ctx context.Context, k Key) (error, bool) {
	return nil, false
}

// TODO(seh): Update a set of objects.
