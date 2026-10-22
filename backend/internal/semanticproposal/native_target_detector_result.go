package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type nativeTargetDetectorResult struct {
	planSeal  string
	completed completedNativeDetector
	seal      string
}

func targetDetectorResultDigest(p nativeTargetDetectorResult) (string, error) {
	return canonical.Hash(map[string]any{"plan": p.planSeal, "catalog": p.completed.catalog, "body": p.completed.body, "source": p.completed.sourceBody, "params": p.completed.detectorParamsBody, "boxes": p.completed.boxes, "objects": []any{map[string]any{"ref": p.completed.objects[0].ref, "object": p.completed.objects[0].object}, map[string]any{"ref": p.completed.objects[1].ref, "object": p.completed.objects[1].object}}})
}
func (s SQLTransferSourceReader) nativeTargetDetectorResultCatalog(ctx context.Context, who auth.Identity, plan nativeTargetDetectorPlan) (nativeDetectorResultCatalog, error) {
	var zero nativeDetectorResultCatalog
	if err := verifyNativeTargetDetectorPlan(ctx, plan); err != nil {
		return zero, err
	}
	rebuilt, err := s.compileNativeTargetDetectorPlan(ctx, who, plan.anchor, plan.detector.prepared, plan.detector.policy, plan.detector.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if rebuilt.seal != plan.seal || s.DB == nil {
		return zero, v2input.ErrConflict
	}
	return s.nativeTargetDetectorResultCatalogCurrent(ctx, who, plan)
}

func (s SQLTransferSourceReader) nativeTargetDetectorResultCatalogCurrent(ctx context.Context, who auth.Identity, plan nativeTargetDetectorPlan) (nativeDetectorResultCatalog, error) {
	var zero nativeDetectorResultCatalog
	if err := verifyNativeTargetDetectorPlan(ctx, plan); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, v2input.ErrInvalid
	}
	var out nativeDetectorResultCatalog
	err := tenant.WithTenant(ctx, s.DB, who, plan.anchor.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var raw []byte
		if err := tx.QueryRow(ctx, "SELECT photo.semantic_target_detector_canonical_outputs($1,$2,$3,$4,$5,$6)", plan.anchor.request.TenantID, s.ProjectID, who.UserID, who.SessionID, plan.detector.shell.Run, plan.detector.shell.ConfigSHA).Scan(&raw); err != nil {
			return err
		}
		var saved struct {
			Result  uuid.UUID     `json:"result_id"`
			Outputs []v2input.Ref `json:"outputs"`
		}
		if len(raw) == 0 || len(raw) > 8192 || json.Unmarshal(raw, &saved) != nil || len(saved.Outputs) != 1 {
			return v2input.ErrConflict
		}
		output, err := canonical.Marshal(saved.Outputs[0])
		if err != nil {
			return err
		}
		params := v2input.Ref{ArtifactID: plan.detector.shell.Tasks[0].ParamsArtifactID, SHA256: plan.detector.shell.Tasks[0].ParamsSHA}
		paramsBody, err := canonical.Marshal(params)
		if err != nil {
			return err
		}
		out, err = decodeNativeDetectorResultCatalog(saved.Result, output, paramsBody, plan.anchor.entry.Binding.Source, params)
		return err
	})
	if err != nil {
		return zero, err
	}
	return out, ctx.Err()
}
func (s SQLTransferSourceReader) readNativeTargetDetectorResult(ctx context.Context, who auth.Identity, plan nativeTargetDetectorPlan, color TransferColorProfile) (nativeTargetDetectorResult, error) {
	var zero nativeTargetDetectorResult
	completed, err := s.readCompletedNativeTargetDetector(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	out := nativeTargetDetectorResult{planSeal: plan.seal, completed: completed}
	out.seal, err = targetDetectorResultDigest(out)
	if err != nil {
		return zero, err
	}
	return out, ctx.Err()
}
func (s SQLTransferSourceReader) lockNativeTargetDetectorResult(ctx context.Context, tx pgx.Tx, who auth.Identity, d detectorDraftPlan, plan nativeTargetDetectorPlan, built v2input.Built, p nativeTargetDetectorResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	digest, err := targetDetectorResultDigest(p)
	if err != nil {
		return err
	}
	if tx == nil || p.seal == "" || digest != p.seal || p.planSeal != plan.seal || p.completed.catalog.ResultID == uuid.Nil {
		return v2input.ErrConflict
	}
	packet, err := s.marshalNativeTargetDetectorAdmissionEvidence(ctx, who, d, plan, built)
	if err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", plan.anchor.request.TenantID.String()); err != nil {
		return err
	}
	outputs, err := canonical.Marshal([]v2input.Ref{p.completed.catalog.Output})
	if err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_target_detector_result($1,$2,$3::jsonb,$4,$5::jsonb)", plan.anchor.request.TenantID, plan.detector.shell.Run, string(packet), p.completed.catalog.ResultID, string(outputs)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
func (s SQLTransferSourceReader) readCompletedNativeTargetDetectorUncached(ctx context.Context, who auth.Identity, plan nativeTargetDetectorPlan, color TransferColorProfile) (completedNativeDetector, error) {
	var zero completedNativeDetector
	if err := verifyNativeTargetDetectorPlan(ctx, plan); err != nil {
		return zero, err
	}
	selected, err := selectNativeQualificationPolicies(plan.detector.policy, []string{"face.detect"})
	if err != nil {
		return zero, err
	}
	expected := selected["face.detect"]
	if err := s.recheckNativePlanPolicy(ctx, who, plan.detector.request.TenantID, plan.detector.policy); err != nil {
		return zero, err
	}
	if err := s.recheckDetectorQualificationProof(ctx, who, plan.detector.request.TenantID, plan.detector.prepared.qualification, expected); err != nil {
		return zero, err
	}
	before, err := s.nativeTargetDetectorResultCatalog(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	binding := plan.anchor.entry.Binding
	_, sourceProof, err := s.readQualifiedSourcePreparationProof(ctx, who, plan.detector.request.TenantID, binding, color, true)
	if err != nil {
		return zero, err
	}
	if sourceProof != plan.detector.prepared.source {
		return zero, v2input.ErrConflict
	}
	objects, err := s.facePreparationObjects(ctx, who, plan.detector.request.TenantID, binding, before.Output, "face.detect")
	if err != nil {
		return zero, err
	}
	if objects[1].ref != before.Params {
		return zero, v2input.ErrConflict
	}
	sourceBody, err := s.Read(ctx, who, plan.detector.request.TenantID, binding)
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
	current, err := s.nativeTargetDetectorResultCatalogCurrent(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	if current != before {
		return zero, v2input.ErrConflict
	}
	currentObjects, err := s.facePreparationObjects(ctx, who, plan.detector.request.TenantID, binding, before.Output, "face.detect")
	if err != nil {
		return zero, err
	}
	if currentObjects != objects {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckSourcePreparationProof(ctx, who, plan.detector.request.TenantID, binding, color, true, sourceProof); err != nil {
		return zero, err
	}
	if err := s.recheckDetectorQualificationProof(ctx, who, plan.detector.request.TenantID, plan.detector.prepared.qualification, expected); err != nil {
		return zero, err
	}
	if err := s.recheckNativePlanPolicy(ctx, who, plan.detector.request.TenantID, plan.detector.policy); err != nil {
		return zero, err
	}
	return completedNativeDetector{catalog: before, boxes: boxes, body: string(body), sourceBody: string(sourceBody), detectorParamsBody: string(params), objects: objects}, nil
}

// Finalize only this canonical producer. The SQL transition releases residual
// run quota and emits completion once; parent/next-stage admission is separate.
func (s SQLTransferSourceReader) finishNativeTargetDetectorInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, d detectorDraftPlan, plan nativeTargetDetectorPlan, built v2input.Built, proof nativeTargetDetectorResult) error {
	if err := s.lockNativeTargetDetectorResult(ctx, tx, who, d, plan, built, proof); err != nil {
		return err
	}
	packet, err := s.marshalNativeTargetDetectorAdmissionEvidence(ctx, who, d, plan, built)
	if err != nil {
		return err
	}
	outputs, err := canonical.Marshal([]v2input.Ref{proof.completed.catalog.Output})
	if err != nil {
		return err
	}
	var finished bool
	if err := tx.QueryRow(ctx, "SELECT photo.finish_semantic_target_detector($1,$2,$3::jsonb,$4,$5::jsonb)", plan.anchor.request.TenantID, plan.detector.shell.Run, string(packet), proof.completed.catalog.ResultID, string(outputs)).Scan(&finished); err != nil {
		return err
	}
	if !finished {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
