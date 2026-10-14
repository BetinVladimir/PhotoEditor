package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type nativeInputDefectResultCatalog struct {
	ResultID uuid.UUID
	Outputs  []v2input.Ref
	Params   v2input.Ref
}

// Lookup never substitutes source-wide latest output or delegates admission.
func (s SQLTransferSourceReader) nativeInputDefectResultCatalog(ctx context.Context, who auth.Identity, plan nativeDefectDraftPlan) (nativeInputDefectResultCatalog, error) {
	var zero nativeInputDefectResultCatalog
	if err := verifyNativeDefectDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := s.compileNativeDefectDraftPlan(ctx, who, plan.prepared, plan.shell.Bytes); err != nil {
		return zero, err
	}
	task, err := plan.prepared.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	var result nativeInputDefectResultCatalog
	err = tenant.WithTenant(ctx, s.DB, who, plan.prepared.dependencies.preparation.landmarksPlan.detector.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var outputs, params []byte
		var id uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT result_id,outputs,params FROM photo.semantic_native_defect_result_catalog($1,$2,$3,$4,$5,$6,$7)", plan.prepared.dependencies.preparation.landmarksPlan.detector.request.TenantID, s.ProjectID, who.UserID, who.SessionID, plan.prepared.dependencies.preparation.landmarksPlan.detector.request.RunID, plan.prepared.dependencies.preparation.landmarksPlan.detector.request.RequestSHA, task.ID).Scan(&id, &outputs, &params); err != nil {
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
		if decoded.ResultID == plan.prepared.dependencies.preparation.landmarks.catalog.ResultID || decoded.ResultID == plan.prepared.dependencies.preparation.skin.catalog.ResultID {
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

func decodeNativeInputDefectResultCatalog(id uuid.UUID, outputs, params []byte, operation string, expectedParams v2input.Ref, forbidden []v2input.Ref) (nativeInputDefectResultCatalog, error) {
	var zero nativeInputDefectResultCatalog
	count := 0
	switch operation {
	case "defects.propose":
		count = 5
	default:
		return zero, v2input.ErrInvalid
	}
	decode := func(raw []byte, dst any) bool {
		if len(raw) < 2 || len(raw) > 8192 {
			return false
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		return decoder.Decode(dst) == nil && decoder.Decode(new(any)) == io.EOF
	}
	result := nativeInputDefectResultCatalog{ResultID: id}
	if id == uuid.Nil || !decode(outputs, &result.Outputs) || len(result.Outputs) != count || !decode(params, &result.Params) || result.Params != expectedParams {
		return zero, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	refs := append(append([]v2input.Ref(nil), forbidden...), result.Params)
	refs = append(refs, result.Outputs...)
	for _, ref := range refs {
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return zero, v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	return result, nil
}
