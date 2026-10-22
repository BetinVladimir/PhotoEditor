package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

func (s SQLTransferSourceReader) nativeTargetFaceResultCatalog(ctx context.Context, who auth.Identity, plan nativeTargetFaceDraftPlan) (nativeFaceResultCatalog, error) {
	var zero nativeFaceResultCatalog
	if err := verifyNativeTargetFacePlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	rebuilt, err := s.compileNativeTargetFaceDraftPlan(ctx, who, plan.detector, plan.result, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if rebuilt.seal != plan.seal || s.DB == nil {
		return zero, v2input.ErrConflict
	}
	return s.nativeTargetFaceResultCatalogCurrent(ctx, who, plan)
}

func (s SQLTransferSourceReader) nativeTargetFaceResultCatalogCurrent(ctx context.Context, who auth.Identity, plan nativeTargetFaceDraftPlan) (nativeFaceResultCatalog, error) {
	var zero nativeFaceResultCatalog
	if err := verifyNativeTargetFacePlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, v2input.ErrInvalid
	}
	var out nativeFaceResultCatalog
	err := tenant.WithTenant(ctx, s.DB, who, plan.shell.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var raw []byte
		if err := tx.QueryRow(ctx, "SELECT photo.semantic_target_face_canonical_outputs($1,$2,$3,$4,$5,$6)", plan.shell.Tenant, s.ProjectID, who.UserID, who.SessionID, plan.shell.Run, plan.shell.ConfigSHA).Scan(&raw); err != nil {
			return err
		}
		var saved struct {
			Result  uuid.UUID     `json:"result_id"`
			Outputs []v2input.Ref `json:"outputs"`
		}
		if len(raw) == 0 || len(raw) > 8192 || json.Unmarshal(raw, &saved) != nil {
			return v2input.ErrConflict
		}
		output, err := canonical.Marshal(saved.Outputs)
		if err != nil {
			return err
		}
		params := v2input.Ref{ArtifactID: plan.shell.Tasks[0].ParamsArtifactID, SHA256: plan.shell.Tasks[0].ParamsSHA}
		paramsBody, err := canonical.Marshal(params)
		if err != nil {
			return err
		}
		out, err = decodeNativeFaceResultCatalog(saved.Result, output, paramsBody, plan.prepared.operation, params, []v2input.Ref{plan.detector.anchor.entry.Binding.Source, plan.prepared.detector.Output, plan.prepared.detector.Params})
		return err
	})
	if err != nil {
		return zero, err
	}
	return out, ctx.Err()
}
