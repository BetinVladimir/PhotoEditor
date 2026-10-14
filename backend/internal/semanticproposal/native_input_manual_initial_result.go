package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Original manual-stage result lookup only. This is a catalog locator, not
// physical result verification or downstream protected-source authority.
func (s SQLTransferSourceReader) nativeInputManualInitialResultCatalog(ctx context.Context, who auth.Identity, plan nativeInputManualInitialDraftPlan) (nativeInputDefectResultCatalog, error) {
	var zero nativeInputDefectResultCatalog
	if err := verifyNativeInputManualInitialDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, v2input.ErrInvalid
	}
	owned, err := s.compileNativeInputManualInitialDraft(ctx, who, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if owned.seal != plan.seal || !reflect.DeepEqual(owned.shell, plan.shell) {
		return zero, v2input.ErrConflict
	}
	task, err := plan.prepared.compiled.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	var result nativeInputDefectResultCatalog
	err = tenant.WithTenant(ctx, s.DB, who, plan.prepared.stage.dependencies.dependencies.producers.detector.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var outputs, params []byte
		var id uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT result_id,outputs,params FROM photo.semantic_native_defect_result_catalog($1,$2,$3,$4,$5,$6,$7)", plan.prepared.stage.dependencies.dependencies.producers.detector.request.TenantID, s.ProjectID, who.UserID, who.SessionID, plan.prepared.stage.dependencies.dependencies.producers.detector.request.RunID, plan.prepared.stage.dependencies.dependencies.producers.detector.request.RequestSHA, task.ID).Scan(&id, &outputs, &params); err != nil {
			return err
		}
		var entries []v2input.Entry
		if json.Unmarshal([]byte(plan.prepared.compiled.entries), &entries) != nil {
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
		if decoded.ResultID == plan.prepared.stage.dependencies.dependencies.selection.landmarks.catalog.ResultID || decoded.ResultID == plan.prepared.stage.dependencies.dependencies.selection.skin.catalog.ResultID {
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
