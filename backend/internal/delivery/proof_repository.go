package delivery

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

type ProofTransaction interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
type PrepareProofScope struct {
	Tenant, Project, Run, Task uuid.UUID
	Params, Frozen             v2input.Ref
}

// SavePrepareProof belongs inside the caller's authorized photo_api admission
// transaction after immutable params publication, before graph sealing. It does
// not start/commit a separate transaction or create a task/workflow implicitly.
func SavePrepareProof(ctx context.Context, tx ProofTransaction, who auth.Identity, scope PrepareProofScope, frozenRaw, snapshotRaw []byte, configs []PresetContent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || scope.Tenant == uuid.Nil || scope.Project == uuid.Nil || scope.Run == uuid.Nil || scope.Task == uuid.Nil || scope.Params.ArtifactID == uuid.Nil {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	_, proof, params, err := CompilePrepareProof(scope.Project, who.SessionID, scope.Frozen, frozenRaw, snapshotRaw, configs)
	if err != nil {
		return err
	}
	if digest(params) != scope.Params.SHA256 {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	var accepted bool
	err = tx.QueryRow(ctx, `SELECT photo.save_v30_delivery_prepare_proof($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb)`,
		scope.Tenant, who.UserID, who.SessionID, scope.Project, scope.Run, scope.Task, scope.Params.ArtifactID, scope.Params.SHA256, string(params), string(proof)).Scan(&accepted)
	if err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_SOURCE_STALE", 409)
	}
	return nil
}
