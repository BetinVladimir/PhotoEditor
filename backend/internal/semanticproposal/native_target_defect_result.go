package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Lookup never substitutes source-wide latest output or delegates admission.
func (s SQLTransferSourceReader) nativeTargetDefectResultCatalog(ctx context.Context, who auth.Identity, plan nativeTargetDefectDraftPlan) (nativeInputDefectResultCatalog, error) {
	var zero nativeInputDefectResultCatalog
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := s.compileNativeTargetDefectDraftPlan(ctx, who, plan.prepared, plan.shell.Bytes); err != nil {
		return zero, err
	}
	task, err := plan.prepared.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	var result nativeInputDefectResultCatalog
	err = tenant.WithTenant(ctx, s.DB, who, plan.anchor.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var outputs, params []byte
		var id uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT result_id,outputs,params FROM photo.semantic_target_defect_result_catalog($1,$2,$3,$4,$5,$6,$7)", plan.anchor.request.TenantID, s.ProjectID, who.UserID, who.SessionID, plan.anchor.request.RunID, plan.anchor.request.RequestSHA, task.ID).Scan(&id, &outputs, &params); err != nil {
			return err
		}
		var entries []v2input.Entry
		if json.Unmarshal([]byte(plan.prepared.entries), &entries) != nil {
			return v2input.ErrInvalid
		}
		forbidden := make([]v2input.Ref, 0, len(entries))
		for _, entry := range entries {
			if entry.Ref.ArtifactID != task.ParamsArtifactID {
				forbidden = append(forbidden, entry.Ref)
			}
		}
		decoded, err := decodeNativeInputDefectResultCatalog(id, outputs, params, "defects.propose", v2input.Ref{ArtifactID: task.ParamsArtifactID, SHA256: task.ParamsSHA}, forbidden)
		if err != nil {
			return err
		}
		if decoded.ResultID == plan.prepared.preparation.landmarks.catalog.ResultID || decoded.ResultID == plan.prepared.preparation.skin.catalog.ResultID {
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
