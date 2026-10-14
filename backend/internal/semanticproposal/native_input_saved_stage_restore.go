package semanticproposal

import (
	"context"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func matchNativeInputSavedStagePlan(saved nativeInputSavedStage, shell v2input.DraftShell) error {
	if shell.Run != saved.run || shell.ConfigSHA != saved.configSHA || shell.Bytes != saved.recovery.Reserved || len(shell.Tasks) != 1 {
		return v2input.ErrConflict
	}
	sha, err := canonical.Hash(shell.Config)
	if err != nil || sha != saved.configSHA {
		return v2input.ErrConflict
	}
	task := shell.Tasks[0]
	if task.ID != saved.task || task.ParamsArtifactID != saved.params.ArtifactID || task.ParamsSHA != saved.params.SHA256 || task.InputDigest != saved.inputDigest {
		return v2input.ErrConflict
	}
	profile, err := canonical.Bytes(task.ExecutionProfile)
	if err != nil || string(profile) != saved.profile {
		return v2input.ErrConflict
	}
	return nil
}

// Loads original ready Params bytes, not merely their catalog byte count. The
// frozen compiler checks the exact expected Params SHA during reconstruction.
func (s SQLTransferSourceReader) readNativeSavedStageParams(ctx context.Context, who auth.Identity, request Request, operation string, ref v2input.Ref) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	document := uuid.NewSHA1(request.RunID, []byte("semantic."+operation+".params:reference"))
	object, err := s.defectVersionParamsObject(ctx, who, request.TenantID, request.Intent.Reference, document, ref)
	if err != nil {
		return nil, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, ref)
	if err != nil {
		return nil, err
	}
	after, err := s.defectVersionParamsObject(ctx, who, request.TenantID, request.Intent.Reference, document, ref)
	if err != nil {
		return nil, err
	}
	if object != after {
		return nil, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}

func (s SQLTransferSourceReader) restoreNativeFaceDraftPlan(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, detector detectorDraftPlan, color TransferColorProfile, operation string) (nativeFaceDraftPlan, error) {
	var zero nativeFaceDraftPlan
	if err := verifyDetectorPlanSeal(ctx, detector); err != nil {
		return zero, err
	}
	if catalog == nil || (operation != "face.landmarks" && operation != "face.parse") {
		return zero, v2input.ErrInvalid
	}
	saved, err := s.readNativeInputSavedStage(ctx, who, detector.request, detector.published.Ref, detector.anchor.snapshotRef, operation)
	if err != nil {
		return zero, err
	}
	raw, err := s.readNativeSavedStageParams(ctx, who, detector.request, operation, saved.params)
	if err != nil {
		return zero, err
	}
	completed, err := s.readCompletedDetectorPreparation(ctx, who, detector, color)
	if err != nil {
		return zero, err
	}
	params := nativeFaceStageParams{operation: operation, document: uuid.NewSHA1(detector.request.RunID, []byte("semantic."+operation+".params:reference")), ref: saved.params, detector: completed.catalog, sourceBody: completed.sourceBody, facesBody: completed.body, detectorParamsBody: completed.detectorParamsBody}
	m := saved.recovery
	if m.Report == nil {
		return zero, v2input.ErrInvalid
	}
	budget := nativePreparationBudget{Costs: m.Costs, RAM: m.RAM, VRAM: m.VRAM, Scratch: m.Scratch, Output: m.Output, Reserved: m.Reserved}
	prepared, err := s.prepareNativeFaceTask(ctx, catalog, who, detector, color, params, *m.Report, budget, m.FaceProfileVersion)
	if err != nil {
		return zero, err
	}
 var queue map[string]json.RawMessage
 if json.Unmarshal([]byte(saved.config),&queue)!=nil{return zero,v2input.ErrInvalid}
 rawQueue,present:=queue["queue_class"]
 if present&&string(rawQueue)!=`"batch"`{return zero,v2input.ErrInvalid}
 prepared.legacyQueueOmitted=!present
	plan, err := s.compileNativeFaceDraftPlan(ctx, who, detector, prepared, m.Reserved)
	if err != nil {
		return zero, err
	}
	if err := matchNativeInputSavedStagePlan(saved, plan.shell); err != nil {
		return zero, err
	}
	current, err := s.readNativeSavedStageParams(ctx, who, detector.request, operation, saved.params)
	if err != nil {
		return zero, err
	}
	after, err := s.readNativeInputSavedStage(ctx, who, detector.request, detector.published.Ref, detector.anchor.snapshotRef, operation)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(saved, after) || string(raw) != string(current) {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativePlanPolicy(ctx, who, detector.request.TenantID, detector.policy); err != nil {
		return zero, err
	}
	return plan, nil
}

func (s SQLTransferSourceReader) restoreNativeSkinDraftPlan(ctx context.Context, who auth.Identity, parser nativeFaceDraftPlan, color TransferColorProfile) (nativeSkinDraftPlan, error) {
	var zero nativeSkinDraftPlan
	if err := verifyNativeFacePlanSeal(ctx, parser); err != nil {
		return zero, err
	}
	if parser.prepared.operation != "face.parse" {
		return zero, v2input.ErrInvalid
	}
	detector := parser.detector
	saved, err := s.readNativeInputSavedStage(ctx, who, detector.request, detector.published.Ref, detector.anchor.snapshotRef, "mask.face_skin")
	if err != nil {
		return zero, err
	}
	raw, err := s.readNativeSavedStageParams(ctx, who, detector.request, "mask.face_skin", saved.params)
	if err != nil {
		return zero, err
	}
	completed, err := s.readCompletedNativeFaceParse(ctx, who, parser, color)
	if err != nil {
		return zero, err
	}
	params := nativeSkinStageParams{document: uuid.NewSHA1(detector.request.RunID, []byte("semantic.mask.face_skin.params:reference")), ref: saved.params, parser: completed}
	m := saved.recovery
	prepared, err := s.prepareNativeSkinTask(ctx, who, parser, color, params, m.RAM, m.Scratch, m.Output, m.SkinProfileVersion)
	if err != nil {
		return zero, err
	}
 var queue map[string]json.RawMessage
 if json.Unmarshal([]byte(saved.config),&queue)!=nil{return zero,v2input.ErrInvalid}
 rawQueue,present:=queue["queue_class"]
 if present&&string(rawQueue)!=`"batch"`{return zero,v2input.ErrInvalid}
 prepared.legacyQueueOmitted=!present
	plan, err := s.compileNativeSkinDraftPlan(ctx, who, parser, prepared, m.Reserved)
	if err != nil {
		return zero, err
	}
	if err := matchNativeInputSavedStagePlan(saved, plan.shell); err != nil {
		return zero, err
	}
	current, err := s.readNativeSavedStageParams(ctx, who, detector.request, "mask.face_skin", saved.params)
	if err != nil {
		return zero, err
	}
	after, err := s.readNativeInputSavedStage(ctx, who, detector.request, detector.published.Ref, detector.anchor.snapshotRef, "mask.face_skin")
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(saved, after) || string(raw) != string(current) {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativePlanPolicy(ctx, who, detector.request.TenantID, detector.policy); err != nil {
		return zero, err
	}
	return plan, nil
}
