package db

import (
	"errors"
	"fmt"
)

// ErrRecordExists is the error returned for attempts to insert a new record into the database
// when a record the given key already exists. This may be wrapped in another error, and should
// normally be tested using errors.Is(err, ErrRecordExists).
var ErrRecordExists = errors.New("record exists")

// ErrRecordDoesNotExist is the error returned for attempts to update a record in the database
// when no such record for the given key exists. This may be wrapped in another error, and should
// normally be tested using errors.Is(err, ErrRecordDoesNotExist).
var ErrRecordDoesNotExist = errors.New("record does not exist")

type errRecordExists string

func (e errRecordExists) Error() string {
	return fmt.Sprintf("record with key %q exists", string(e))
}

func (e errRecordExists) Is(err error) bool {
	if err == ErrRecordExists {
		return true
	}
	ere, ok := err.(*errRecordExists)
	return ok && *ere == e
}

type errRecordDoesNotExist string

func (e errRecordDoesNotExist) Error() string {
	return fmt.Sprintf("record with key %q does not exist", string(e))
}

func (e errRecordDoesNotExist) Is(err error) bool {
	if err == ErrRecordDoesNotExist {
		return true
	}
	ere, ok := err.(*errRecordDoesNotExist)
	return ok && *ere == e
}
