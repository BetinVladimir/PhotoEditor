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

type nativeSkinResultCatalog struct {
	ResultID uuid.UUID
	Outputs  []v2input.Ref
	Params   v2input.Ref
}

// Lookup never substitutes source-wide latest output or delegates admission.
func (s SQLTransferSourceReader) nativeSkinResultCatalog(ctx context.Context, who auth.Identity, plan nativeSkinDraftPlan) (nativeSkinResultCatalog, error) {
	if _, err := s.compileNativeSkinDraftPlan(ctx, who, plan.parser, plan.prepared, plan.shell.Bytes); err != nil {
		return nativeSkinResultCatalog{}, err
	}
	return s.nativeSkinResultCatalogCurrent(ctx, who, plan)
}

func (s SQLTransferSourceReader) nativeSkinResultCatalogCurrent(ctx context.Context, who auth.Identity, plan nativeSkinDraftPlan) (nativeSkinResultCatalog, error) {
	var zero nativeSkinResultCatalog
	if err := verifyNativeSkinPlanSeal(ctx, plan); err != nil {
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
	err = tenant.WithTenant(ctx, s.DB, who, plan.parser.detector.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var outputs, params []byte
		var id uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT result_id,outputs,params FROM photo.semantic_native_skin_result_catalog($1,$2,$3,$4,$5,$6,$7)", plan.parser.detector.request.TenantID, s.ProjectID, who.UserID, who.SessionID, plan.parser.detector.request.RunID, plan.parser.detector.request.RequestSHA, task.ID).Scan(&id, &outputs, &params); err != nil {
			return err
		}
		decoded, err := decodeNativeSkinResultCatalog(id, outputs, params, "mask.face_skin", v2input.Ref{ArtifactID: task.ParamsArtifactID, SHA256: task.ParamsSHA}, []v2input.Ref{plan.parser.detector.request.Intent.Reference.Source, plan.prepared.params.parser.catalog.Outputs[0], plan.prepared.params.parser.catalog.Outputs[1], plan.prepared.params.parser.catalog.Params, plan.prepared.params.parser.detector.catalog.Output, plan.prepared.params.parser.detector.catalog.Params})
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

func decodeNativeSkinResultCatalog(id uuid.UUID, outputs, params []byte, operation string, expectedParams v2input.Ref, forbidden []v2input.Ref) (nativeSkinResultCatalog, error) {
	var zero nativeSkinResultCatalog
	count := 0
	switch operation {
	case "mask.face_skin":
		count = 3
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
	result := nativeSkinResultCatalog{ResultID: id}
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
