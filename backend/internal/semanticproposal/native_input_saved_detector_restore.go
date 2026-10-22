package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Reconstructs the original SFD compiler plan without publication or admission.
// The saved report and resource inputs are physically requalified; saved config
// and task profile must exactly match the resulting current compiler output.
func (s SQLTransferSourceReader) restoreDetectorDraftPlan(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody []byte, color TransferColorProfile) (detectorDraftPlan, error) {
	var zero detectorDraftPlan
	owned, err := ownNativePlanRequest(ctx, who, request, key)
	if err != nil {
		return zero, err
	}
	if catalog == nil {
		return zero, v2input.ErrInvalid
	}
	saved, err := s.readNativeInputSavedStage(ctx, who, owned, published.Ref, snapshot, "face.detect")
	if err != nil {
		return zero, err
	}
	policy, err := s.readQualificationPolicySnapshot(ctx, who, owned.TenantID)
	if err != nil {
		return zero, err
	}
	selected, err := selectNativeQualificationPolicies(policy, []string{"face.detect"})
	if err != nil {
		return zero, err
	}
	expected := selected["face.detect"]
	raw, err := s.readNativeSavedStageParams(ctx, who, owned, "face.detect", saved.params)
	if err != nil {
		return zero, err
	}
	var params review.FaceDetectParams
	if json.Unmarshal(raw, &params) != nil {
		return zero, v2input.ErrInvalid
	}
	body, err := s.Read(ctx, who, owned.TenantID, owned.Intent.Reference)
	if err != nil {
		return zero, err
	}
	compiled, err := v2input.PrepareSemanticFaceDetectParams(ctx, owned.Intent.Reference, body, expected.ModelLockSHA, params.ScoreThreshold, params.MaxFaces)
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(raw, compiled.Params) {
		return zero, v2input.ErrConflict
	}
	m := saved.recovery
	s.scenePolicy = m.ScenePolicy
	if m.Report == nil || (m.RequestKey != "" && m.RequestKey != key) {
		return zero, v2input.ErrInvalid
	}
	document := uuid.NewSHA1(owned.RunID, []byte("semantic.face.detect.params:reference"))
	prepared, err := s.prepareSourceDetectorTask(ctx, catalog, who, owned.TenantID, owned.RunID, 0, owned.Intent.Reference, color, document, saved.params, *m.Report, expected, params.ScoreThreshold, params.MaxFaces, m.Costs, m.RAM, m.VRAM, m.Scratch, m.Output, m.FaceProfileVersion)
	if err != nil {
		return zero, err
	}
	var queue struct {
		QueueClass string `json:"queue_class,omitempty"`
	}
	if json.Unmarshal([]byte(saved.config), &queue) != nil {
		return zero, v2input.ErrInvalid
	}
	plan, err := s.compileDetectorDraftPlanForQueue(ctx, who, owned, key, published, snapshot, snapshotBody, prepared, policy, m.Reserved, m.RequestKey != "", queue.QueueClass)
	if err != nil {
		return zero, err
	}
	if err := matchNativeInputSavedStagePlan(saved, plan.shell); err != nil {
		return zero, err
	}
	current, err := s.readNativeSavedStageParams(ctx, who, owned, "face.detect", saved.params)
	if err != nil {
		return zero, err
	}
	after, err := s.readNativeInputSavedStage(ctx, who, owned, published.Ref, snapshot, "face.detect")
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(saved, after) || !bytes.Equal(raw, current) {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativePlanPolicy(ctx, who, owned.TenantID, policy); err != nil {
		return zero, err
	}
	return plan, nil
}

type nativeInputRestoredFaceProducers struct {
	detector          detectorDraftPlan
	landmarks, parser nativeFaceDraftPlan
	skin              nativeSkinDraftPlan
}

// Full original producer plans for subsequent transport/dependency recovery.
// No transport, result, protected mask or transfer authority is synthesized.
func (s SQLTransferSourceReader) restoreNativeInputFaceProducers(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody []byte, color TransferColorProfile) (nativeInputRestoredFaceProducers, error) {
	var zero nativeInputRestoredFaceProducers
	detector, err := s.restoreDetectorDraftPlan(ctx, catalog, who, request, key, published, snapshot, snapshotBody, color)
	if err != nil {
		return zero, err
	}
	landmarks, err := s.restoreNativeFaceDraftPlan(ctx, catalog, who, detector, color, "face.landmarks")
	if err != nil {
		return zero, err
	}
	parser, err := s.restoreNativeFaceDraftPlan(ctx, catalog, who, detector, color, "face.parse")
	if err != nil {
		return zero, err
	}
	skin, err := s.restoreNativeSkinDraftPlan(ctx, who, parser, color)
	if err != nil {
		return zero, err
	}
	// Later producer IO must not leave earlier saved configurations live.
	for _, stage := range []struct {
		operation string
		shell     v2input.DraftShell
	}{{"face.detect", detector.shell}, {"face.landmarks", landmarks.shell}, {"face.parse", parser.shell}, {"mask.face_skin", skin.shell}} {
		saved, err := s.readNativeInputSavedStage(ctx, who, detector.request, published.Ref, snapshot, stage.operation)
		if err != nil {
			return zero, err
		}
		if err := matchNativeInputSavedStagePlan(saved, stage.shell); err != nil {
			return zero, err
		}
	}
	if err := s.recheckNativePlanPolicy(ctx, who, detector.request.TenantID, detector.policy); err != nil {
		return zero, err
	}
	return nativeInputRestoredFaceProducers{detector: detector, landmarks: landmarks, parser: parser, skin: skin}, nil
}
