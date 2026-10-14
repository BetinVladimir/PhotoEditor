package provenance

import (
	"bytes"
	"context"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type SigningRenderMaskCounter interface {
	CountSigningRenderMask(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, review.MaskRef) (int64, error)
}

// Evidence remains private and invocation-local. Counts prove binary mask bytes,
// not model attribution, complete parent ancestry, recomposition or human review.
type SigningRenderPixelEvidence struct {
	root                           v2input.Ref
	recipe                         v2draft.FrozenRenderRecipe
	result                         v2draft.ImageResultDocument
	image                          review.WorkingImage
	changedPixels, generatedPixels int64
}

func (e SigningRenderPixelEvidence) Root() v2input.Ref      { return e.root }
func (e SigningRenderPixelEvidence) ChangedPixels() int64   { return e.changedPixels }
func (e SigningRenderPixelEvidence) GeneratedPixels() int64 { return e.generatedPixels }

func ReadSigningRenderPixelEvidence(ctx context.Context, adoption SigningRenderAdoptionReader, documents SigningTaskDocumentReader, counter SigningRenderMaskCounter, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask) (SigningRenderPixelEvidence, error) {
	var zero SigningRenderPixelEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if counter == nil {
		return zero, ErrInvalid
	}
	recipe, err := ReadSigningRenderRecipe(ctx, documents, who, ready, task)
	if err != nil {
		return zero, err
	}
	root, result, err := ReadSigningRenderResult(ctx, adoption, documents, who, ready, task)
	if err != nil {
		return zero, err
	}
	readImage := func(reference v2input.Ref) (review.WorkingImage, []byte, error) {
		raw, err := documents.ReadSigningTaskDocument(ctx, who, ready, task, reference, 1<<20)
		if err != nil {
			return review.WorkingImage{}, nil, err
		}
		if !pinned(raw, reference, 1<<20) {
			return review.WorkingImage{}, nil, ErrInvalid
		}
		owned := append([]byte(nil), raw...)
		image, err := review.ParseWorkingImage(owned)
		return image, owned, err
	}
	image, imageRaw, err := readImage(result.WorkingImage)
	if err != nil {
		return zero, err
	}
	source, sourceRaw, err := readImage(recipe.Scalars.Source.Source)
	if err != nil {
		return zero, err
	}
	canvasHash, err := canonical.Hash(source.Canvas)
	if err != nil || canvasHash != recipe.Scalars.Source.CanvasHash {
		return zero, ErrInvalid
	}
	if image.DType != "float32" || source.DType != image.DType || source.Canvas != image.Canvas || source.Color != image.Color || !bytes.Equal(source.TransformChain, image.TransformChain) {
		return zero, ErrInvalid
	}
	changed, err := ReadSigningRenderMask(ctx, documents, who, ready, task, image, result.ChangedMask, "changed")
	if err != nil {
		return zero, err
	}
	changedCount, err := counter.CountSigningRenderMask(ctx, who, ready, task, result.ChangedMask, changed)
	if err != nil {
		return zero, err
	}
	generatedCount := int64(0)
	if result.GeneratedMask != nil {
		generated, err := ReadSigningRenderMask(ctx, documents, who, ready, task, image, *result.GeneratedMask, "generated")
		if err != nil {
			return zero, err
		}
		generatedCount, err = counter.CountSigningRenderMask(ctx, who, ready, task, *result.GeneratedMask, generated)
		if err != nil {
			return zero, err
		}
	}
	limit := image.Canvas.Width * image.Canvas.Height
	if changedCount < 0 || changedCount > limit || generatedCount < 0 || generatedCount > limit {
		return zero, ErrInvalid
	}
	_, currentImage, err := readImage(result.WorkingImage)
	if err != nil {
		return zero, err
	}
	_, currentSource, err := readImage(recipe.Scalars.Source.Source)
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(imageRaw, currentImage) || !bytes.Equal(sourceRaw, currentSource) {
		return zero, ErrInvalid
	}
	repeated, err := adoption.ReadSigningAdoptedRenderRoot(ctx, who, ready, task)
	if err != nil {
		return zero, err
	}
	if repeated != root {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return SigningRenderPixelEvidence{root: root, recipe: recipe, result: result, image: image, changedPixels: changedCount, generatedPixels: generatedCount}, nil
}

var _ SigningRenderMaskCounter = BoundSigningRenderMaskCounter{}
