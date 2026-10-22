package retryscope

import (
	"context"
	"errors"
	"time"
)

type Transaction interface {
	Commit(context.Context) error
	Rollback(context.Context) error
}

// CheckedClaim keeps SQL claim mutations uncommitted until the operator scope
// validates the returned lease. Deferred rollback covers nil/error/foreign and
// uncertain commit; only a validated lease may reach Commit.
func CheckedClaim[T any](ctx context.Context, begin func(context.Context) (Transaction, error), claim func(Transaction) (*T, error), valid func(*T) bool) (*T, error) {
	tx, e := begin(ctx)
	if e != nil {
		return nil, e
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	lease, e := claim(tx)
	if e != nil || lease == nil {
		return nil, e
	}
	if !valid(lease) {
		return nil, errors.New("claim outside exact reviewed scope")
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return lease, nil
}
