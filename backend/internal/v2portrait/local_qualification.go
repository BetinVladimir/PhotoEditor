package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// LockLocalRuntimeQualification holds current policy/report/lifecycle locks
// through the caller transaction. It is not graph admission by itself.
func LockLocalRuntimeQualification(ctx context.Context, tx pgx.Tx, tenant, project uuid.UUID, operation, runtimeSHA string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenant == uuid.Nil || project == uuid.Nil || (operation != "portrait.hair" && operation != "portrait.clothing") {
		return v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(runtimeSHA); err != nil {
		return v2input.ErrInvalid
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.v28_local_runtime_qualification_ready($1,$2,$3,$4)", tenant, project, operation, runtimeSHA).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return ctx.Err()
}
