package main

import (
	"context"

	"seh/db/internal/db"
)

type database interface {
	// Insert adds a new record to the database for the given key, storing the given value.
	//
	// If the database already contains a record for the given key, Insert returns ErrRecordExists.
	Insert(ctx context.Context, k db.Key, v db.Value) error
	// Update modifies an existing record in the database with the given key to store the given
	// value.
	//
	// If the database does not contain a record with the given key. Update returns
	// ErrRecordDoesNotExist.
	Update(ctx context.Context, k db.Key, v db.Value) error
	// Upsert ensures that a record exists in the database for the given key storing the given
	// value. If no record for the given key already exists, Upsert behaves like Insert. Conversely,
	// if a record for the given key already exists, Upsert behaves like Update.
	Upsert(ctx context.Context, k db.Key, v db.Value) error
	// Delete ensures that no record exists in the database for the given key, removing an existing
	// record if need be.
	//
	// Delete returns true if it removed an existing record, or false if no such record existed.
	Delete(ctx context.Context, k db.Key) (error, bool)

	// TODO(seh): Update a set of objects.
}
