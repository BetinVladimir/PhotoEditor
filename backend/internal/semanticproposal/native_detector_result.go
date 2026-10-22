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
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type nativeDetectorResultCatalog struct {
	ResultID       uuid.UUID
	Output, Params v2input.Ref
}

// Catalog identity is tied to the retained parent plan, not a caller-selected
// source-wide latest detector. This read is preflight, never admission authority.
func (s SQLTransferSourceReader) nativeDetectorResultCatalog(ctx context.Context, who auth.Identity, plan detectorDraftPlan) (nativeDetectorResultCatalog, error) {
	if _, err := s.recompileDetectorDraftPlan(ctx, who, plan); err != nil {
		return nativeDetectorResultCatalog{}, err
	}
	return s.nativeDetectorResultCatalogCurrent(ctx, who, plan)
}

func (s SQLTransferSourceReader) nativeDetectorResultCatalogCurrent(ctx context.Context, who auth.Identity, plan detectorDraftPlan) (nativeDetectorResultCatalog, error) {
	var zero nativeDetectorResultCatalog
	if err := verifyDetectorPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, v2input.ErrInvalid
	}
	task, err := plan.prepared.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	var result nativeDetectorResultCatalog
	err = tenant.WithTenant(ctx, s.DB, who, plan.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var output, params []byte
		var id uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT result_id,output,params FROM photo.semantic_native_detector_result_catalog($1,$2,$3,$4,$5,$6,$7)", plan.request.TenantID, s.ProjectID, who.UserID, who.SessionID, plan.request.RunID, plan.request.RequestSHA, task.ID).Scan(&id, &output, &params); err != nil {
			return err
		}
		decoded, err := decodeNativeDetectorResultCatalog(id, output, params, plan.request.Intent.Reference.Source, v2input.Ref{ArtifactID: task.ParamsArtifactID, SHA256: task.ParamsSHA})
		if err != nil {
			return err
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

func decodeNativeDetectorResultCatalog(id uuid.UUID, output, params []byte, source, expectedParams v2input.Ref) (nativeDetectorResultCatalog, error) {
	var zero nativeDetectorResultCatalog
	decode := func(raw []byte, ref *v2input.Ref) bool {
		if len(raw) < 2 || len(raw) > 4096 || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
			return false
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		return decoder.Decode(ref) == nil && decoder.Decode(new(any)) == io.EOF
	}
	result := nativeDetectorResultCatalog{ResultID: id}
	if id == uuid.Nil || !decode(output, &result.Output) || !decode(params, &result.Params) || result.Params != expectedParams {
		return zero, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range []v2input.Ref{source, result.Output, result.Params} {
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

type completedNativeDetector struct {
	catalog nativeDetectorResultCatalog
	boxes   review.FaceBoxes
	// Exact original control bytes can feed downstream frozen compilers. They
	// remain private and must be revalidated/locked when admitting those stages.
	body                           string
	sourceBody, detectorParamsBody string
	objects                        [2]defectEvidenceObject
}

func (s SQLTransferSourceReader) readCompletedDetectorPreparationUncached(ctx context.Context, who auth.Identity, plan detectorDraftPlan, color TransferColorProfile) (completedNativeDetector, error) {
	var zero completedNativeDetector
	if err := verifyDetectorPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	selected, err := selectNativeQualificationPolicies(plan.policy, []string{"face.detect"})
	if err != nil {
		return zero, err
	}
	expected := selected["face.detect"]
	if err := s.recheckNativePlanPolicy(ctx, who, plan.request.TenantID, plan.policy); err != nil {
		return zero, err
	}
	if err := s.recheckDetectorQualificationProof(ctx, who, plan.request.TenantID, plan.prepared.qualification, expected); err != nil {
		return zero, err
	}
	before, err := s.nativeDetectorResultCatalog(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	binding := plan.request.Intent.Reference
	_, sourceProof, err := s.readQualifiedSourcePreparationProof(ctx, who, plan.request.TenantID, binding, color, true)
	if err != nil {
		return zero, err
	}
	if sourceProof != plan.prepared.source {
		return zero, v2input.ErrConflict
	}
	objects, err := s.facePreparationObjects(ctx, who, plan.request.TenantID, binding, before.Output, "face.detect")
	if err != nil {
		return zero, err
	}
	if objects[1].ref != before.Params {
		return zero, v2input.ErrConflict
	}
	sourceBody, err := s.Read(ctx, who, plan.request.TenantID, binding)
	if err != nil {
		return zero, err
	}
	image, err := review.ParseWorkingImage(sourceBody)
	if err != nil {
		return zero, err
	}
	source := image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256})
	body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objects[0].object, objects[0].ref)
	if err != nil {
		return zero, err
	}
	params, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objects[1].object, objects[1].ref)
	if err != nil {
		return zero, err
	}
	if err := review.VerifyFaceDetectParams(review.ArtifactRef{ID: before.Params.ArtifactID, SHA256: before.Params.SHA256}, params, body, source); err != nil {
		return zero, err
	}
	boxes, err := review.ParseFaceBoxes(body, source)
	if err != nil {
		return zero, err
	}
	if boxes.Backend != expected.Backend || boxes.ModelLockID != expected.ModelLockSHA {
		return zero, v2input.ErrInvalid
	}
	current, err := s.nativeDetectorResultCatalogCurrent(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	if current != before {
		return zero, v2input.ErrConflict
	}
	currentObjects, err := s.facePreparationObjects(ctx, who, plan.request.TenantID, binding, before.Output, "face.detect")
	if err != nil {
		return zero, err
	}
	if currentObjects != objects {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckSourcePreparationProof(ctx, who, plan.request.TenantID, binding, color, true, sourceProof); err != nil {
		return zero, err
	}
	if err := s.recheckDetectorQualificationProof(ctx, who, plan.request.TenantID, plan.prepared.qualification, expected); err != nil {
		return zero, err
	}
	if err := s.recheckNativePlanPolicy(ctx, who, plan.request.TenantID, plan.policy); err != nil {
		return zero, err
	}
	return completedNativeDetector{catalog: before, boxes: boxes, body: string(body), sourceBody: string(sourceBody), detectorParamsBody: string(params), objects: objects}, nil
}
