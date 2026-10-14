package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type registeredNativeInputManualInitialProtection struct {
	verified  VerifiedProtection
	published publishedNativeInputManualInitialProtection
	objects   [2]defectEvidenceObject
}

// Physical result verification plus exact registered root document, outside SQL.
// Final parent admission must hold registration and completed result authority.
func (s SQLTransferSourceReader) readRegisteredNativeInputManualInitialProtection(ctx context.Context, who auth.Identity, published publishedNativeInputManualInitialProtection, color TransferColorProfile) (registeredNativeInputManualInitialProtection, error) {
	var zero registeredNativeInputManualInitialProtection
	p := published.preparation
	if err := verifyNativeInputManualInitialDraftSeal(ctx, p.plan); err != nil {
		return zero, err
	}
	result, err := s.readCompletedNativeInputManualInitialDefect(ctx, who, p.plan, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(result, p.result) {
		return zero, v2input.ErrConflict
	}
	objects, err := s.registeredNativeInputManualInitialProtectionObjects(ctx, who, p.plan, published)
	if err != nil {
		return zero, err
	}
	if objects[1] != result.objects[1] {
		return zero, v2input.ErrConflict
	}
	body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objects[0].object, objects[0].ref)
	if err != nil {
		return zero, err
	}
	if string(body) != p.body {
		return zero, v2input.ErrConflict
	}
	parent := p.plan.prepared.stage.dependencies.dependencies.producers.detector.request
	image := []byte(p.plan.prepared.stage.dependencies.dependencies.selection.sourceBody)
	pixels, err := v2input.ResolveSemanticMaskPixels(published.root, body, parent.Intent.Reference, image, true)
	if err != nil {
		return zero, err
	}
	if pixels != p.pixels || pixels != result.evidence.Protection {
		return zero, v2input.ErrConflict
	}
	current, err := s.registeredNativeInputManualInitialProtectionObjects(ctx, who, p.plan, published)
	if err != nil {
		return zero, err
	}
	after, err := s.readCompletedNativeInputManualInitialDefect(ctx, who, p.plan, color)
	if err != nil {
		return zero, err
	}
	if current != objects || !reflect.DeepEqual(after, result) {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return registeredNativeInputManualInitialProtection{verified: VerifiedProtection{Root: published.root, Pixels: pixels, Body: body}, published: published, objects: objects}, nil
}
