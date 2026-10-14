package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
	"time"
)

// One pinned session lock spans preparation/recovery without an open SQL
// transaction. Unknown acquisition/unlock outcomes never reenter the pool.
func withNativeManualParentLock(ctx context.Context, pool *pgxpool.Pool, tenant, batch uuid.UUID, fn func(context.Context) error) (done bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if pool == nil || pool.Config().MaxConns < 2 || tenant == uuid.Nil || batch == uuid.Nil || fn == nil {
		return false, v2input.ErrInvalid
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	// Namespace collisions can only serialize unrelated work, never grant scope.
	key := "v27-native-parent-recovery:" + tenant.String() + ":" + batch.String()
	var locked bool
	if err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1,0))", key).Scan(&locked); err != nil {
		// An unknown lock outcome must never return this session to the pool.
		isolated := conn.Hijack()
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = isolated.Close(cleanup)
		return false, err
	}
	if !locked {
		conn.Release()
		return false, nil
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		var released bool
		cleanupErr := conn.QueryRow(cleanup, "SELECT pg_advisory_unlock(hashtextextended($1,0))", key).Scan(&released)
		if cleanupErr != nil || !released {
			if cleanupErr == nil {
				cleanupErr = errors.New("native parent recovery lock release failed")
			}
			isolated := conn.Hijack()
			_ = isolated.Close(cleanup)
			err = errors.Join(err, cleanupErr)
			done = false
		} else {
			conn.Release()
		}
	}()
	if err = fn(ctx); err != nil {
		return false, err
	}
	return true, nil
}
