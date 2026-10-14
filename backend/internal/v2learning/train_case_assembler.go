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

type TrainingRegionReader interface {
	LoadRegions(context.Context, auth.Identity, TrainingStart, TrainingCandidate, TrainingImage, v2input.Ref) ([]TrainingRegionMask, error)
}

type TrainingBackgroundReader interface {
	LoadBackground(context.Context, auth.Identity, TrainingStart, v2input.Ref, string) (*v2draft.FrozenBackgroundAsset, error)
}

type TrainingReviewedDefectPreparer interface {
	Prepare(context.Context, TrainingStart, TrainingCandidate, uuid.UUID,
		TrainingRenderCase, CompiledTrainingRecipes, TrainingImage,
		TrainingMask) (*PreparedReviewedDefectHoldout, error)
}

type TrainingReviewedDustPreparer interface {
	Prepare(context.Context, TrainingStart, TrainingCandidate, uuid.UUID,
		TrainingRenderCase, CompiledTrainingRecipes, TrainingImage,
		TrainingMask) (*PreparedReviewedDustHoldout, error)
}

type TrainingPortraitDetailPreparer interface {
	Prepare(context.Context, TrainingStart, TrainingCandidate, uuid.UUID,
		TrainingRenderCase, CompiledTrainingRecipes, TrainingImage,
		TrainingMask) (*PreparedPortraitDetailHoldout, error)
}

type TrainingSeriesPreparer interface {
	Prepare(context.Context, TrainingStart, TrainingCandidate, uuid.UUID,
		TrainingRenderCase, CompiledTrainingRecipes, TrainingImage,
		TrainingMask) (*PreparedSeriesHoldout, error)
}

type TrainingCaseAssembler struct {
	Documents       TrainingDocumentReader
	Recipes         TrainingRecipeReader
	Images          TrainingImageReader
	Descriptors     TrainingDescriptorReader
	Faces           TrainingFacesReader
	Protection      TrainingProtectedSelector
	Regions         TrainingRegionReader
	Backgrounds     TrainingBackgroundReader
	ReviewedDefects TrainingReviewedDefectPreparer
	ReviewedDust    TrainingReviewedDustPreparer
	PortraitDetail  TrainingPortraitDetailPreparer
	Series          TrainingSeriesPreparer
}

