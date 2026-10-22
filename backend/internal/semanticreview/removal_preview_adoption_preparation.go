package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"unicode/utf8"
)

// PreparedShownRemoval owns the exact publication and SQL preflight payload.
// It is not a committed review, admitted task or permission to activate a child.
type PreparedShownRemoval struct {
	compiled  CompiledShownRemovalAdoption
	preview   RemovalPreviewParams
	payload   ShownRemovalCommitInputs
	documents json.RawMessage
}

// PrepareShownRemoval composes the copy path without calling a render compiler.
// Live authority is checked before and after publication; atomic SQL admission
// must check it again, along with the decision batch and quota.
func PrepareShownRemoval(ctx context.Context, child uuid.UUID, approved ApprovedTaskDraft,
	selected LockedRemovalPreviewSelection, physical PhysicalRemovalPreview, candidate, intent []byte,
	docs ExposureSourceDocuments, owner ExposureOwnerCosts, limits ExposureResourceEstimate,
	pin ExposureQualificationPin, publisher v2input.Publisher, recheck func(context.Context) error) (PreparedShownRemoval, error) {
	return prepareShownRemoval(ctx, child, approved, selected, physical, candidate, intent, docs, owner, limits, pin,
		func(FrozenShownRemovalTask) (v2input.Publisher, error) {
			if publisher == nil {
				return nil, ErrInvalid
			}
			return publisher, nil
		}, recheck)
}

// PrepareShownRemovalWithPublisher constructs publication from the exact compiled
// closure. Factory executes only after document ownership and live recheck.
func PrepareShownRemovalWithPublisher(ctx context.Context, child uuid.UUID, approved ApprovedTaskDraft,
	selected LockedRemovalPreviewSelection, physical PhysicalRemovalPreview, candidate, intent []byte,
	docs ExposureSourceDocuments, owner ExposureOwnerCosts, limits ExposureResourceEstimate,
	pin ExposureQualificationPin, factory func(FrozenShownRemovalTask) (v2input.Publisher, error),
	recheck func(context.Context) error) (PreparedShownRemoval, error) {
	return prepareShownRemoval(ctx, child, approved, selected, physical, candidate, intent, docs, owner, limits, pin, factory, recheck)
}

func prepareShownRemoval(ctx context.Context, child uuid.UUID, approved ApprovedTaskDraft,
	selected LockedRemovalPreviewSelection, physical PhysicalRemovalPreview, candidate, intent []byte,
	docs ExposureSourceDocuments, owner ExposureOwnerCosts, limits ExposureResourceEstimate,
	pin ExposureQualificationPin, factory func(FrozenShownRemovalTask) (v2input.Publisher, error),
	recheck func(context.Context) error) (PreparedShownRemoval, error) {
	empty := PreparedShownRemoval{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if factory == nil || recheck == nil {
		return empty, ErrInvalid
	}
	compiled, err := CompileShownRemovalAdoption(child, approved, selected, physical)
	if err != nil {
		return empty, err
	}
	frozen, err := CompileShownRemovalTask(compiled, approved, physical, candidate, intent, docs, owner, limits, pin)
	if err != nil {
		return empty, err
	}
	// Own all exact raw documents before the first callback or object-store wait.
	bundle, err := shownRemovalDocumentBundle(approved, physical, candidate, intent, docs)
	if err != nil {
		return empty, err
	}
	if err := recheck(ctx); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	publisher, err := factory(frozen)
	if err != nil {
		return empty, err
	}
	if publisher == nil {
		return empty, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	published, err := PublishShownRemovalTask(ctx, frozen, publisher)
	if err != nil {
		return empty, err
	}
	payload, err := PrepareShownRemovalCommitInputs(ctx, frozen, published)
	if err != nil {
		return empty, err
	}
	if err := recheck(ctx); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return PreparedShownRemoval{compiled: compiled, preview: physical.linkage.Presentation.Params, payload: payload, documents: bundle}, nil
}

func shownRemovalDocumentBundle(approved ApprovedTaskDraft, physical PhysicalRemovalPreview,
	candidateRaw, intentRaw []byte, docs ExposureSourceDocuments) (json.RawMessage, error) {
	if !physical.verified {
		return nil, ErrInvalid
	}
	preview := physical.linkage.Presentation.Params
	candidate, err := v2input.DecodeSemanticCandidate(candidateRaw)
	if err != nil || candidate.Mask == nil {
		return nil, ErrInvalid
	}
	intent, err := v2input.DecodeSemanticEditIntent(intentRaw)
	if err != nil {
		return nil, ErrInvalid
	}
	values := []struct {
		name string
		ref  v2input.Ref
		raw  []byte
	}{
		{"approved", approved.Params, approved.Body},
		{"presentation", physical.linkage.Document.Source.Ref, physical.linkage.Document.raw},
		{"numerical", physical.linkage.Presentation.NumericalEvidence, physical.linkage.Evidence.raw},
		{"image", physical.linkage.Presentation.WorkingImage, physical.image.ImageBytes},
		{"candidate", preview.Candidate, candidateRaw}, {"intent", preview.Intent, intentRaw},
		{"source", preview.Source.Source, docs.Source}, {"reference", intent.Reference.Source, docs.Reference},
		{"mask", *candidate.Mask, docs.Mask}, {"protection", preview.Protection, docs.Protection}}
	bundle := make(map[string]string, len(values))
	for _, v := range values {
		if len(v.raw) < 2 || len(v.raw) > 1048576 || !utf8.Valid(v.raw) || !json.Valid(v.raw) || v.ref.ArtifactID == uuid.Nil {
			return nil, ErrInvalid
		}
		sum := sha256.Sum256(v.raw)
		if hex.EncodeToString(sum[:]) != v.ref.SHA256 {
			return nil, ErrInvalid
		}
		bundle[v.name] = string(v.raw)
	}
	raw, err := canonical.Marshal(bundle)
	if err != nil {
		return nil, ErrInvalid
	}
	return raw, nil
}
