package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/editor"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// RenderLearningReader proves active model eligibility/consent and adopted
// descriptor provenance. Service implements this using current SQL authority.
type RenderLearningReader interface {
	ReadEligibleLearningProfile(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) (EligibleLearningModel, error)
	ReadFrameDescriptor(context.Context, auth.Identity, uuid.UUID, uuid.UUID, v2input.SessionVersionBinding, string, *ResolvedCorrection) (PinnedFrameDescriptor, error)
}

type RenderRecipeSource struct {
	Resolver  ClosureResolver
	Learning  RenderLearningReader
	ProjectID uuid.UUID
}

// LoadedRenderRecipe owns all document bytes required for full compilation.
// It is not a client wire format or a substitute for live task admission.
type LoadedRenderRecipe struct {
	Authored       []byte
	Inputs         ResolvedRecipeInputs
	StyleBody      []byte
	OverrideBodies [][]byte
	LayerBody      []byte
	SourceSHA      string
}

// Load verifies the authored closure and pins the source, style, correction,
// complete ordered overrides, layer stack and optional learning prediction.
// ICC/model locks come from the qualified server profile, never request JSON.
func (s RenderRecipeSource) Load(ctx context.Context, who auth.Identity, tenantID, documentID uuid.UUID,
	authored []byte, binding v2input.SessionVersionBinding, iccLock, modelLock string) (LoadedRenderRecipe, error) {
	invalid := func() (LoadedRenderRecipe, error) { return LoadedRenderRecipe{}, v2input.ErrInvalid }
	if err := ctx.Err(); err != nil {
		return LoadedRenderRecipe{}, err
	}
	if s.ProjectID == uuid.Nil || documentID == uuid.Nil || tenantID == uuid.Nil ||
		!correctionDigest(iccLock) || !correctionDigest(modelLock) || len(authored) < 2 || len(authored) > 1048576 {
		return invalid()
	}
	if catalog, ok := s.Resolver.Catalog.(SQLArtifactCatalog); ok &&
		(catalog.ProjectID != s.ProjectID || catalog.AssetVersionID != binding.AssetVersionID) {
		return invalid()
	}
	raw := append([]byte(nil), authored...)
	wire, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(wire, raw) || v2input.ValidateEditingRecipeV3(raw, binding) != nil {
		return invalid()
	}
	var fields struct {
		Overrides []v2input.Ref `json:"overrides"`
		Learning  *uuid.UUID    `json:"learning_version_id"`
		Layers    *v2input.Ref  `json:"layer_stack"`
	}
	if json.Unmarshal(raw, &fields) != nil {
		return invalid()
	}
	// This traversal validates nested reference provenance, including source
	// canvas and override mask lineage, before extracting any scalar evidence.
	baseline, styleRef, session, err := s.Resolver.ReadResolvedScalars(ctx, who, tenantID, raw, binding)
	if err != nil {
		return LoadedRenderRecipe{}, err
	}
	read := func(path, contract string, ref v2input.Ref) ([]byte, error) {
		return s.Resolver.readScopedContract(ctx, who, tenantID, LayerRef{Path: path, Contract: contract, Ref: ref})
	}
	sourceRaw, err := read("EditingRecipeV3.source.source", "WorkingImage", binding.Source)
	if err != nil {
		return LoadedRenderRecipe{}, err
	}
	image, err := review.ParseWorkingImage(sourceRaw)
	if err != nil || image.Color.ICCSHA256 != iccLock {
		return invalid()
	}
	canvasHash, err := canonical.Hash(image.Canvas)
	if err != nil || canvasHash != binding.CanvasHash {
		return invalid()
	}
	styleRaw, err := read("EditingRecipeV3.base_style", "StyleV2", styleRef)
	if err != nil {
		return LoadedRenderRecipe{}, err
	}
	rereadBaseline, err := DecodeStyleBaseline(styleRaw)
	if err != nil || rereadBaseline != baseline {
		return invalid()
	}
	out := LoadedRenderRecipe{Authored: raw, StyleBody: styleRaw, SourceSHA: image.Canvas.SourceSHA256,
		Inputs: ResolvedRecipeInputs{Binding: binding, BaseStyle: styleRef, Style: baseline, Session: session, ICCLockSHA: iccLock, ModelLockSHA: modelLock}, OverrideBodies: make([][]byte, 0, len(fields.Overrides))}
	if fields.Learning != nil {
		if s.Learning == nil {
			return LoadedRenderRecipe{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
		}
		model, err := s.Learning.ReadEligibleLearningProfile(ctx, who, tenantID, s.ProjectID, documentID, *fields.Learning)
		if err != nil {
			return LoadedRenderRecipe{}, err
		}
		if model.ICCLockSHA != iccLock || model.ModelLockSHA != modelLock || !isDocumentRef(model.ModelRef) || !isDocumentRef(model.ManifestRef) {
			return invalid()
		}
		var ownedSession *ResolvedCorrection
		if session != nil {
			value := *session
			ownedSession = &value
		}
		frame, err := s.Learning.ReadFrameDescriptor(ctx, who, tenantID, s.ProjectID, binding, modelLock, ownedSession)
		if err != nil {
			return LoadedRenderRecipe{}, err
		}
		if frame.Features.Binding != binding || !isDocumentRef(frame.Features.Faces) || !isDocumentRef(frame.ShardRef) || frame.AnalysisID == uuid.Nil || frame.Ordinal < 0 || frame.Ordinal > 4999 {
			return invalid()
		}
		prediction, err := PredictCorrection(model.Profile, frame.Features)
		if err != nil {
			return invalid()
		}
		out.Inputs.LearningID = *fields.Learning
		out.Inputs.Learning = &prediction
		out.Inputs.LearningModelRef = model.ModelRef
		out.Inputs.LearningDescriptorRef = frame.ShardRef
		out.Inputs.LearningAnalysisID = frame.AnalysisID
		out.Inputs.LearningDescriptorOrdinal = frame.Ordinal
	}
	out.Inputs.Overrides = make([]ResolvedCorrection, 0, len(fields.Overrides))
	for i, ref := range fields.Overrides {
		body, err := read("EditingRecipeV3.overrides["+strconv.Itoa(i)+"]", "Override", ref)
		if err != nil {
			return LoadedRenderRecipe{}, err
		}
		if editor.ValidateOverride(body, out.SourceSHA) != nil {
			return invalid()
		}
		out.OverrideBodies = append(out.OverrideBodies, body)
		// OverrideV1 values are absolute whole-module replacements, applied by
		// prepare_render_style. Their additive contribution here is zero. Deriving
		// a delta from those absolute values would apply the same edit twice.
		out.Inputs.Overrides = append(out.Inputs.Overrides, ResolvedCorrection{Ref: ref})
	}
	if fields.Layers != nil {
		out.LayerBody, err = read("EditingRecipeV3.layer_stack", "LayerStack", *fields.Layers)
		if err != nil {
			return LoadedRenderRecipe{}, err
		}
	}
	if _, err := BuildFrozenRenderRecipeFromDocuments(raw, out.Inputs, out.LayerBody, styleRaw, out.OverrideBodies, out.SourceSHA); err != nil {
		return invalid()
	}
	if _, err := SelectRenderBackgroundAsset(raw, out.Inputs, styleRaw, out.OverrideBodies, out.SourceSHA); err != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return LoadedRenderRecipe{}, err
	}
	return out, nil
}
