package v2sessions

import (
	"context"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2portrait"
)

// VerifyCalibratedDonorFaces derives the threshold from a complete pinned
// Calibration. Catalog scope, live qualification and consent remain admission
// gates; a document's qualified flag alone is not permission to run a task.
func VerifyCalibratedDonorFaces(ctx context.Context, loaded v2draft.LoadedDonorTaskPlan,
	confirmationRaw, sourceFacesRaw, donorFacesRaw, calibrationRaw []byte,
	calibrationSHA, modelLockSHA, datasetSHA string) (v2portrait.DonorFaceGeometry, error) {
	var zero v2portrait.DonorFaceGeometry
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	minEdge, err := CalibrationMinFaceEdge(calibrationRaw, calibrationSHA, modelLockSHA, datasetSHA)
	if err != nil {
		return zero, err
	}
	return v2draft.VerifyLoadedDonorFaces(ctx, loaded, confirmationRaw, sourceFacesRaw, donorFacesRaw, float64(minEdge))
}
