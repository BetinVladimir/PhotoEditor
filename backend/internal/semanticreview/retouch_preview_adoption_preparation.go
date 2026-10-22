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

// PreparedShownRetouch owns the exact publication and SQL preflight payload.
// It is not a committed review, admitted task or permission to activate a child.
type PreparedShownRetouch struct {
	compiled  CompiledShownRetouchAdoption
	preview   RetouchPreviewParams
	payload   ShownRetouchCommitInputs
	documents json.RawMessage
}

// PrepareShownRetouch composes the copy path without calling a render compiler.
// Live authority is checked before and after publication; atomic SQL admission
// must check it again, along with the decision batch and quota.
func PrepareShownRetouch(ctx context.Context, child uuid.UUID, approved ApprovedTaskDraft,
	selected LockedRetouchPreviewSelection, physical PhysicalRetouchPreview, candidate, intent []byte,
	docs ExposureSourceDocuments, owner ExposureOwnerCosts, limits ExposureResourceEstimate,
	pin RetouchQualificationPin, publisher v2input.Publisher, recheck func(context.Context) error) (PreparedShownRetouch, error) {
	return prepareShownRetouch(ctx, child, approved, selected, physical, candidate, intent, docs, owner, limits, pin,
		func(FrozenShownRetouchTask) (v2input.Publisher, error) {
			if publisher == nil {
				return nil, ErrInvalid
			}
			return publisher, nil
		}, recheck)
}

// PrepareShownRetouchWithPublisher constructs publication from the exact compiled
// closure. Factory executes only after document ownership and live recheck.
func PrepareShownRetouchWithPublisher(ctx context.Context, child uuid.UUID, approved ApprovedTaskDraft,
	selected LockedRetouchPreviewSelection, physical PhysicalRetouchPreview, candidate, intent []byte,
	docs ExposureSourceDocuments, owner ExposureOwnerCosts, limits ExposureResourceEstimate,
	pin RetouchQualificationPin, factory func(FrozenShownRetouchTask) (v2input.Publisher, error),
	recheck func(context.Context) error) (PreparedShownRetouch, error) {
	return prepareShownRetouch(ctx, child, approved, selected, physical, candidate, intent, docs, owner, limits, pin, factory, recheck)
}

func prepareShownRetouch(ctx context.Context, child uuid.UUID, approved ApprovedTaskDraft,
	selected LockedRetouchPreviewSelection, physical PhysicalRetouchPreview, candidate, intent []byte,
	docs ExposureSourceDocuments, owner ExposureOwnerCosts, limits ExposureResourceEstimate,
	pin RetouchQualificationPin, factory func(FrozenShownRetouchTask) (v2input.Publisher, error),
	recheck func(context.Context) error) (PreparedShownRetouch, error) {
	empty := PreparedShownRetouch{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if factory == nil || recheck == nil {
		return empty, ErrInvalid
	}
	compiled, err := CompileShownRetouchAdoption(child, approved, selected, physical)
	if err != nil {
		return empty, err
	}
	frozen, err := CompileShownRetouchTask(compiled, approved, physical, candidate, intent, docs, owner, limits, pin)
	if err != nil {
		return empty, err
	}
	// Own all exact raw documents before the first callback or object-store wait.
	bundle, err := shownRetouchDocumentBundle(approved, physical, candidate, intent, docs)
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
	// Publisher construction may wait on deployment state; repeat authority before IO.
	if err := recheck(ctx); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	published, err := PublishShownRetouchTask(ctx, frozen, publisher)
	if err != nil {
		return empty, err
	}
	payload, err := PrepareShownRetouchCommitInputs(ctx, frozen, published)
	if err != nil {
		return empty, err
	}
	if err := recheck(ctx); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return PreparedShownRetouch{compiled: compiled, preview: physical.linkage.Presentation.Params, payload: payload, documents: bundle}, nil
}

func shownRetouchDocumentBundle(approved ApprovedTaskDraft, physical PhysicalRetouchPreview,
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
