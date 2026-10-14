package v2draft

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

// CheckIdentityLiveQualificationInTx returns the current trusted dataset pin.
// The caller must verify exact Calibration bytes against that dataset before
// committing, in this same scheduler transaction.
func CheckIdentityLiveQualificationInTx(ctx context.Context, tx pgx.Tx,
	evidence PreparedIdentityAdoption) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	params := evidence.Inputs.Params
	if tx == nil || evidence.Scope.Tenant == uuid.Nil ||
		evidence.Scope.Task == uuid.Nil || evidence.Scope.Result == uuid.Nil ||
		!isDocumentRef(evidence.Inputs.ParamsRef) ||
		!isDocumentRef(params.Calibration) ||
		!isDocumentRef(v2input.Ref{ArtifactID: params.Calibration.ArtifactID,
			SHA256: params.ModelLockSHA}) {
		return "", v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)",
		evidence.Scope.Tenant.String()); err != nil {
		return "", err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return "", err
	}
	if role != "photo_scheduler" {
		return "", v2input.ErrInvalid
	}
	var dataset *string
	err := tx.QueryRow(ctx, `SELECT photo.v23_identity_live_qualification($1,$2,$3,$4,$5,$6,$7,$8)`,
		evidence.Scope.Tenant, evidence.Scope.Task, evidence.Scope.Result,
		evidence.Inputs.ParamsRef.ArtifactID, evidence.Inputs.ParamsRef.SHA256,
		params.ModelLockSHA, params.Calibration.ArtifactID,
		params.Calibration.SHA256).Scan(&dataset)
	if err != nil {
		return "", err
	}
	if dataset == nil || !isDocumentRef(v2input.Ref{ArtifactID: params.Calibration.ArtifactID,
		SHA256: *dataset}) {
		return "", v2input.ErrInvalid
	}
	return *dataset, ctx.Err()
}
