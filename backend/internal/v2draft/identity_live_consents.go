package v2draft

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

// CheckIdentityLiveConsentsInTx holds the tenant and task locks through the
// caller's commit. The same pgx transaction must perform final adoption.
func CheckIdentityLiveConsentsInTx(ctx context.Context, tx pgx.Tx,
	evidence PreparedIdentityAdoption) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || evidence.Scope.Tenant == uuid.Nil ||
		evidence.Scope.Task == uuid.Nil || evidence.Scope.Result == uuid.Nil ||
		!isDocumentRef(evidence.Consents.Ref) ||
		evidence.Consents.Ref != evidence.Inputs.Params.Consents ||
		evidence.Inputs.Params.Before.AssetVersionID == uuid.Nil ||
		evidence.Inputs.Params.After.AssetVersionID == uuid.Nil ||
		len(evidence.Consents.Raw) < 2 ||
		v2input.ValidateSessionConsentSnapshot(evidence.Consents.Raw) != nil {
		return v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)",
		evidence.Scope.Tenant.String()); err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var live bool
	err := tx.QueryRow(ctx, `SELECT photo.v23_identity_live_consents($1,$2,$3,$4,$5,$6,$7,$8)`,
		evidence.Scope.Tenant, evidence.Scope.Task, evidence.Scope.Result,
		evidence.Consents.Ref.ArtifactID, evidence.Consents.Ref.SHA256,
		string(evidence.Consents.Raw),
		evidence.Inputs.Params.Before.AssetVersionID,
		evidence.Inputs.Params.After.AssetVersionID).Scan(&live)
	if err != nil {
		return err
	}
	if !live {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}
