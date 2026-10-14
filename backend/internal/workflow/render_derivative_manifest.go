package workflow

import "photoeditor/backend/internal/review"

// Keep the workflow API while review and adoption share one strict media codec.
type RenderDerivativeManifest = review.RenderDerivativeManifest

func ParseRenderDerivativeManifest(raw []byte, manifestRef, sourceRef, derivativeRef review.ArtifactRef, image review.WorkingImage, variant, srgbSHA string) (RenderDerivativeManifest, error) {
	return review.ParseRenderDerivativeManifest(raw, manifestRef, sourceRef, derivativeRef, image, variant, srgbSHA)
}
