package interchange

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// ExportAdoptionCandidate is private server-side recovery data. The root is
// selected by the current canonical task result, not by a client payload.
type ExportAdoptionCandidate struct {
	Frozen FrozenExportDocuments
	Root   v2input.Ref
}

func RecoverExportAdoptionCandidate(ctx context.Context, db *pgxpool.Pool,
	who auth.Identity, tenantID uuid.UUID, request CreateRequest, key string,
) (ExportAdoptionCandidate, error) {
	var candidate ExportAdoptionCandidate
	if err := ctx.Err(); err != nil {
		return candidate, err
	}
	if db == nil {
		return candidate, v2input.ErrInvalid
	}
	intent, err := FreezeCreateIntent(tenantID, who, request, key)
	if err != nil {
		return candidate, err
	}
	if intent.Request.Direction != "export" {
		return candidate, v2input.ErrInvalid
	}
	err = tenant.WithTenant(ctx, db, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		pending, err := RecoverExportPending(ctx, tx, who, tenantID, intent.Request, key)
		if err != nil {
			return err
		}
		if !pending.HasPending {
			return domain.NewError("INTERCHANGE_RESULT_STALE", 409)
		}
		if err := tx.QueryRow(ctx, `SELECT artifact_id,sha256 FROM photo.v30_export_adoption_root($1,$2,$3,$4,$5)`,
			tenantID, who.UserID, who.SessionID, key, intent.RequestSHA).
			Scan(&candidate.Root.ArtifactID, &candidate.Root.SHA256); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewError("INTERCHANGE_RESULT_STALE", 409)
			}
			return err
		}
		if !validImportRef(candidate.Root) {
			return v2input.ErrInvalid
		}
		candidate.Frozen = pending.Frozen
		return ctx.Err()
	})
	if err != nil {
		return ExportAdoptionCandidate{}, err
	}
	return candidate, nil
}
