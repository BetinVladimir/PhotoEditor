package v2learning

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// TrainingRecipeSource resolves the scalar recipe closure through the same
// scoped catalog used by V2 approval. It never interprets a bare client ref
// as a trusted correction, StyleV2, learning model, or frame descriptor.
type TrainingRecipeSource struct {
	Catalog   v2draft.ArtifactCatalog
	Objects   LearningEvaluateObjectReader
	Learning  TrainingLearningReader
	ProjectID uuid.UUID
}

type TrainingLearningReader interface {
	ReadEligibleLearningProfile(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) (v2draft.EligibleLearningModel, error)
	ReadFrameDescriptor(context.Context, auth.Identity, uuid.UUID, uuid.UUID, v2input.SessionVersionBinding, string, *v2draft.ResolvedCorrection) (v2draft.PinnedFrameDescriptor, error)
}

func (s TrainingRecipeSource) LoadPair(ctx context.Context,
	who auth.Identity, tenantID uuid.UUID,
	documents TrainingEditingDocuments, iccLock, modelLock string) (
	CompiledTrainingRecipes, *v2draft.ResolvedCorrection, error) {
	var empty CompiledTrainingRecipes
	if s.Catalog == nil || s.Objects == nil || tenantID == uuid.Nil ||
		!validSHA(iccLock) || !validSHA(modelLock) ||
		documents.Binding.AssetVersionID == uuid.Nil ||
		!validManifestRef(documents.Binding.Source) ||
		!validManifestRef(documents.BaseStyle) ||
		len(documents.BaseBody) < 2 || len(documents.ApprovedBody) < 2 {
		return empty, nil, ErrInvalidTrainingDraft
	}
	if catalog, ok := s.Catalog.(v2draft.SQLArtifactCatalog); ok &&
		(catalog.ProjectID != s.ProjectID ||
			catalog.AssetVersionID != documents.Binding.AssetVersionID) {
		return empty, nil, ErrInvalidTrainingDraft
	}
	resolver := v2draft.ClosureResolver{Catalog: s.Catalog,
		Objects: s.Objects}
	layers := TrainingLayerSource{Catalog: s.Catalog,
		Objects: s.Objects}
	load := func(body []byte, documentID uuid.UUID) (TrainingRecipeEvidence,
		*v2draft.ResolvedCorrection, error) {
		var fields struct {
			Overrides         []v2input.Ref `json:"overrides"`
			LearningVersionID *uuid.UUID    `json:"learning_version_id"`
			LayerStack        *v2input.Ref  `json:"layer_stack"`
		}
		if json.Unmarshal(body, &fields) != nil ||
			fields.Overrides == nil {
			return TrainingRecipeEvidence{}, nil, ErrInvalidTrainingDraft
		}
		style, styleRef, session, err := resolver.ReadResolvedScalars(ctx,
			who, tenantID, body, documents.Binding)
		if err != nil {
			return TrainingRecipeEvidence{}, nil, err
		}
		if styleRef != documents.BaseStyle {
			return TrainingRecipeEvidence{}, nil, ErrInvalidTrainingDraft
		}
		evidence := TrainingRecipeEvidence{Inputs: v2draft.ResolvedRecipeInputs{
			Binding: documents.Binding, BaseStyle: styleRef,
			Style: style, Session: session,
			ICCLockSHA: iccLock, ModelLockSHA: modelLock}}
		if fields.LearningVersionID != nil {
			if s.Learning == nil || s.ProjectID == uuid.Nil ||
				documentID == uuid.Nil || *fields.LearningVersionID == uuid.Nil {
				return TrainingRecipeEvidence{}, nil, ErrInvalidTrainingDraft
			}
			model, err := s.Learning.ReadEligibleLearningProfile(ctx, who,
				tenantID, s.ProjectID, documentID, *fields.LearningVersionID)
			if err != nil {
				return TrainingRecipeEvidence{}, nil, err
			}
			if model.ICCLockSHA != iccLock || model.ModelLockSHA != modelLock ||
				!validManifestRef(model.ModelRef) ||
				!validManifestRef(model.ManifestRef) {
				return TrainingRecipeEvidence{}, nil, ErrInvalidTrainingDraft
			}
			frame, err := s.Learning.ReadFrameDescriptor(ctx, who, tenantID,
				s.ProjectID, documents.Binding, modelLock, session)
			if err != nil {
				return TrainingRecipeEvidence{}, nil, err
			}
			if frame.Features.Binding != documents.Binding ||
				!validManifestRef(frame.Features.Faces) ||
				!validManifestRef(frame.ShardRef) ||
				frame.AnalysisID == uuid.Nil || frame.Ordinal < 0 ||
				frame.Ordinal > 4999 {
				return TrainingRecipeEvidence{}, nil, ErrInvalidTrainingDraft
			}
			prediction, err := v2draft.PredictCorrection(model.Profile, frame.Features)
			if err != nil {
				return TrainingRecipeEvidence{}, nil, err
			}
			evidence.Inputs.LearningID = *fields.LearningVersionID
			evidence.Inputs.Learning = &prediction
			evidence.Inputs.LearningModelRef = model.ModelRef
			evidence.Inputs.LearningDescriptorRef = frame.ShardRef
			evidence.Inputs.LearningAnalysisID = frame.AnalysisID
			evidence.Inputs.LearningDescriptorOrdinal = frame.Ordinal
		}
		overrides, err := s.loadScalarOverrides(ctx, who, tenantID,
			fields.Overrides, styleRef, style, session,
			evidence.Inputs.Learning)
		if err != nil {
			return TrainingRecipeEvidence{}, nil, err
		}
		evidence.Inputs.Overrides = overrides
		if fields.LayerStack != nil {
			stack, active, err := layers.Load(ctx, who, tenantID,
				*fields.LayerStack, documents.Binding)
			if err != nil {
				return TrainingRecipeEvidence{}, nil, err
			}
			if active {
				return TrainingRecipeEvidence{}, nil, ErrInvalidTrainingExample
			}
			evidence.LayerStackRef = fields.LayerStack
			evidence.LayerStackBody = stack
		}
		return evidence, session, nil
	}
	base, baseSession, err := load(documents.BaseBody, documents.BaseDocumentID)
	if err != nil {
		return empty, nil, err
	}
	approved, approvedSession, err := load(documents.ApprovedBody, documents.ApprovedDocumentID)
	if err != nil {
		return empty, nil, err
	}
	if (baseSession == nil) != (approvedSession == nil) ||
		(baseSession != nil && *baseSession != *approvedSession) {
		return empty, nil, ErrInvalidTrainingDraft
	}
	compiled, err := CompileTrainingRecipes(documents, base, approved)
	if err != nil {
		return empty, nil, err
	}
	return compiled, approvedSession, nil
}
