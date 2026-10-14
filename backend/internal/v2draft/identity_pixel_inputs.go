package v2draft

import (
	"bytes"
	"context"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// IdentityPixelDocuments pins the four source documents and derives every
// child array from those exact bytes. No caller supplies an independent array.
type IdentityPixelDocuments struct {
	Before, After                                               review.WorkingImage
	BeforeRaw, AfterRaw, EffectiveRaw, ProtectedRaw             []byte
	BeforeRef, AfterRef, EffectiveRef, ProtectedRef             v2input.Ref
	BeforePixels, AfterPixels, EffectivePixels, ProtectedPixels v2input.Ref
	BeforeAlpha, AfterAlpha                                     *v2input.Ref
	Objects                                                     [4]objectstore.Object
}

func LoadIdentityPixelDocuments(ctx context.Context, scope IdentityResultScope,
	catalog IdentityResultInputCatalog, objects v2input.SessionAnalysisObjectReader,
	inputs IdentityResultInputs) (IdentityPixelDocuments, error) {
	var zero IdentityPixelDocuments
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || objects == nil || !isDocumentRef(inputs.ParamsRef) ||
		!isDocumentRef(inputs.AfterRef) || inputs.AfterRef != inputs.Params.After.Source {
		return zero, v2input.ErrInvalid
	}
	refs := [4]v2input.Ref{inputs.Params.Before.Source, inputs.AfterRef,
		inputs.Params.EffectiveMask, inputs.Params.Protected}
	roles := [4]string{"before", "after", "effective", "protected"}
	var raws [4][]byte
	var pinned [4]objectstore.Object
	for i, ref := range refs {
		if !isDocumentRef(ref) {
			return zero, v2input.ErrInvalid
		}
		object, err := catalog.ResolveIdentityResultInput(ctx, scope, roles[i], ref)
		if err != nil {
			return zero, err
		}
		raw, err := readJSONArtifact(ctx, objects, object, ref)
		if err != nil {
			return zero, err
		}
		raws[i], pinned[i] = raw, object
	}
	if !bytes.Equal(raws[1], inputs.AfterRaw) || pinned[1] != inputs.Objects[1] {
		return zero, v2input.ErrExpired
	}
	before, err := review.ParseWorkingImage(raws[0])
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	after, err := review.ParseWorkingImage(raws[1])
	if err != nil || before.Canvas != after.Canvas || before.Color != after.Color ||
		before.DType != "float32" || after.DType != "float32" ||
		before.Canvas.Width*before.Canvas.Height > 200000000 {
		return zero, v2input.ErrInvalid
	}
	leftChain, err := canonical.Bytes(before.TransformChain)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	rightChain, err := canonical.Bytes(after.TransformChain)
	if err != nil || !bytes.Equal(leftChain, rightChain) {
		return zero, v2input.ErrInvalid
	}
	if _, err = v2input.ResolveSemanticSourceMetadata(inputs.Params.Before, raws[0]); err != nil {
		return zero, err
	}
	if _, err = v2input.ResolveSemanticSourceMetadata(inputs.Params.After, raws[1]); err != nil {
		return zero, err
	}
	effectivePixels, err := v2input.ResolveSemanticMaskPixels(refs[2], raws[2], inputs.Params.After, raws[1], false)
	if err != nil {
		return zero, err
	}
	protectedPixels, err := v2input.ResolveSemanticMaskPixels(refs[3], raws[3], inputs.Params.After, raws[1], true)
	if err != nil {
		return zero, err
	}
	for i, ref := range refs {
		current, err := catalog.ResolveIdentityResultInput(ctx, scope, roles[i], ref)
		if err != nil {
			return zero, err
		}
		if current != pinned[i] {
			return zero, v2input.ErrExpired
		}
	}
	child := func(ref review.ArtifactRef) v2input.Ref { return v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256} }
	result := IdentityPixelDocuments{Before: before, After: after,
		BeforeRaw: raws[0], AfterRaw: raws[1], EffectiveRaw: raws[2], ProtectedRaw: raws[3],
		BeforeRef: refs[0], AfterRef: refs[1], EffectiveRef: refs[2], ProtectedRef: refs[3],
		BeforePixels: child(before.Pixels), AfterPixels: child(after.Pixels),
		EffectivePixels: effectivePixels, ProtectedPixels: protectedPixels, Objects: pinned}
	if before.Alpha != nil {
		value := child(*before.Alpha)
		result.BeforeAlpha = &value
	}
	if after.Alpha != nil {
		value := child(*after.Alpha)
		result.AfterAlpha = &value
	}
	return result, nil
}
