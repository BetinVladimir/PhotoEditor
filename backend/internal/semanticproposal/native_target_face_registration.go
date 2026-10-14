package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Only blocked native face input registration. The caller rolls back failures;
// source/dependency/report/Params/anchor locks remain held until its commit.
func (s SQLTransferSourceReader) registerNativeTargetFaceInputsInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, original detectorDraftPlan, plan nativeTargetFaceDraftPlan, color TransferColorProfile, detectorBuilt, built v2input.Built) error {
	if err := verifyNativeTargetFacePlanSeal(ctx, plan); err != nil {
		return err
	}
	prepared, err := plan.prepared.frozen.BindPublishedTransport(ctx, built)
	if err != nil {
		return err
	}
	binding, err := marshalNativeTargetFaceRegistrationBinding(ctx, plan, built)
	if err != nil {
		return err
	}
	if _, err := s.beginNativeTargetFaceDraftInTransaction(ctx, tx, who, original, plan, color, detectorBuilt); err != nil {
		return err
	}
	state := func() (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var out string
		err := tx.QueryRow(ctx, "SELECT photo.semantic_target_face_registration_binding_state($1,$2,$3,$4::jsonb)", plan.shell.Tenant, plan.shell.Run, plan.prepared.operation, string(binding)).Scan(&out)
		return out, err
	}
	current, err := state()
	if err != nil {
		return err
	}
	if current == "replayed" {
		return ctx.Err()
	}
	if current != "missing" {
		return v2input.ErrConflict
	}
	if err := v2input.RegisterInTransaction(ctx, tx, plan.shell.Tenant, plan.shell.Run, prepared.Graph.ID, built, prepared.Graph.Open, prepared.Graph.Direct); err != nil {
		return err
	}
	current, err = state()
	if err != nil {
		return err
	}
	if current != "replayed" {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
