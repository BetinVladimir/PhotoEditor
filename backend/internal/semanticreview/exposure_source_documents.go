package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// ExposureSourceDocuments are exact pinned WorkingImage and MaskRef bytes.
// Scoped catalog/producer authority, transform bytes and physical NPY/ICC
// verification remain required; these documents do not grant object access.
type ExposureSourceDocuments struct {
	Source, Reference, Mask, Protection json.RawMessage
}

// CompileExposureTaskFromDocuments derives dimensions, RGB/alpha/geometry and
// mask/protection pixels from hashed documents rather than caller estimates.
// The protection root must come from verified candidate protection provenance.
func CompileExposureTaskFromDocuments(child uuid.UUID, draft ApprovedTaskDraft,
	reference v2input.SessionVersionBinding, protection v2input.Ref, documents ExposureSourceDocuments,
	owner ExposureOwnerCosts, limits ExposureResourceEstimate, pin ExposureQualificationPin) (FrozenExposureTask, error) {
	input, err := ResolveExposureTaskInputsFromDocuments(draft, reference, protection, documents, owner, limits, pin)
	if err != nil {
		return FrozenExposureTask{}, err
	}
	return CompileExposureTask(child, draft, input.Dependencies, input.Width, input.Height, input.OwnerCosts, input.Limits, input.Qualification)
}

// ResolveExposureTaskInputsFromDocuments builds the exact input projection
// consumed by the private-review batch compiler and post-publication verifier.
// Protection provenance, transform and NPY authority remain caller duties.
func ResolveExposureTaskInputsFromDocuments(draft ApprovedTaskDraft, reference v2input.SessionVersionBinding,
	protection v2input.Ref, documents ExposureSourceDocuments, owner ExposureOwnerCosts,
	limits ExposureResourceEstimate, pin ExposureQualificationPin) (ExposureTaskInputs, error) {
	empty := ExposureTaskInputs{}
	var edit ApprovedEdit
	sum := sha256.Sum256(draft.Body)
	if len(draft.Body) > 1048576 || draft.TaskID == uuid.Nil || draft.TargetID == uuid.Nil || draft.Params.ArtifactID != uuid.NewSHA1(draft.TaskID, []byte("Params")) || hex.EncodeToString(sum[:]) != draft.Params.SHA256 || decodeStrict(draft.Body, &edit) != nil {
		return empty, ErrInvalid
	}
	source, err := v2input.ResolveSemanticSourceMetadata(edit.Source, documents.Source)
	if err != nil {
		return empty, err
	}
	ref, err := v2input.ResolveSemanticSourceMetadata(reference, documents.Reference)
	if err != nil {
		return empty, err
	}
	maskPixels, err := v2input.ResolveSemanticMaskPixels(edit.Mask, documents.Mask, edit.Source, documents.Source, false)
	if err != nil {
		return empty, err
	}
	protectionPixels, err := v2input.ResolveSemanticMaskPixels(protection, documents.Protection, edit.Source, documents.Source, true)
	if err != nil {
		return empty, err
	}
	dependencies := v2input.ExposureDependencies{Reference: reference.Source, Source: source.Inputs, ReferenceImage: ref.Inputs,
		MaskPixels: maskPixels, Protection: protection, ProtectionPixels: protectionPixels,
		Geometry: append(append([]v2input.Ref(nil), source.Geometry...), ref.Geometry...)}
	if edit.TargetID != draft.TargetID || draft.Kind != "local_exposure" {
		return empty, ErrInvalid
	}
	return ExposureTaskInputs{TargetID: draft.TargetID, Source: edit.Source, Reference: reference, Width: source.Width, Height: source.Height,
		Dependencies: dependencies, OwnerCosts: owner, Limits: limits, Qualification: pin}, nil
}

// ExposureTargetDocuments carries bounded pinned documents resolved under the
// authenticated target scope. It has no caller-supplied dimensions or pixels.
type ExposureTargetDocuments struct {
	TargetID      uuid.UUID
	Protection    v2input.Ref
	Documents     ExposureSourceDocuments
	OwnerCosts    ExposureOwnerCosts
	Limits        ExposureResourceEstimate
	Qualification ExposureQualificationPin
}

// ResolveApprovedExposureDocumentInputs rebuilds original approved Params and
// reference binding from private review, then resolves exact per-target document
// coverage. Any failure returns no partial inputs; reject-only produces none.
func ResolveApprovedExposureDocumentInputs(child uuid.UUID, prepared PreparedReview, targets []LockedTarget,
	request ApplyDecision, documents []ExposureTargetDocuments) ([]ExposureTaskInputs, error) {
	if child == uuid.Nil || child == prepared.Sources.RunID {
		return nil, ErrInvalid
	}
	drafts, err := CompileApprovedTaskDrafts(child, prepared, targets, request)
	if err != nil || len(drafts) != len(documents) {
		if err != nil {
			return nil, err
		}
		return nil, ErrInvalid
	}
	owned, err := PrepareReviewDocuments(prepared.Sources, prepared.documents, prepared.modelSHA, prepared.datasetSHA)
	if err != nil {
		return nil, err
	}
	byTarget := make(map[uuid.UUID]ExposureTargetDocuments, len(documents))
	for _, document := range documents {
		if document.TargetID == uuid.Nil {
			return nil, ErrInvalid
		}
		if _, exists := byTarget[document.TargetID]; exists {
			return nil, ErrInvalid
		}
		byTarget[document.TargetID] = document
	}
	result := make([]ExposureTaskInputs, 0, len(drafts))
	for _, draft := range drafts {
		document, exists := byTarget[draft.TargetID]
		if !exists {
			return nil, ErrInvalid
		}
		input, err := ResolveExposureTaskInputsFromDocuments(draft, owned.Intent.Reference, document.Protection, document.Documents, document.OwnerCosts, document.Limits, document.Qualification)
		if err != nil {
			return nil, err
		}
		result = append(result, input)
	}
	return result, nil
}
