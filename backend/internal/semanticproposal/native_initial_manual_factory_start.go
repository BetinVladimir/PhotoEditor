package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Project is resolved from the authenticated source/snapshot/user-mask catalog,
// never supplied as a substitute for request authority. Explicit original manual
// pixels remain separate from the uploaded intent ReferenceMask document.
// This preparation entry point cannot provide an HTTP parent receipt.
func (f SQLNativeParentRecoveryFactory) StartManualPreparation(ctx context.Context, who auth.Identity, tenantID uuid.UUID, key string, raw []byte, score float64, maxFaces int, pixels []v2input.Ref, diameter [2]float64) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := f.validateCapacity(); err != nil {
		return false, err
	}
	if err := validateInitialDetectorControls(score, maxFaces); err != nil {
		return false, err
	}
	if err := validateInitialManualPixelSelection(pixels); err != nil {
		return false, err
	}
	if err := validateInitialManualDiameter(diameter); err != nil {
		return false, err
	}
	request, err := CompileRequest(tenantID, who.UserID, key, append([]byte(nil), raw...))
	if err != nil {
		return false, err
	}
	request, err = ownNativePlanRequest(ctx, who, request, key)
	if err != nil {
		return false, err
	}
	selected := append([]v2input.Ref(nil), pixels...)
	work, cancel := context.WithTimeout(ctx, f.Timeout)
	defer cancel()
	scope, err := ReadCatalog(work, f.API, who, request)
	if err != nil {
		return false, err
	}
	job, err := f.InitialForProject(work, scope.ProjectID)
	if err != nil {
		return false, err
	}
	return job.StartManual(work, who, tenantID, key, request.Body, score, maxFaces, selected, diameter)
}
