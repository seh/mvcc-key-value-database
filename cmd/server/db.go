package main

import (
	"context"

	"sehlabs.com/db/internal/db"
)

type database interface {
	WithinTransaction(context.Context, func(context.Context, db.Transaction) (commit bool, err error)) error
}
