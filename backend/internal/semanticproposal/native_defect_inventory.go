package semanticproposal

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type nativeDefectInputPin struct {
	contract string
	pin      defectEvidenceObject
}

// Assembles exact declared files from retained pins. Additional pins must come
// from separately verified protection/manual catalogs; this pure helper grants
// no authority to read them. Missing/extra/conflicting pins fail closed.
func compileNativeDefectInventory(ctx context.Context, prepared nativeDefectFacePreparation, params defectEvidenceObject, additional []nativeDefectInputPin) ([]v2input.Entry, []v2input.SemanticInputSize, error) {
	fail := func(err error) ([]v2input.Entry, []v2input.SemanticInputSize, error) { return nil, nil, err }
	if err := verifyNativeDefectPreparedParams(ctx, prepared); err != nil {
		return fail(err)
	}
	if prepared.person != nil {
		expected, err := nativeDefectPersonInputPins(ctx, prepared)
		if err != nil {
			return fail(err)
		}
		if len(additional) != len(expected) {
			return fail(v2input.ErrConflict)
		}
		for i := range expected {
			if additional[i] != expected[i] {
				return fail(v2input.ErrConflict)
			}
		}
	}
	return assembleNativeDefectInventory(ctx, prepared, params, additional)
}

// Pure file assembly shared only after the caller validates its distinct
// original producer authority. This helper neither validates nor grants it.
func assembleNativeDefectInventory(ctx context.Context, prepared nativeDefectFacePreparation, params defectEvidenceObject, additional []nativeDefectInputPin) ([]v2input.Entry, []v2input.SemanticInputSize, error) {
	fail := func(err error) ([]v2input.Entry, []v2input.SemanticInputSize, error) { return nil, nil, err }
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if params.ref.ArtifactID == uuid.Nil || params.ref.SHA256 != prepared.params.digest || params.object.Bytes != int64(len(prepared.params.params)) {
		return fail(v2input.ErrConflict)
	}
	var refs []review.ArtifactRef
	if json.Unmarshal([]byte(prepared.params.inputs), &refs) != nil {
		return fail(v2input.ErrInvalid)
	}
	wanted := map[uuid.UUID]v2input.Ref{}
	for _, ref := range refs {
		wanted[ref.ID] = v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}
	}
	if _, exists := wanted[params.ref.ArtifactID]; exists {
		return fail(v2input.ErrConflict)
	}
	wanted[params.ref.ArtifactID] = params.ref
	source := prepared.landmarksPlan.detector.prepared.source
	pins, err := decodeSourcePreparationPins(source)
	if err != nil {
		return fail(err)
	}
	candidates := []nativeDefectInputPin{{"Params", params}}
	for _, entry := range pins {
		contract := "NpyArray"
		if entry.Node.Contract == "WorkingImage" {
			contract = "WorkingImage"
		}
		candidates = append(candidates, nativeDefectInputPin{contract, defectEvidenceObject{ref: entry.Node.Ref, object: entry.Object}})
	}
	if _, used := wanted[prepared.landmarks.catalog.Outputs[0].ArtifactID]; used {
		candidates = append(candidates, nativeDefectInputPin{"FaceLandmarks", prepared.landmarks.objects[0]})
	}
	if _, used := wanted[prepared.skin.objects[0].ref.ArtifactID]; used {
		candidates = append(candidates, nativeDefectInputPin{"NpyArray", prepared.skin.objects[0]})
	}
	candidates = append(candidates, additional...)
	return assembleNativeDefectPinnedInventory(ctx, refs, params, candidates)
}

// Shared byte inventory assembly only; producer authority belongs to callers.
func assembleNativeDefectPinnedInventory(ctx context.Context, refs []review.ArtifactRef, params defectEvidenceObject, candidates []nativeDefectInputPin) ([]v2input.Entry, []v2input.SemanticInputSize, error) {
	fail := func(err error) ([]v2input.Entry, []v2input.SemanticInputSize, error) { return nil, nil, err }
	wanted := map[uuid.UUID]v2input.Ref{}
	for _, ref := range refs {
		if ref.ID == uuid.Nil {
			return fail(v2input.ErrInvalid)
		}
		if _, exists := wanted[ref.ID]; exists {
			return fail(v2input.ErrConflict)
		}
		wanted[ref.ID] = v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}
	}
	if params.ref.ArtifactID == uuid.Nil {
		return fail(v2input.ErrInvalid)
	}
	if _, exists := wanted[params.ref.ArtifactID]; exists {
		return fail(v2input.ErrConflict)
	}
	wanted[params.ref.ArtifactID] = params.ref
	seen := map[uuid.UUID]bool{}
	entries := make([]v2input.Entry, 0, len(candidates))
	sizes := make([]v2input.SemanticInputSize, 0, len(candidates))
	for _, entry := range candidates {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		ref, obj := entry.pin.ref, entry.pin.object
		if wanted[ref.ArtifactID] != ref || seen[ref.ArtifactID] || obj.Bytes < 1 || obj.Bytes > 9007199254740991 || obj.SHA256 != ref.SHA256 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
			return fail(v2input.ErrConflict)
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return fail(v2input.ErrInvalid)
		}
		switch entry.contract {
		case "Params", "WorkingImage", "FaceLandmarks", "MaskRef":
			if obj.ContentType != "application/json" {
				return fail(v2input.ErrInvalid)
			}
		case "NpyArray":
			if obj.ContentType != "application/octet-stream" && obj.ContentType != "application/x-npy" {
				return fail(v2input.ErrInvalid)
			}
		default:
			return fail(v2input.ErrInvalid)
		}
		seen[ref.ArtifactID] = true
		entries = append(entries, v2input.Entry{Ref: ref, Contract: entry.contract})
		sizes = append(sizes, v2input.SemanticInputSize{Ref: ref, Bytes: obj.Bytes})
	}
	if len(seen) != len(wanted) {
		return fail(v2input.ErrConflict)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String() })
	sort.Slice(sizes, func(i, j int) bool { return sizes[i].Ref.ArtifactID.String() < sizes[j].Ref.ArtifactID.String() })
	return entries, sizes, nil
}
