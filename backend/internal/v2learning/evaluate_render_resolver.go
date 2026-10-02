package v2learning

import (
	"context"

	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type FrozenEvaluateRootReader interface {
	Load(context.Context, LearningEvaluateLease, v2input.Ref,
		string) ([]byte, FrozenRootProvenance, error)
}
type FrozenEvaluateImageReader interface {
	Resolve(context.Context, LearningEvaluateLease, v2input.Ref, []byte,
		v2input.SessionVersionBinding) (FrozenEvaluationImage, error)
}
type FrozenEvaluateMaskReader interface {
	Resolve(context.Context, LearningEvaluateLease, v2input.Ref, []byte,
		review.WorkingImage) (v2input.Ref, error)
	ResolveEffective(context.Context, LearningEvaluateLease, v2input.Ref, []byte,
		review.WorkingImage) (v2input.Ref, error)
}
type FrozenEvaluateStyleReader interface {
	Load(context.Context, LearningEvaluateLease, v2input.Ref) ([]byte, error)
}
type FrozenEvaluateConsentPinner interface {
	Pin(context.Context, LearningEvaluateLease, []byte) error
}

type FrozenLearningRenderResolver struct {
	Roots    FrozenEvaluateRootReader
	Images   FrozenEvaluateImageReader
	Masks    FrozenEvaluateMaskReader
	Styles   FrozenEvaluateStyleReader
	Consents FrozenEvaluateConsentPinner
}

// Resolve reconstructs only the pinned holdout closure. Every returned ref
// has been read from a verified root or checked against its producer lineage.
func (r FrozenLearningRenderResolver) Resolve(ctx context.Context,
	lease LearningEvaluateLease,
	manifest TrainingManifest) ([]LearningRenderInputs, error) {
	if r.Roots == nil || r.Images == nil || r.Masks == nil || r.Styles == nil ||
		!validLearningEvaluateLease(lease) || manifest.SchemaVersion != 1 ||
		manifest.RendererVersion != "v2.2" {
		return nil, ErrInvalidTrainingDraft
	}
	result := make([]LearningRenderInputs, 0)
	for _, item := range manifest.Examples {
		if item.Split != "holdout" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		caseRef := item.RenderCase
		imageRaw, _, err := r.Roots.Load(ctx, lease, caseRef.WorkingImage,
			"WorkingImage")
		if err != nil {
			return nil, err
		}
		image, err := ParseFrozenEvaluationImage(imageRaw, caseRef.Binding)
		if err != nil {
			return nil, err
		}
		imageInputs, err := r.Images.Resolve(ctx, lease, caseRef.WorkingImage,
			imageRaw, caseRef.Binding)
		if err != nil {
			return nil, err
		}
		if imageInputs.Pixels != (v2input.Ref{ArtifactID: image.Pixels.ID,
			SHA256: image.Pixels.SHA256}) ||
			imageInputs.PixelCount != image.Canvas.Width*image.Canvas.Height ||
			(image.Alpha == nil) != (imageInputs.Alpha == nil) {
			return nil, ErrInvalidTrainingDraft
		}
		if image.Alpha != nil && *imageInputs.Alpha != (v2input.Ref{
			ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256}) {
			return nil, ErrInvalidTrainingDraft
		}
		baseRaw, _, err := r.Roots.Load(ctx, lease, caseRef.BaseRecipe,
			"FrozenRecipe")
		if err != nil {
			return nil, err
		}
		approvedRaw, _, err := r.Roots.Load(ctx, lease,
			caseRef.ApprovedRecipe, "FrozenRecipe")
		if err != nil {
			return nil, err
		}
		style, err := ValidateFrozenRecipePair(baseRaw, approvedRaw,
			caseRef, manifest)
		if err != nil {
			return nil, err
		}
		styleRaw, err := r.Styles.Load(ctx, lease, style)
		if err != nil {
			return nil, err
		}
		maskRaw, _, err := r.Roots.Load(ctx, lease, caseRef.Protected,
			"MaskRef")
		if err != nil {
			return nil, err
		}
		protected, err := r.Masks.Resolve(ctx, lease, caseRef.Protected,
			maskRaw, image)
		if err != nil {
			return nil, err
		}
		expectedProtected, err := ParseFrozenProtectedMask(maskRaw, image)
		if err != nil || protected != expectedProtected {
			return nil, ErrInvalidTrainingDraft
		}
		faceRaw, _, err := r.Roots.Load(ctx, lease, caseRef.Faces,
			"FaceObservations")
		if err != nil {
			return nil, err
		}
		if _, err := ValidateFrozenFaceObservations(faceRaw,
			caseRef.Binding); err != nil {
			return nil, err
		}
		regions := make([]LearningRegionPixels, 0, len(caseRef.RegionMasks))
		regionMasks := make(map[string]review.MaskRef, len(caseRef.RegionMasks))
		seenRegions := make(map[string]bool, len(caseRef.RegionMasks))
		if len(caseRef.RegionMasks) > 128 {
			return nil, ErrInvalidTrainingDraft
		}
		for _, region := range caseRef.RegionMasks {
			if len(region.RegionID) < 1 || len(region.RegionID) > 120 ||
				seenRegions[region.RegionID] {
				return nil, ErrInvalidTrainingDraft
			}
			seenRegions[region.RegionID] = true
			body, _, err := r.Roots.Load(ctx, lease, region.Mask, "MaskRef")
			if err != nil {
				return nil, err
			}
			child, err := r.Masks.ResolveEffective(ctx, lease,
				region.Mask, body, image)
			if err != nil {
				return nil, err
			}
			expected, err := ParseFrozenEffectiveMask(body, image)
			if err != nil || child != expected {
				return nil, ErrInvalidTrainingDraft
			}
			mask, err := review.ParseMaskRef(body)
			if err != nil {
				return nil, ErrInvalidTrainingDraft
			}
			regionMasks[region.RegionID] = mask
			regions = append(regions, LearningRegionPixels{
				RegionID: region.RegionID, Mask: region.Mask,
				Pixels: child})
		}
		if err := validateFrozenRegionBindings(styleRaw, caseRef.RegionMasks,
			regionMasks); err != nil {
			return nil, err
		}
		var effectivePixels *v2input.Ref
		if caseRef.EffectiveMask != nil {
			if caseRef.ConsentSnapshot == nil || r.Consents == nil {
				return nil, ErrInvalidTrainingDraft
			}
			effectiveRaw, _, err := r.Roots.Load(ctx, lease,
				*caseRef.EffectiveMask, "MaskRef")
			if err != nil {
				return nil, err
			}
			child, err := r.Masks.ResolveEffective(ctx, lease,
				*caseRef.EffectiveMask, effectiveRaw, image)
			if err != nil {
				return nil, err
			}
			expected, err := ParseFrozenEffectiveMask(effectiveRaw, image)
			if err != nil || child != expected {
				return nil, ErrInvalidTrainingDraft
			}
			consentRaw, _, err := r.Roots.Load(ctx, lease,
				*caseRef.ConsentSnapshot, "ConsentSnapshot")
			if err != nil {
				return nil, err
			}
			if err := ValidateFrozenEvaluateConsent(consentRaw, faceRaw,
				caseRef.Binding); err != nil {
				return nil, err
			}
			if err := r.Consents.Pin(ctx, lease, consentRaw); err != nil {
				return nil, err
			}
			effectivePixels = &child
		} else if caseRef.ConsentSnapshot != nil {
			return nil, ErrInvalidTrainingDraft
		}
		result = append(result, LearningRenderInputs{
			EventID: item.EventID, WorkingImage: caseRef.WorkingImage,
			PixelCount: imageInputs.PixelCount, Pixels: imageInputs.Pixels,
			Alpha: imageInputs.Alpha, Protected: caseRef.Protected,
			ProtectedPixels: protected, BaseStyle: style,
			EffectiveMask:   caseRef.EffectiveMask,
			EffectivePixels: effectivePixels,
			ConsentSnapshot: caseRef.ConsentSnapshot,
			RegionMasks:     regions,
			BaseRecipeBody:  baseRaw, ApprovedRecipeBody: approvedRaw,
		})
	}
	if len(result) < 10 {
		return nil, ErrInvalidTrainingDraft
	}
	return result, nil
}

var _ LearningEvaluateRenderResolver = FrozenLearningRenderResolver{}
var _ FrozenEvaluateRootReader = SQLLearningEvaluateRootSource{}
var _ FrozenEvaluateImageReader = SQLLearningEvaluateImageSource{}
var _ FrozenEvaluateMaskReader = SQLLearningEvaluateMaskSource{}
var _ FrozenEvaluateStyleReader = SQLLearningEvaluateStyleSource{}
var _ FrozenEvaluateConsentPinner = SQLLearningEvaluateConsentPinner{}
