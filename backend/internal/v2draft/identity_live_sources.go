package v2draft

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

// CheckIdentityLiveSourcesInTx binds both exact WorkingImages to current
// project asset versions while holding tenant/source locks until caller commit.
// It must run in the same scheduler transaction as final adoption.
func CheckIdentityLiveSourcesInTx(ctx context.Context, tx pgx.Tx,
	evidence PreparedIdentityAdoption) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	params := evidence.Inputs.Params
	docs := evidence.Documents
	if tx == nil || evidence.Scope.Tenant == uuid.Nil ||
		evidence.Scope.Task == uuid.Nil || evidence.Scope.Result == uuid.Nil ||
		params.Before.AssetVersionID == uuid.Nil || params.After.AssetVersionID == uuid.Nil ||
		params.Before.AssetVersionID == params.After.AssetVersionID ||
		!isDocumentRef(docs.BeforeRef) || !isDocumentRef(docs.AfterRef) ||
		docs.BeforeRef != params.Before.Source || docs.AfterRef != params.After.Source ||
		docs.BeforeRef == docs.AfterRef || docs.Objects[0].Key == "" ||
		docs.Objects[1].Key == "" || docs.Objects[0].VersionID == "" ||
		docs.Objects[1].VersionID == "" || docs.Objects[0].VersionID == "null" ||
		docs.Objects[1].VersionID == "null" ||
		docs.Objects[0].SHA256 != docs.BeforeRef.SHA256 ||
		docs.Objects[1].SHA256 != docs.AfterRef.SHA256 {
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
	err := tx.QueryRow(ctx, `SELECT photo.v23_identity_live_sources($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		evidence.Scope.Tenant, evidence.Scope.Task, evidence.Scope.Result,
		params.Before.AssetVersionID, docs.BeforeRef.ArtifactID, docs.BeforeRef.SHA256,
		docs.Objects[0].Key, docs.Objects[0].VersionID,
		params.After.AssetVersionID, docs.AfterRef.ArtifactID, docs.AfterRef.SHA256,
		docs.Objects[1].Key, docs.Objects[1].VersionID).Scan(&live)
	if err != nil {
		return err
	}
	if !live {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}
