package gateway

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
)

// Retry only transaction serialization failures from the access-check query.
// Every attempt uses a fresh statement and fresh result; no object IO is replayed.
func retryGatewayAuthorization(ctx context.Context, read func(context.Context) (TaskContext, error)) (TaskContext, error) {
	const attempts = 3
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return TaskContext{}, err
		}
		task, err := read(ctx)
		if cancelled := ctx.Err(); cancelled != nil {
			return TaskContext{}, cancelled
		}
		if err == nil {
			return task, nil
		}
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "40001" || attempt == attempts-1 {
			return TaskContext{}, err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return TaskContext{}, ctx.Err()
		case <-timer.C:
		}
	}
}
