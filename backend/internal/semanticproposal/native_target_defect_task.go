package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type nativeTargetDefectTaskPreparation struct {
	legacyQueueOmitted bool
	frozen               v2input.FrozenSemanticDefectTask
	preparation          nativeTargetDefectPreparation
	qualification        nativeTargetDefectQualification
	params, personPixels defectEvidenceObject
	entries, sizes, seal string
}

func compileNativeTargetDefectInventory(ctx context.Context, p nativeTargetDefectPreparation, params, person defectEvidenceObject) ([]v2input.Entry, []v2input.SemanticInputSize, error) {
	fail := func(err error) ([]v2input.Entry, []v2input.SemanticInputSize, error) { return nil, nil, err }
	if err := verifyNativeTargetDefectPreparation(ctx, p); err != nil {
		return fail(err)
	}
	if params.ref.SHA256 != p.params.digest || params.object.Bytes != int64(len(p.params.params)) || person.ref != p.person.mask.Pixels {
		return fail(v2input.ErrConflict)
	}
	var refs []review.ArtifactRef
	if json.Unmarshal([]byte(p.params.inputs), &refs) != nil {
		return fail(v2input.ErrInvalid)
	}
	pins, err := decodeSourcePreparationPins(p.landmarksPlan.detector.detector.prepared.source)
	if err != nil {
		return fail(err)
	}
	candidates := []nativeDefectInputPin{{"Params", params}}
	for _, pin := range pins {
		contract := "NpyArray"
		if pin.Node.Contract == "WorkingImage" {
			contract = "WorkingImage"
		}
		candidates = append(candidates, nativeDefectInputPin{contract, defectEvidenceObject{ref: pin.Node.Ref, object: pin.Object}})
	}
	// Zero-face Params legitimately omit landmark/skin inputs; permanent person
	// pixels and native source stay mandatory. No unused files enter the closure.
	for _, candidate := range []nativeDefectInputPin{{"FaceLandmarks", p.landmarks.objects[0]}, {"NpyArray", p.skin.objects[0]}, {"NpyArray", person}} {
		for _, ref := range refs {
			if ref.ID == candidate.pin.ref.ArtifactID {
				candidates = append(candidates, candidate)
				break
			}
		}
	}
	return assembleNativeDefectPinnedInventory(ctx, refs, params, candidates)
}

func (s SQLTransferSourceReader) compileNativeTargetDefectTask(ctx context.Context, who auth.Identity, p nativeTargetDefectPreparation, q nativeTargetDefectQualification, params, person defectEvidenceObject, ram, scratch, output int64) (nativeTargetDefectTaskPreparation, error) {
	var zero nativeTargetDefectTaskPreparation
	if err := s.verifyNativeTargetDefectQualification(ctx, who, p, q); err != nil {
		return zero, err
	}
	entries, sizes, err := compileNativeTargetDefectInventory(ctx, p, params, person)
	if err != nil {
		return zero, err
	}
	a := p.landmarksPlan.detector.anchor
	run := uuid.NewSHA1(a.entry.Namespace, []byte("semantic.defect.prepare:reference"))
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	frozen, err := v2input.CompileSemanticDefectTask(ctx, workflow, 0, a.entry.Binding, []byte(p.landmarks.detector.completed.sourceBody), []byte(p.params.params), params.ref, entries, sizes, q.costs, ram, 0, scratch, output)
	if err != nil {
		return zero, err
	}
	entryBytes, err := canonical.Marshal(entries)
	if err != nil {
		return zero, err
	}
	sizeBytes, err := canonical.Marshal(sizes)
	if err != nil {
		return zero, err
	}
	prepared := nativeTargetDefectTaskPreparation{frozen: frozen, preparation: p, qualification: q, params: params, personPixels: person, entries: string(entryBytes), sizes: string(sizeBytes)}
	prepared.seal, err = targetDefectTaskDigest(prepared)
	if err != nil {
		return zero, err
	}
	return prepared, ctx.Err()
}

func targetDefectTaskDigest(p nativeTargetDefectTaskPreparation) (string, error) {
	task, err := p.frozen.DraftTask()
	if err != nil {
		return "", err
	}
	return canonical.Hash(map[string]any{"task": task, "preparation": p.preparation.seal, "qualification": p.qualification.seal, "params": map[string]any{"ref": p.params.ref, "object": p.params.object}, "person": map[string]any{"ref": p.personPixels.ref, "object": p.personPixels.object}, "entries": p.entries, "sizes": p.sizes})
}

// Brackets exact target Params and person pixel pin discovery with complete
// physical dependency/qualification checks. No transaction spans object IO.
func (s SQLTransferSourceReader) prepareNativeTargetDefectTask(ctx context.Context, who auth.Identity, p nativeTargetDefectPreparation, q nativeTargetDefectQualification, color TransferColorProfile, published PublishedDefectParams, ram, scratch, output int64) (nativeTargetDefectTaskPreparation, error) {
	var zero nativeTargetDefectTaskPreparation
	if err := s.recheckNativeTargetDefectQualification(ctx, who, p, q, color); err != nil {
		return zero, err
	}
	inputs, err := canonical.Marshal(published.Inputs)
	if err != nil {
		return zero, err
	}
	roots, err := canonical.Marshal(published.ManualRoots)
	if err != nil {
		return zero, err
	}
	if string(inputs) != p.params.inputs || string(roots) != p.params.roots || published.Ref.SHA256 != p.params.digest {
		return zero, v2input.ErrConflict
	}
	a := p.landmarksPlan.detector.anchor
	object, err := s.defectVersionParamsObject(ctx, who, a.request.TenantID, a.entry.Binding, targetDefectParamsDocument(a.entry.Namespace), published.Ref)
	if err != nil {
		return zero, err
	}
	body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, published.Ref)
	if err != nil {
		return zero, err
	}
	if string(body) != p.params.params {
		return zero, v2input.ErrConflict
	}
	catalog, err := (SQLNativeParentArtifactCatalogFactory{API: s.DB, Bucket: s.Bucket}).ForReference(ctx, who, a.request.TenantID, s.ProjectID, a.entry.Binding.AssetVersionID)
	if err != nil {
		return zero, err
	}
	node := v2draft.LayerRef{Ref: p.person.mask.Pixels, Contract: "MaskPixels", ParentRef: p.person.mask.Root}
	pixels, err := catalog.Lookup(ctx, who, a.request.TenantID, node)
	if err != nil {
		return zero, err
	}
	task, err := s.compileNativeTargetDefectTask(ctx, who, p, q, defectEvidenceObject{ref: published.Ref, object: object}, defectEvidenceObject{ref: p.person.mask.Pixels, object: pixels}, ram, scratch, output)
	if err != nil {
		return zero, err
	}
	if err := s.recheckNativeTargetDefectQualification(ctx, who, p, q, color); err != nil {
		return zero, err
	}
	current, err := s.defectVersionParamsObject(ctx, who, a.request.TenantID, a.entry.Binding, targetDefectParamsDocument(a.entry.Namespace), published.Ref)
	if err != nil {
		return zero, err
	}
	after, err := catalog.Lookup(ctx, who, a.request.TenantID, node)
	if err != nil {
		return zero, err
	}
	if current != object || after != pixels {
		return zero, v2input.ErrConflict
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, current, published.Ref)
	if err != nil {
		return zero, err
	}
	if string(raw) != string(body) {
		return zero, v2input.ErrConflict
	}
	return task, ctx.Err()
}
