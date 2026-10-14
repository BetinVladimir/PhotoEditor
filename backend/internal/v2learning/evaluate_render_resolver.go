package v2learning

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"photoeditor/backend/internal/canonical"

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
type FrozenEvaluateDefectReader interface {
	Load(context.Context, LearningEvaluateLease, uuid.UUID,
		TrainingRenderCase) (FrozenEvaluateDefectInputs, error)
}
type FrozenEvaluateDustReader interface {
	Load(context.Context, LearningEvaluateLease, uuid.UUID,
		TrainingRenderCase) (FrozenEvaluateDustInputs, error)
}
type FrozenEvaluateDetailReader interface {
	Load(context.Context, LearningEvaluateLease, uuid.UUID,
		TrainingRenderCase) (FrozenEvaluateDetailInputs, error)
}
type FrozenEvaluateSeriesReader interface {
	Load(context.Context, LearningEvaluateLease, uuid.UUID,
		TrainingRenderCase) (FrozenEvaluateSeriesInputs, error)
}

type FrozenLearningRenderResolver struct {
	Roots    FrozenEvaluateRootReader
	Images   FrozenEvaluateImageReader
	Masks    FrozenEvaluateMaskReader
	Styles   FrozenEvaluateStyleReader
	Consents FrozenEvaluateConsentPinner
	Defects  FrozenEvaluateDefectReader
	Dust     FrozenEvaluateDustReader
	Detail   FrozenEvaluateDetailReader
	Series   FrozenEvaluateSeriesReader
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owned, err := ownEvaluationManifest(manifest)
	if err != nil {
		return nil, err
	}
	manifest = owned
	result := make([]LearningRenderInputs, 0)
	for _, item := range manifest.Examples {
		if item.Split != "holdout" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		caseRef := item.RenderCase
		imageRaw, _, err := r.loadRoot(ctx, lease, caseRef.WorkingImage,
			"WorkingImage")
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		image, err := ParseFrozenEvaluationImage(imageRaw, caseRef.Binding)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		imageInputs, err := r.Images.Resolve(ctx, lease, caseRef.WorkingImage,
			imageRaw, caseRef.Binding)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if imageInputs.Alpha != nil {
			alpha := *imageInputs.Alpha
			imageInputs.Alpha = &alpha
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
		baseRaw, _, err := r.loadRoot(ctx, lease, caseRef.BaseRecipe,
			"FrozenRecipe")
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		approvedRaw, _, err := r.loadRoot(ctx, lease,
			caseRef.ApprovedRecipe, "FrozenRecipe")
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		style, err := ValidateFrozenRecipePair(baseRaw, approvedRaw,
			caseRef, manifest)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		styleRaw, err := r.Styles.Load(ctx, lease, style)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		styleRaw = append([]byte(nil), styleRaw...)
		var defectProof *FrozenEvaluateDefectInputs
		if caseRef.DefectEvidence != nil {
			if r.Defects == nil {
				return nil, ErrInvalidTrainingDraft
			}
			defect, err := r.Defects.Load(ctx, lease, item.EventID, caseRef)
			if err != nil {
				return nil, err
			}
			defectProof = &defect
		}
		var dustProof *FrozenEvaluateDustInputs
		if caseRef.DustEvidence != nil {
			if r.Dust == nil || defectProof != nil {
				return nil, ErrInvalidTrainingDraft
			}
			dust, err := r.Dust.Load(ctx, lease, item.EventID, caseRef)
			if err != nil {
				return nil, err
			}
			dustProof = &dust
		}
		var detailProof *FrozenEvaluateDetailInputs
		if caseRef.DetailEvidence != nil {
			if r.Detail == nil || defectProof != nil || dustProof != nil {
				return nil, ErrInvalidTrainingDraft
			}
			detail, err := r.Detail.Load(ctx, lease, item.EventID, caseRef)
			if err != nil {
				return nil, err
			}
			detailProof = &detail
		}
		var seriesProof *FrozenEvaluateSeriesInputs
		if caseRef.SeriesEvidence != nil {
			if r.Series == nil || defectProof != nil || dustProof != nil ||
				detailProof != nil {
				return nil, ErrInvalidTrainingDraft
			}
			series, err := r.Series.Load(ctx, lease, item.EventID, caseRef)
			if err != nil {
				return nil, err
			}
			seriesProof = &series
		}
		if err := validateFrozenHoldoutPipeline(styleRaw,
			defectProof != nil, dustProof != nil,
			detailProof != nil, seriesProof != nil); err != nil {
			return nil, err
		}
		if err := validateFrozenBackgroundBinding(styleRaw, caseRef.BackgroundAsset); err != nil {
			return nil, err
		}
		if caseRef.BackgroundAsset != nil {
			verifier, ok := r.Styles.(FrozenEvaluateBackgroundVerifier)
			if !ok {
				return nil, ErrInvalidTrainingDraft
			}
			if !validTrainingBackgroundPin(caseRef.BackgroundAsset, manifest.ICCLockSHA, style) {
				return nil, ErrInvalidTrainingDraft
			}
			pin := *caseRef.BackgroundAsset
			if pin.AssumeProfile != nil {
				value := *pin.AssumeProfile
				pin.AssumeProfile = &value
			}
			if err := verifier.VerifyBackground(ctx, lease, pin); err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		maskRaw, _, err := r.loadRoot(ctx, lease, caseRef.Protected,
			"MaskRef")
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		protected, err := r.Masks.Resolve(ctx, lease, caseRef.Protected,
			maskRaw, image)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		expectedProtected, err := ParseFrozenProtectedMask(maskRaw, image)
		if err != nil || protected != expectedProtected {
			return nil, ErrInvalidTrainingDraft
		}
		faceRaw, _, err := r.loadRoot(ctx, lease, caseRef.Faces,
			"FaceObservations")
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
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
			body, _, err := r.loadRoot(ctx, lease, region.Mask, "MaskRef")
			if err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			child, err := r.Masks.ResolveEffective(ctx, lease,
				region.Mask, body, image)
			if err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
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
			effectiveRaw, _, err := r.loadRoot(ctx, lease,
				*caseRef.EffectiveMask, "MaskRef")
			if err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			child, err := r.Masks.ResolveEffective(ctx, lease,
				*caseRef.EffectiveMask, effectiveRaw, image)
			if err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			expected, err := ParseFrozenEffectiveMask(effectiveRaw, image)
			if err != nil || child != expected {
				return nil, ErrInvalidTrainingDraft
			}
			consentRaw, _, err := r.loadRoot(ctx, lease,
				*caseRef.ConsentSnapshot, "ConsentSnapshot")
			if err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
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
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result = append(result, LearningRenderInputs{
			EventID: item.EventID, WorkingImage: caseRef.WorkingImage,
			DefectEvidence: caseRef.DefectEvidence,
			DefectProof:    defectProof,
			DustEvidence:   caseRef.DustEvidence,
			DustProof:      dustProof,
			DetailEvidence: caseRef.DetailEvidence,
			DetailProof:    detailProof,
			SeriesEvidence: caseRef.SeriesEvidence,
			SeriesProof:    seriesProof,
			PixelCount:     imageInputs.PixelCount, Pixels: imageInputs.Pixels,
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

var _ LearningEvaluateRenderResolver = FrozenLearningRenderResolver{}
var _ FrozenEvaluateRootReader = SQLLearningEvaluateRootSource{}
var _ FrozenEvaluateImageReader = SQLLearningEvaluateImageSource{}
var _ FrozenEvaluateMaskReader = SQLLearningEvaluateMaskSource{}
var _ FrozenEvaluateStyleReader = SQLLearningEvaluateStyleSource{}
var _ FrozenEvaluateConsentPinner = SQLLearningEvaluateConsentPinner{}
var _ FrozenEvaluateDefectReader = SQLFrozenEvaluateDefectReader{}
var _ FrozenEvaluateDustReader = SQLFrozenEvaluateDustReader{}
var _ FrozenEvaluateDetailReader = SQLFrozenEvaluateDetailReader{}

// Root readers may reuse their transport buffers on the next IO. Retained
// frozen recipe/face bytes must remain owned by this resolution attempt.
func (r FrozenLearningRenderResolver) loadRoot(ctx context.Context,
	lease LearningEvaluateLease, ref v2input.Ref, contract string) (
	[]byte, FrozenRootProvenance, error) {
	if err := ctx.Err(); err != nil {
		return nil, FrozenRootProvenance{}, err
	}
	body, provenance, err := r.Roots.Load(ctx, lease, ref, contract)
	if err != nil {
		return nil, FrozenRootProvenance{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, FrozenRootProvenance{}, err
	}
	return append([]byte(nil), body...), provenance, nil
}

func ownEvaluationManifest(manifest TrainingManifest) (TrainingManifest, error) {
	if !validFrozenEvaluationManifest(manifest) {
		return TrainingManifest{}, ErrInvalidTrainingDraft
	}
	body, err := canonical.Marshal(manifest)
	if err != nil || len(body) < 2 || len(body) > 8*1048576 {
		return TrainingManifest{}, ErrInvalidTrainingDraft
	}
	var owned TrainingManifest
	if err := json.Unmarshal(body, &owned); err != nil {
		return TrainingManifest{}, ErrInvalidTrainingDraft
	}
	return owned, nil
}
