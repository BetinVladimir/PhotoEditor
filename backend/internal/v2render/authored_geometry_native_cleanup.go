package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// ProcessAuthoredNativeLifecycle uses the same scheduler tenant rotation. It
// never cleans a physical object or forces a live worker's lease to disappear.
func ProcessAuthoredNativeLifecycle(ctx context.Context, db *pgxpool.Pool, tenant uuid.UUID) (bool, error) {
	if e := ctx.Err(); e != nil {
		return false, e
	}
	if db == nil || tenant == uuid.Nil {
		return false, v2input.ErrInvalid
	}
	var changed bool
	e := database.TenantTransaction(ctx, db, tenant, func(tx pgx.Tx) error {
		var selected *uuid.UUID
		if e := tx.QueryRow(ctx, `SELECT photo.next_authored_native_cleanup($1)`, tenant).Scan(&selected); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return nil
			}
			return e
		}
		// SQL scalar NULL is decoded separately so absence is a normal idle poll.
		if selected == nil || *selected == uuid.Nil {
			return nil
		}
		child := *selected
		var cancelled, released bool
		if e := tx.QueryRow(ctx, `SELECT photo.cancel_authored_native_child($1,$2)`, tenant, child).Scan(&cancelled); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx, `SELECT photo.release_authored_native_quota($1,$2)`, tenant, child).Scan(&released); e != nil {
			return e
		}
		changed = cancelled || released
		return ctx.Err()
	})
	return changed, e
}
