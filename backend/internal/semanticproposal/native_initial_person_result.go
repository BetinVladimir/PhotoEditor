package semanticproposal

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// The original saved person plan supplies qualification and task identity.
// Source-wide candidate discovery never substitutes another parent's producer.
// No parent acceptance or queue authority is returned by this physical reader.
func (s SQLTransferSourceReader) readOriginalNativePersonInput(ctx context.Context, who auth.Identity, plan personDraftPlan) (nativePersonInputAuthority, error) {
	var zero nativePersonInputAuthority
	if err := verifyPersonPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, v2input.ErrInvalid
	}
	task, err := plan.prepared.catalog.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	proof, err := s.readNativePersonInputAuthority(ctx, who, plan.request, plan.prepared.qualification.report.ref)
	if err != nil {
		return zero, err
	}
	if proof.policy.body != plan.policy.body || proof.expected != plan.prepared.qualification.expected {
		return zero, v2input.ErrConflict
	}
	original := func(p nativePersonInputAuthority) error {
		return tenant.WithTenant(ctx, s.DB, who, plan.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
			var ready bool
			err := tx.QueryRow(ctx, "SELECT photo.semantic_native_original_person_result($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", plan.request.TenantID, s.ProjectID, who.UserID, who.SessionID, plan.request.RunID, plan.request.RequestSHA, task.ID, p.root.ArtifactID, p.root.SHA256, p.pixels.ArtifactID, p.pixels.SHA256).Scan(&ready)
			if err != nil {
				return err
			}
			if !ready {
				return v2input.ErrConflict
			}
			return nil
		})
	}
	if err := original(proof); err != nil {
		return zero, err
	}
	current, err := s.readNativePersonInputAuthority(ctx, who, plan.request, plan.prepared.qualification.report.ref)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(proof, current) {
		return zero, v2input.ErrConflict
	}
	if err := original(current); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return proof, nil
}
