package v2draft

import (
	"context"
	"io"

	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type identityPixelObject struct {
	role   string
	ref    v2input.Ref
	object objectstore.Object
}

// VerifyIdentityPublishedPixels independently recomputes the changed mask and
// guard errors from canonical task inputs and the canonical result array.
// It does not grant adoption: the caller must recheck lineage, consent, model
// qualification and every object version in its commit transaction.
func VerifyIdentityPublishedPixels(ctx context.Context, scope IdentityResultScope,
	inputs IdentityResultInputs, documents IdentityPixelDocuments, result VerifiedIdentityResult,
	inputCatalog IdentityResultInputCatalog, outputCatalog IdentityResultCatalog,
	objects v2input.SessionAnalysisObjectReader) (masknpy.IdentityPixelMeasurements, error) {
	return verifyIdentityPublishedPixels(ctx, scope, inputs, documents, result,
		inputCatalog, outputCatalog, objects, nil)
}

func verifyIdentityPublishedPixels(ctx context.Context, scope IdentityResultScope,
	inputs IdentityResultInputs, documents IdentityPixelDocuments, result VerifiedIdentityResult,
	inputCatalog IdentityResultInputCatalog, outputCatalog IdentityResultCatalog,
	objects v2input.SessionAnalysisObjectReader,
	captured *map[string]objectstore.Object) (masknpy.IdentityPixelMeasurements, error) {
	var zero masknpy.IdentityPixelMeasurements
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if inputCatalog == nil || outputCatalog == nil || objects == nil ||
		!isDocumentRef(result.Pixels) || documents.BeforeRef != inputs.Params.Before.Source ||
		documents.AfterRef != inputs.Params.After.Source ||
		documents.EffectiveRef != inputs.Params.EffectiveMask ||
		documents.ProtectedRef != inputs.Params.Protected ||
		documents.Before.Canvas != documents.After.Canvas {
		return zero, v2input.ErrInvalid
	}
	beforeMetadata, err := v2input.ResolveSemanticSourceMetadata(inputs.Params.Before, documents.BeforeRaw)
	if err != nil {
		return zero, err
	}
	afterMetadata, err := v2input.ResolveSemanticSourceMetadata(inputs.Params.After, documents.AfterRaw)
	if err != nil {
		return zero, err
	}
	effectiveRef, err := v2input.ResolveSemanticMaskPixels(documents.EffectiveRef,
		documents.EffectiveRaw, inputs.Params.After, documents.AfterRaw, false)
	if err != nil {
		return zero, err
	}
	protectedRef, err := v2input.ResolveSemanticMaskPixels(documents.ProtectedRef,
		documents.ProtectedRaw, inputs.Params.After, documents.AfterRaw, true)
	if err != nil {
		return zero, err
	}
	if beforeMetadata.Inputs.Pixels != documents.BeforePixels ||
		afterMetadata.Inputs.Pixels != documents.AfterPixels ||
		effectiveRef != documents.EffectivePixels || protectedRef != documents.ProtectedPixels ||
		!sameOptionalIdentityRef(beforeMetadata.Inputs.Alpha, documents.BeforeAlpha) ||
		!sameOptionalIdentityRef(afterMetadata.Inputs.Alpha, documents.AfterAlpha) {
		return zero, v2input.ErrInvalid
	}
	items := []identityPixelObject{
		{role: "before_pixels", ref: documents.BeforePixels},
		{role: "after_pixels", ref: documents.AfterPixels},
		{role: "effective_pixels", ref: documents.EffectivePixels},
		{role: "protected_pixels", ref: documents.ProtectedPixels},
	}
	if documents.BeforeAlpha != nil {
		items = append(items, identityPixelObject{role: "before_alpha", ref: *documents.BeforeAlpha})
	}
	if documents.AfterAlpha != nil {
		items = append(items, identityPixelObject{role: "after_alpha", ref: *documents.AfterAlpha})
	}
	opened := make([]io.ReadCloser, 0, len(items)+1)
	defer func() {
		for _, reader := range opened {
			_ = reader.Close()
		}
	}()
	pins := make(map[string]masknpy.PinnedMask, len(items)+1)
	for i := range items {
		item := &items[i]
		if !isDocumentRef(item.ref) {
			return zero, v2input.ErrInvalid
		}
		object, err := inputCatalog.ResolveIdentityResultInput(ctx, scope, item.role, item.ref)
		if err != nil {
			return zero, err
		}
		if object.ContentType != "application/x-npy" || object.SHA256 != item.ref.SHA256 ||
			object.VersionID == "" || object.VersionID == "null" || object.Bytes < 14 {
			return zero, v2input.ErrInvalid
		}
		reader, err := objects.Open(ctx, object)
		if err != nil {
			return zero, err
		}
		if reader == nil {
			return zero, v2input.ErrInvalid
		}
		opened = append(opened, reader)
		item.object = object
		pins[item.role] = masknpy.PinnedMask{Reader: reader, Bytes: object.Bytes, SHA256: object.SHA256}
	}
	changed, err := outputCatalog.ResolveIdentityResultObject(ctx, scope, "pixels", result.Pixels)
	if err != nil {
		return zero, err
	}
	if changed != result.Objects[2] || changed.ContentType != "application/x-npy" ||
		changed.SHA256 != result.Pixels.SHA256 || changed.Bytes < 14 {
		return zero, v2input.ErrExpired
	}
	changedReader, err := objects.Open(ctx, changed)
	if err != nil {
		return zero, err
	}
	if changedReader == nil {
		return zero, v2input.ErrInvalid
	}
	opened = append(opened, changedReader)
	pins["changed"] = masknpy.PinnedMask{Reader: changedReader, Bytes: changed.Bytes, SHA256: changed.SHA256}
	var beforeAlpha, afterAlpha *masknpy.PinnedMask
	if documents.BeforeAlpha != nil {
		value := pins["before_alpha"]
		beforeAlpha = &value
	}
	if documents.AfterAlpha != nil {
		value := pins["after_alpha"]
		afterAlpha = &value
	}
	measured, err := masknpy.VerifyIdentityPixelDelta(ctx,
		pins["before_pixels"], pins["after_pixels"], pins["effective_pixels"],
		pins["protected_pixels"], pins["changed"], beforeAlpha, afterAlpha,
		int64(documents.After.Canvas.Width), int64(documents.After.Canvas.Height))
	if err != nil {
		return zero, err
	}
	if measured.OutsideMaskMaxError != result.OutsideMaskMaxError ||
		(result.Status == "pass" && (measured.OutsideMaskMaxError > 0 || measured.ProtectedMaxError > 0)) ||
		(measured.ProtectedMaxError > 0 && result.Status != "fail") {
		return zero, v2input.ErrInvalid
	}
	for _, item := range items {
		current, err := inputCatalog.ResolveIdentityResultInput(ctx, scope, item.role, item.ref)
		if err != nil {
			return zero, err
		}
		if current != item.object {
			return zero, v2input.ErrExpired
		}
	}
	current, err := outputCatalog.ResolveIdentityResultObject(ctx, scope, "pixels", result.Pixels)
	if err != nil {
		return zero, err
	}
	if current != changed {
		return zero, v2input.ErrExpired
	}
	for i, item := range []struct {
		role string
		ref  v2input.Ref
	}{
		{"before", documents.BeforeRef}, {"after", documents.AfterRef},
		{"effective", documents.EffectiveRef}, {"protected", documents.ProtectedRef}} {
		current, err := inputCatalog.ResolveIdentityResultInput(ctx, scope, item.role, item.ref)
		if err != nil {
			return zero, err
		}
		if current != documents.Objects[i] {
			return zero, v2input.ErrExpired
		}
	}
	if captured != nil {
		pins := make(map[string]objectstore.Object, len(items))
		for _, item := range items {
			pins[item.role] = item.object
		}
		*captured = pins
	}
	return measured, ctx.Err()
}

func sameOptionalIdentityRef(a, b *v2input.Ref) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
