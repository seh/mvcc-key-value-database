package db

import (
	"errors"
	"fmt"
)

// ErrRecordExists is the error returned for attempts to insert a new record into the database
// when a record the given key already exists. This may be wrapped in another error, and should
// normally be tested using errors.Is(err, ErrRecordExists).
var ErrRecordExists = errors.New("record exists")

type recordExistsError string

func (e recordExistsError) Error() string {
	return fmt.Sprintf("record with key %q exists", string(e))
}

func (e recordExistsError) Is(err error) bool {
	if err == ErrRecordExists {
		return true
	}
	downcasted, ok := err.(*recordExistsError)
	return ok && *downcasted == e
}

// ErrRecordDoesNotExist is the error returned for attempts to update a record in the database
// when no such record for the given key exists. This may be wrapped in another error, and should
// normally be tested using errors.Is(err, ErrRecordDoesNotExist).
var ErrRecordDoesNotExist = errors.New("record does not exist")

type recordDoesNotExistError string

func (e recordDoesNotExistError) Error() string {
	return fmt.Sprintf("record with key %q does not exist", string(e))
}

func (e recordDoesNotExistError) Is(err error) bool {
	if err == ErrRecordDoesNotExist {
		return true
	}
	downcasted, ok := err.(*recordDoesNotExistError)
	return ok && *downcasted == e
}

// ErrTransactionInConflict is the error returned for attempts to insert, update, or delete a record
// in the database when another transaction is still attempting to mutate the same record for the
// given key. This may be wrapped in another error, and should normally be tested using
// errors.Is(err, ErrTransactionInConflict).
var ErrTransactionInConflict = errors.New("write attempt conflicts with another transaction")

type transactionInConflictError string

func (e transactionInConflictError) Error() string {
	return fmt.Sprintf("attempt to write record with key %q conflicts with another transaction", string(e))
}

func (e transactionInConflictError) Is(err error) bool {
	if err == ErrTransactionInConflict {
		return true
	}
	downcasted, ok := err.(*transactionInConflictError)
	return ok && *downcasted == e
}
