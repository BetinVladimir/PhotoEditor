package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type nativeTargetSkinParams struct {
	document uuid.UUID
	ref      v2input.Ref
	parser   nativeTargetFaceResult
}
type nativeTargetSkinTaskPreparation struct {
	legacyQueueOmitted bool
	frozen                                  v2input.FrozenSemanticFaceSkinTask
	params                                  nativeTargetSkinParams
	paramsObject                            defectEvidenceObject
	tenant, project, actor, session, parent uuid.UUID
	bucket                                  string
}

// Publication brackets immutable Params with complete physical target-parser
// proofs. The target binding is explicit; original reference is never rewritten.
func (s SQLTransferSourceReader) publishNativeTargetSkinParams(ctx context.Context, who auth.Identity, parser nativeTargetFaceDraftPlan, color TransferColorProfile, publisher v2draft.Publisher) (nativeTargetSkinParams, error) {
	var zero nativeTargetSkinParams
	if parser.prepared.operation != "face.parse" {
		return zero, v2input.ErrInvalid
	}
	before, err := s.readNativeTargetFaceResult(ctx, who, parser, color)
	if err != nil {
		return zero, err
	}
	d := before.detector.completed
	binding := parser.detector.anchor.entry.Binding
	document := uuid.NewSHA1(parser.detector.anchor.entry.Namespace, []byte("semantic.mask.face_skin.params:reference"))
	image, labels, params, boxes := json.RawMessage(d.sourceBody), json.RawMessage(before.body), json.RawMessage(before.paramsBody), json.RawMessage(d.body)
	prepared, err := v2input.PrepareSemanticFaceSkinParams(ctx, binding, image, before.catalog.Outputs[1], labels, before.catalog.Params, params, d.catalog.Output, boxes)
	if err != nil {
		return zero, err
	}
	ref, err := publisher.PublishSemanticFaceSkinParams(ctx, who, parser.shell.Tenant, document, binding, image, before.catalog.Outputs[1], labels, before.catalog.Params, params, d.catalog.Output, boxes)
	if err != nil {
		return zero, err
	}
	sha, err := canonical.Hash(prepared.Params)
	if err != nil || ref.ArtifactID == uuid.Nil || ref.SHA256 != sha {
		return zero, v2input.ErrConflict
	}
	after, err := s.readNativeTargetFaceResult(ctx, who, parser, color)
	if err != nil {
		return zero, err
	}
	if before.seal != after.seal {
		return zero, v2input.ErrConflict
	}
	return nativeTargetSkinParams{document: document, ref: ref, parser: before}, ctx.Err()
}

func (s SQLTransferSourceReader) prepareNativeTargetSkinTask(ctx context.Context, who auth.Identity, parser nativeTargetFaceDraftPlan, color TransferColorProfile, published nativeTargetSkinParams, ram, scratch, output int64, profileVersions ...int) (nativeTargetSkinTaskPreparation, error) {
	var zero nativeTargetSkinTaskPreparation
	version, err := nativeModelProfileVersion(profileVersions)
	if err != nil {
		return zero, err
	}
	if parser.prepared.operation != "face.parse" {
		return zero, v2input.ErrInvalid
	}
	before, err := s.readNativeTargetFaceResult(ctx, who, parser, color)
	if err != nil {
		return zero, err
	}
	digest, err := targetFaceResultDigest(published.parser)
	if err != nil || digest != published.parser.seal || published.parser.seal != before.seal || published.document != uuid.NewSHA1(parser.detector.anchor.entry.Namespace, []byte("semantic.mask.face_skin.params:reference")) {
		return zero, v2input.ErrConflict
	}
	binding := parser.detector.anchor.entry.Binding
	object, err := s.defectVersionParamsObject(ctx, who, parser.shell.Tenant, binding, published.document, published.ref)
	if err != nil {
		return zero, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, published.ref)
	if err != nil {
		return zero, err
	}
	run := uuid.NewSHA1(parser.detector.anchor.entry.Namespace, []byte("semantic.mask.face_skin:reference"))
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	d := before.detector.completed
	sizes := []v2input.SemanticInputSize{{Ref: published.ref, Bytes: object.Bytes}, {Ref: before.catalog.Outputs[1], Bytes: before.objects[0].object.Bytes}, {Ref: before.pixels.ref, Bytes: before.pixels.object.Bytes}}
	frozen, err := v2input.CompileSemanticFaceSkinTask(ctx, workflow, 0, binding, json.RawMessage(d.sourceBody), before.catalog.Outputs[1], json.RawMessage(before.body), before.catalog.Params, json.RawMessage(before.paramsBody), d.catalog.Output, json.RawMessage(d.body), published.ref, sizes, ram, scratch, output)
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
	if err != nil || count != int64(len(raw)) || count != object.Bytes {
		return zero, v2input.ErrConflict
	}
	after, err := s.readNativeTargetFaceResult(ctx, who, parser, color)
	if err != nil {
		return zero, err
	}
	current, err := s.defectVersionParamsObject(ctx, who, parser.shell.Tenant, binding, published.document, published.ref)
	if err != nil {
		return zero, err
	}
	if before.seal != after.seal || current != object {
		return zero, v2input.ErrConflict
	}
	currentRaw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, current, published.ref)
	if err != nil {
		return zero, err
	}
	if string(raw) != string(currentRaw) {
		return zero, v2input.ErrConflict
	}
	return nativeTargetSkinTaskPreparation{frozen: frozen, params: published, paramsObject: defectEvidenceObject{ref: published.ref, object: object}, tenant: parser.shell.Tenant, project: s.ProjectID, actor: who.UserID, session: who.SessionID, parent: parser.detector.anchor.entry.Namespace, bucket: s.Bucket}, ctx.Err()
}
