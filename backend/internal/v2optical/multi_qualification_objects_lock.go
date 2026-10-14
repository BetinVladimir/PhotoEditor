package v2optical

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"time"
)

// LockLocatedMultiQualification retains policy and exact locator locks until
// caller transaction completion. It performs no object IO or commit.
func LockLocatedMultiQualification(ctx context.Context, tx pgx.Tx, who auth.Identity,
	tenant, project uuid.UUID, value LocatedMultiQualification) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	verified, err := value.Qualification()
	if err != nil {
		return err
	}
	if err := LockMultiQualification(ctx, tx, who, tenant, project, verified.pin, verified.operation); err != nil {
		return err
	}
	body, err := verified.pin.Compile()
	if err != nil {
		return err
	}
	objects, err := canonical.Marshal(value.objects)
	if err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_v29_multi_qualification_objects($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7)",
		tenant, project, who.UserID, who.SessionID, string(body), string(objects), verified.operation).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return ctx.Err()
}