func ownTrainingAssemblyCandidate(candidate TrainingCandidate) TrainingCandidate {
	candidate.ChangedPaths = append([]string(nil), candidate.ChangedPaths...)
	if candidate.ApprovalID != nil {
		value := *candidate.ApprovalID
		candidate.ApprovalID = &value
	}
	return candidate
}
func ownTrainingAssemblyDocuments(documents TrainingEditingDocuments) TrainingEditingDocuments {
	documents.BaseBody = append([]byte(nil), documents.BaseBody...)
	documents.ApprovedBody = append([]byte(nil), documents.ApprovedBody...)
	return documents
}
func ownTrainingAssemblyRecipes(compiled CompiledTrainingRecipes) CompiledTrainingRecipes {
	compiled.BaseBody = append([]byte(nil), compiled.BaseBody...)
	compiled.ApprovedBody = append([]byte(nil), compiled.ApprovedBody...)
	return compiled
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
	if err := ctx.Err(); err != nil {
		return "", err
	}
	documents, err := a.Documents.Load(ctx, start, ownTrainingAssemblyCandidate(candidate))
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	documents = ownTrainingAssemblyDocuments(documents)
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
	if err := ctx.Err(); err != nil {
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
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	candidate = ownTrainingAssemblyCandidate(candidate)
	who := auth.Identity{UserID: start.ActorID, SessionID: start.AuthSessionID}
	documents, err := a.Documents.Load(ctx, start, ownTrainingAssemblyCandidate(candidate))
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	documents = ownTrainingAssemblyDocuments(documents)
	if documents.Binding.AssetVersionID != candidate.AssetVersionID ||
		documents.Binding.Source.SHA256 != candidate.SourceContentSHA {
		return empty, ErrInvalidTrainingExample
	}
	compiled, session, err := a.Recipes.LoadPair(ctx, who, start.TenantID,
		ownTrainingAssemblyDocuments(documents), iccLock, modelLock)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	compiled = ownTrainingAssemblyRecipes(compiled)
	if session != nil {
		value := *session
		session = &value
	}
	image, err := a.Images.Load(ctx, who, start.TenantID,
		documents.Binding, iccLock)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
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
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if _, err := a.Faces.Load(ctx, who, start.TenantID,
		documents.Binding, descriptor.Faces); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	var protection struct {
		ProtectedMask *v2input.Ref `json:"protected_mask"`
	}
	if json.Unmarshal(documents.ApprovedBody, &protection) != nil {
		return empty, ErrInvalidTrainingDraft
	}
	var protected v2input.Ref
	var protectedMask TrainingMask
	if protection.ProtectedMask != nil {
		pinned, okay := a.Protection.(interface {
			SelectPinned(context.Context, auth.Identity, TrainingStart,
				TrainingCandidate, TrainingImage, v2input.Ref) (
				v2input.Ref, TrainingMask, error)
		})
		if !okay {
			return empty, ErrInvalidTrainingDraft
		}
		protected, protectedMask, err = pinned.SelectPinned(ctx, who, start,
			ownTrainingAssemblyCandidate(candidate), image, *protection.ProtectedMask)
	} else {
		protected, protectedMask, err = a.Protection.SelectProtected(ctx, who,
			start, ownTrainingAssemblyCandidate(candidate), image)
	}
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	var regions []TrainingRegionMask
	if a.Regions != nil {
		regions, err = a.Regions.LoadRegions(ctx, who, start, ownTrainingAssemblyCandidate(candidate), image, compiled.Base.BaseStyle)
		if err != nil {
			return empty, err
		}
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		regions = append([]TrainingRegionMask(nil), regions...)
	}
	var background *v2draft.FrozenBackgroundAsset
	if a.Backgrounds != nil {
		background, err = a.Backgrounds.LoadBackground(ctx, who, start, compiled.Base.BaseStyle, iccLock)
		if err != nil {
			return empty, err
		}
		if err := ctx.Err(); err != nil {
			return empty, err
		}
	}
	render := TrainingRenderCase{Binding: documents.Binding,
		WorkingImage: documents.Binding.Source, Protected: protected, RegionMasks: regions, BackgroundAsset: background}
	result, err := BuildTrainingCaseFromDescriptor(runID, candidate,
		descriptor, render, compiled, iccLock, modelLock)
	if err != nil {
		return empty, err
	}
	if a.ReviewedDefects != nil {
		prepared, err := a.ReviewedDefects.Prepare(ctx, start,
			ownTrainingAssemblyCandidate(candidate), runID,
			result.Resolved.Render, compiled, image, protectedMask)
		if err != nil {
			return empty, err
		}
		if prepared != nil {
			owned := *prepared
			owned.Body = append([]byte(nil), owned.Body...)
			result.Defect = &owned
		}
	}
	if a.ReviewedDust != nil {
		prepared, err := a.ReviewedDust.Prepare(ctx, start,
			ownTrainingAssemblyCandidate(candidate), runID,
			result.Resolved.Render, compiled, image, protectedMask)
		if err != nil {
			return empty, err
		}
		if prepared != nil {
			if result.Defect != nil {
				return empty, ErrInvalidTrainingDraft
			}
			owned := *prepared
			owned.Body = append([]byte(nil), owned.Body...)
			result.Dust = &owned
		}
	}
	if a.PortraitDetail != nil {
		prepared, err := a.PortraitDetail.Prepare(ctx, start,
			ownTrainingAssemblyCandidate(candidate), runID,
			result.Resolved.Render, compiled, image, protectedMask)
		if err != nil {
			return empty, err
		}
		if prepared != nil {
			if result.Defect != nil || result.Dust != nil {
				return empty, ErrInvalidTrainingDraft
			}
			owned := *prepared
			owned.Body = append([]byte(nil), owned.Body...)
			owned.Document.MaskArtifacts = append([]v2input.Ref(nil),
				owned.Document.MaskArtifacts...)
			result.Detail = &owned
		}
	}
	if a.Series != nil {
		prepared, err := a.Series.Prepare(ctx, start,
			ownTrainingAssemblyCandidate(candidate), runID,
			result.Resolved.Render, compiled, image, protectedMask)
		if err != nil {
			return empty, err
		}
		if prepared != nil {
			if result.Defect != nil || result.Dust != nil || result.Detail != nil {
				return empty, ErrInvalidTrainingDraft
			}
			owned := *prepared
			owned.Body = append([]byte(nil), owned.Body...)
			result.Series = &owned
		}
	}
	return result, nil
}

var _ TrainingDocumentReader = SQLTrainingDocumentSource{}
var _ TrainingRecipeReader = TrainingRecipeSource{}
var _ TrainingImageReader = TrainingImageSource{}
var _ TrainingDescriptorReader = TrainingDescriptorSource{}
var _ TrainingFacesReader = TrainingFacesSource{}
var _ TrainingProtectedSelector = TrainingMaskSelector{}
