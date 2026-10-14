package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// StartManual is an internal preparation service boundary using the production
// project-scoped dependencies. It never returns a detector ticket as the public
// semantic parent receipt. False means original-parent lock contention.
// Explicit pixel selection must come from the server's manual selection path;
// SemanticEditIntent.ReferenceMask is not a native rasterized pixel reference.
func (j SQLNativeInitialPipelineRecovery) StartManual(ctx context.Context, who auth.Identity, tenantID uuid.UUID, key string, raw []byte, score float64, maxFaces int, pixels []v2input.Ref, diameter [2]float64) (bool, error) {
	if err := j.validate(ctx); err != nil {
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
	capacity := ownTransferPreparation(j.Preparer.Configuration)
	work, cancel := context.WithTimeout(ctx, j.Timeout)
	defer cancel()
	catalog, err := j.CatalogForReference(work, who, tenantID, j.Preparer.Reader.ProjectID, request.Intent.Reference.AssetVersionID)
	if err != nil {
		return false, err
	}
	if catalog == nil {
		return false, v2input.ErrInvalid
	}
	run, _, err := detectorPreparationIDs(request.RunID)
	if err != nil {
		return false, err
	}
	bootstrap := nativePreparationBootstrap{Reader: j.Preparer.Reader, Scheduler: j.Preparer.Scheduler, Catalog: catalog, Documents: j.Preparer.Documents, Transport: v2input.ScopedPublisher{Documents: j.Preparer.TransportDocuments, Tenant: tenantID, Run: run}}
	_, _, worked, err := bootstrap.startOrResumeInitialManualDetector(work, who, request, key, capacity, j.Preparer.ReservedBytes, j.Preparer.TransportDocuments, score, maxFaces, selected, diameter)
	return worked, err
}
