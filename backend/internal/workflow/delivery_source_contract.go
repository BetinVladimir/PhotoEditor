package workflow

import (
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Delivery's historical EditingRecipeV3 inventory label points to the
// registered FrozenRenderRecipe artifact required by v26_render_recipe_scope.
// Validate that wrapper, its authored recipe and the exact source binding;
// accepting a bare authored recipe would bypass frozen scalar/model evidence.
func validateDeliverySourceContracts(imageRaw, recipeRaw []byte, binding v2input.SessionVersionBinding, workingICCSHA string) error {
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil || image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || image.Color.Encoding != "gamma24" || image.Color.ICCSHA256 != workingICCSHA {
		return v2input.ErrInvalid
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != binding.CanvasHash || renderDerivativeBytesSHA(imageRaw) != binding.Source.SHA256 {
		return v2input.ErrInvalid
	}
	frozen, err := v2draft.DecodeFrozenRenderRecipe(recipeRaw)
	if err != nil || frozen.Scalars.Source != binding || frozen.Scalars.ICCLockSHA != workingICCSHA {
		return v2input.ErrInvalid
	}
	return nil
}
