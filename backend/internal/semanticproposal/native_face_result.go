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

type nativeFaceResultCatalog struct {
	ResultID uuid.UUID
	Outputs  []v2input.Ref
	Params   v2input.Ref
}

// Lookup never substitutes source-wide latest output or delegates admission.
func (s SQLTransferSourceReader) nativeFaceResultCatalog(ctx context.Context, who auth.Identity, plan nativeFaceDraftPlan) (nativeFaceResultCatalog, error) {
	if _, err := s.compileNativeFaceDraftPlan(ctx, who, plan.detector, plan.prepared, plan.shell.Bytes); err != nil {
		return nativeFaceResultCatalog{}, err
	}
	return s.nativeFaceResultCatalogCurrent(ctx, who, plan)
}

func (s SQLTransferSourceReader) nativeFaceResultCatalogCurrent(ctx context.Context, who auth.Identity, plan nativeFaceDraftPlan) (nativeFaceResultCatalog, error) {
	var zero nativeFaceResultCatalog
	if err := verifyNativeFacePlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, v2input.ErrInvalid
	}
	task, err := plan.prepared.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	var result nativeFaceResultCatalog
	err = tenant.WithTenant(ctx, s.DB, who, plan.detector.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var outputs, params []byte
		var id uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT result_id,outputs,params FROM photo.semantic_native_face_result_catalog($1,$2,$3,$4,$5,$6,$7)", plan.detector.request.TenantID, s.ProjectID, who.UserID, who.SessionID, plan.detector.request.RunID, plan.detector.request.RequestSHA, task.ID).Scan(&id, &outputs, &params); err != nil {
			return err
		}
		decoded, err := decodeNativeFaceResultCatalog(id, outputs, params, plan.prepared.operation, v2input.Ref{ArtifactID: task.ParamsArtifactID, SHA256: task.ParamsSHA}, []v2input.Ref{plan.detector.request.Intent.Reference.Source, plan.prepared.detector.Output, plan.prepared.detector.Params})
		if err != nil {
			return err
		}
		if decoded.ResultID == plan.prepared.detector.ResultID {
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

func decodeNativeFaceResultCatalog(id uuid.UUID, outputs, params []byte, operation string, expectedParams v2input.Ref, forbidden []v2input.Ref) (nativeFaceResultCatalog, error) {
	var zero nativeFaceResultCatalog
	count := 0
	switch operation {
	case "face.landmarks":
		count = 1
	case "face.parse":
		count = 2
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
	result := nativeFaceResultCatalog{ResultID: id}
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
