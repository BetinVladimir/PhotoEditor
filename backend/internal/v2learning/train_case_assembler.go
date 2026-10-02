package v2learning

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type TrainingDocumentReader interface {
	Load(context.Context, TrainingStart, TrainingCandidate) (TrainingEditingDocuments, error)
}
type TrainingRecipeReader interface {
	LoadPair(context.Context, auth.Identity, uuid.UUID,
		TrainingEditingDocuments, string, string) (CompiledTrainingRecipes,
		*v2draft.ResolvedCorrection, error)
}
type TrainingImageReader interface {
	Load(context.Context, auth.Identity, uuid.UUID,
		v2input.SessionVersionBinding, string) (TrainingImage, error)
}
type TrainingICCReader interface {
	DiscoverICC(context.Context, auth.Identity, uuid.UUID,
		v2input.SessionVersionBinding) (string, error)
}
type TrainingDescriptorReader interface {
	Load(context.Context, auth.Identity, uuid.UUID, uuid.UUID,
		v2input.SessionVersionBinding, string,
		*v2draft.ResolvedCorrection) (TrainingDescriptor, error)
}
type TrainingFacesReader interface {
	Load(context.Context, auth.Identity, uuid.UUID,
		v2input.SessionVersionBinding, v2input.Ref) (int, error)
}
type TrainingProtectedSelector interface {
	SelectProtected(context.Context, auth.Identity, TrainingStart,
		TrainingCandidate, TrainingImage) (v2input.Ref, TrainingMask, error)
}

type TrainingCaseAssembler struct {
	Documents   TrainingDocumentReader
	Recipes     TrainingRecipeReader
	Images      TrainingImageReader
	Descriptors TrainingDescriptorReader
	Faces       TrainingFacesReader
	Protection  TrainingProtectedSelector
}

// DiscoverICC establishes the one run-level color lock from an eligible
// candidate's scoped EditingRecipeV3 and WorkingImage. Assemble re-reads the
// same dependencies and checks the selected lock for every admitted case.
func (a TrainingCaseAssembler) DiscoverICC(ctx context.Context,
	start TrainingStart, candidate TrainingCandidate) (string, error) {
	if a.Documents == nil || a.Images == nil ||
		start.TenantID == uuid.Nil || start.ActorID == uuid.Nil ||
		start.AuthSessionID == uuid.Nil ||
		candidate.AssetVersionID == uuid.Nil ||
		!validSHA(candidate.SourceContentSHA) {
		return "", ErrInvalidTrainingDraft
	}
	reader, okay := a.Images.(TrainingICCReader)
	if !okay {
		return "", ErrInvalidTrainingDraft
	}
	documents, err := a.Documents.Load(ctx, start, candidate)
	if err != nil {
		return "", err
	}
	if documents.Binding.AssetVersionID != candidate.AssetVersionID ||
		documents.Binding.Source.SHA256 != candidate.SourceContentSHA {
		return "", ErrInvalidTrainingExample
	}
	lock, err := reader.DiscoverICC(ctx,
		auth.Identity{UserID: start.ActorID, SessionID: start.AuthSessionID},
		start.TenantID, documents.Binding)
	if err != nil {
		return "", err
	}
	if !validSHA(lock) {
		return "", ErrInvalidTrainingDraft
	}
	return lock, nil
}

// Assemble resolves a single locked-inventory event to immutable training
// evidence. Every reader must return a scoped, byte-verified dependency;
// BuildTrainingCaseFromDescriptor rechecks the compiled target and refs.
func (a TrainingCaseAssembler) Assemble(ctx context.Context,
	start TrainingStart, candidate TrainingCandidate, runID uuid.UUID,
	iccLock, modelLock string) (TrainingCaseEvidence, error) {
	var empty TrainingCaseEvidence
	if a.Documents == nil || a.Recipes == nil || a.Images == nil ||
		a.Descriptors == nil || a.Faces == nil || a.Protection == nil ||
		start.TenantID == uuid.Nil || start.ProjectID == uuid.Nil ||
		start.ActorID == uuid.Nil || start.AuthSessionID == uuid.Nil ||
		candidate.EventID == uuid.Nil || candidate.AssetVersionID == uuid.Nil ||
		runID == uuid.Nil || !validSHA(iccLock) || !validSHA(modelLock) {
		return empty, ErrInvalidTrainingDraft
	}
	who := auth.Identity{UserID: start.ActorID, SessionID: start.AuthSessionID}
	documents, err := a.Documents.Load(ctx, start, candidate)
	if err != nil {
		return empty, err
	}
	if documents.Binding.AssetVersionID != candidate.AssetVersionID ||
		documents.Binding.Source.SHA256 != candidate.SourceContentSHA {
		return empty, ErrInvalidTrainingExample
	}
	compiled, session, err := a.Recipes.LoadPair(ctx, who, start.TenantID,
		documents, iccLock, modelLock)
	if err != nil {
		return empty, err
	}
	image, err := a.Images.Load(ctx, who, start.TenantID,
		documents.Binding, iccLock)
	if err != nil {
		return empty, err
	}
	if image.Document.Canvas.SourceSHA256 != candidate.SourceLineageSHA ||
		image.PixelsCount < 1 {
		return empty, ErrInvalidTrainingExample
	}
	descriptor, err := a.Descriptors.Load(ctx, who, start.TenantID,
		start.ProjectID, documents.Binding, modelLock, session)
	if err != nil {
		return empty, err
	}
	if _, err := a.Faces.Load(ctx, who, start.TenantID,
		documents.Binding, descriptor.Faces); err != nil {
		return empty, err
	}
	var protection struct {
		ProtectedMask *v2input.Ref `json:"protected_mask"`
	}
	if json.Unmarshal(documents.ApprovedBody, &protection) != nil {
		return empty, ErrInvalidTrainingDraft
	}
	var protected v2input.Ref
	if protection.ProtectedMask != nil {
		pinned, okay := a.Protection.(interface {
			SelectPinned(context.Context, auth.Identity, TrainingStart,
				TrainingCandidate, TrainingImage, v2input.Ref) (
				v2input.Ref, TrainingMask, error)
		})
		if !okay {
			return empty, ErrInvalidTrainingDraft
		}
		protected, _, err = pinned.SelectPinned(ctx, who, start,
			candidate, image, *protection.ProtectedMask)
	} else {
		protected, _, err = a.Protection.SelectProtected(ctx, who,
			start, candidate, image)
	}
	if err != nil {
		return empty, err
	}
	render := TrainingRenderCase{Binding: documents.Binding,
		WorkingImage: documents.Binding.Source, Protected: protected}
	return BuildTrainingCaseFromDescriptor(runID, candidate, descriptor,
		render, compiled, iccLock, modelLock)
}

var _ TrainingDocumentReader = SQLTrainingDocumentSource{}
var _ TrainingRecipeReader = TrainingRecipeSource{}
var _ TrainingImageReader = TrainingImageSource{}
var _ TrainingDescriptorReader = TrainingDescriptorSource{}
var _ TrainingFacesReader = TrainingFacesSource{}
var _ TrainingProtectedSelector = TrainingMaskSelector{}
