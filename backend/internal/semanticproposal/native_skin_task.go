package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Preparation owns exact catalog sizes and byte-verified parser evidence. A
// frozen task is not permission to register, admit or execute it.
type nativeSkinTaskPreparation struct {
 legacyQueueOmitted bool
	frozen                                  v2input.FrozenSemanticFaceSkinTask
	params                                  nativeSkinStageParams
	labels                                  defectEvidenceObject
	paramsObject                            defectEvidenceObject
	tenant, project, actor, session, parent uuid.UUID
	bucket                                  string
}

func (s SQLTransferSourceReader) prepareNativeSkinTask(ctx context.Context, who auth.Identity, plan nativeFaceDraftPlan, color TransferColorProfile, published nativeSkinStageParams, ram, scratch, output int64, profileVersions ...int) (nativeSkinTaskPreparation, error) {
	var zero nativeSkinTaskPreparation
	version, err := nativeModelProfileVersion(profileVersions)
	if err != nil {
		return zero, err
	}
	before, err := s.readCompletedNativeFaceParse(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	if published.document != uuid.NewSHA1(plan.detector.request.RunID, []byte("semantic.mask.face_skin.params:reference")) || !reflect.DeepEqual(before, published.parser) {
		return zero, v2input.ErrConflict
	}
	binding := plan.detector.request.Intent.Reference
	manifest := before.catalog.Outputs[1]
	labels, err := s.faceParsePreparationPixels(ctx, who, plan.detector.request.TenantID, binding, manifest, before.detector.catalog.Output, before.catalog.Outputs[0])
	if err != nil {
		return zero, err
	}
	params, err := s.defectVersionParamsObject(ctx, who, plan.detector.request.TenantID, binding, published.document, published.ref)
	if err != nil {
		return zero, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, params, published.ref)
	if err != nil {
		return zero, err
	}
	run := uuid.NewSHA1(plan.detector.request.RunID, []byte("semantic.mask.face_skin:reference"))
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	sizes := []v2input.SemanticInputSize{{Ref: published.ref, Bytes: params.Bytes}, {Ref: manifest, Bytes: before.objects[0].object.Bytes}, {Ref: labels.ref, Bytes: labels.object.Bytes}}
	frozen, err := v2input.CompileSemanticFaceSkinTask(ctx, workflow, 0, binding, json.RawMessage(before.detector.sourceBody), manifest, json.RawMessage(before.body), before.catalog.Params, json.RawMessage(before.paramsBody), before.detector.catalog.Output, json.RawMessage(before.detector.body), published.ref, sizes, ram, scratch, output)
	if err != nil {
		return zero, err
	}
	if version == 1 {
		frozen, err = frozen.WithInputIndexScratch()
		if err != nil {
			return zero, err
		}
	}
	task, err := frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	if err := validateNativeModelProfileCapacity(task, ram, 0, scratch, output); err != nil {
		return zero, err
	}
	count, err := frozen.ParamsByteCount()
	if err != nil || count != int64(len(raw)) || count != params.Bytes {
		return zero, v2input.ErrConflict
	}
	after, err := s.readCompletedNativeFaceParse(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	currentLabels, err := s.faceParsePreparationPixels(ctx, who, plan.detector.request.TenantID, binding, manifest, before.detector.catalog.Output, labels.ref)
	if err != nil {
		return zero, err
	}
	currentParams, err := s.defectVersionParamsObject(ctx, who, plan.detector.request.TenantID, binding, published.document, published.ref)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(before, after) || labels != currentLabels || params != currentParams {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeSkinTaskPreparation{frozen: frozen, params: published, labels: labels, paramsObject: defectEvidenceObject{ref: published.ref, object: params}, tenant: plan.detector.request.TenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, parent: plan.detector.request.RunID, bucket: s.Bucket}, nil
}
