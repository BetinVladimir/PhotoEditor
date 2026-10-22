package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Lookup never substitutes source-wide latest output or delegates admission.
func (s SQLTransferSourceReader) nativeTargetSkinResultCatalog(ctx context.Context, who auth.Identity, plan nativeTargetSkinDraftPlan) (nativeSkinResultCatalog, error) {
	var zero nativeSkinResultCatalog
	if err := verifyNativeTargetSkinPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := s.compileNativeTargetSkinDraftPlan(ctx, who, plan.parser, plan.prepared, plan.shell.Bytes); err != nil {
		return zero, err
	}
	return s.nativeTargetSkinResultCatalogCurrent(ctx, who, plan)
}
func (s SQLTransferSourceReader) nativeTargetSkinResultCatalogCurrent(ctx context.Context, who auth.Identity, plan nativeTargetSkinDraftPlan) (nativeSkinResultCatalog, error) {
	var zero nativeSkinResultCatalog
	if err := verifyNativeTargetSkinPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, v2input.ErrInvalid
	}
	task, err := plan.prepared.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	var result nativeSkinResultCatalog
	err = tenant.WithTenant(ctx, s.DB, who, plan.parser.detector.detector.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var outputs, params []byte
		var id uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT result_id,outputs,params FROM photo.semantic_target_skin_result_catalog($1,$2,$3,$4,$5,$6,$7)", plan.parser.detector.detector.request.TenantID, s.ProjectID, who.UserID, who.SessionID, plan.parser.detector.detector.request.RunID, plan.parser.detector.detector.request.RequestSHA, task.ID).Scan(&id, &outputs, &params); err != nil {
			return err
		}
		decoded, err := decodeNativeSkinResultCatalog(id, outputs, params, "mask.face_skin", v2input.Ref{ArtifactID: task.ParamsArtifactID, SHA256: task.ParamsSHA}, []v2input.Ref{plan.parser.detector.anchor.entry.Binding.Source, plan.prepared.params.parser.catalog.Outputs[0], plan.prepared.params.parser.catalog.Outputs[1], plan.prepared.params.parser.catalog.Params, plan.prepared.params.parser.detector.completed.catalog.Output, plan.prepared.params.parser.detector.completed.catalog.Params})
		if err != nil {
			return err
		}
		if decoded.ResultID == plan.prepared.params.parser.catalog.ResultID {
			return v2input.ErrInvalid
		}
		result = decoded
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrConflict
	}
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return result, nil
}
