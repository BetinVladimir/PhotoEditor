package v2optical

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// MultiTransactions opens the caller-owned scheduler transactions used by the
// panorama/focus coordinator. Qualification, source and receipt locks remain
// inside their existing transaction boundaries.
type MultiTransactions interface {
	Begin(context.Context) (pgx.Tx, error)
}
