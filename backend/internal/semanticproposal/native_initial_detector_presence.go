package semanticproposal

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func (s SQLTransferSourceReader) readInitialNativeDetectorPresence(ctx context.Context, who auth.Identity, request Request, key string) (nativeStagePresence, error) {
	owned, err := ownNativePlanRequest(ctx, who, request, key)
	if err != nil {
		return "", err
	}
	if s.DB == nil {
		return "", v2input.ErrInvalid
	}
	var value *string
	err = tenant.WithTenant(ctx, s.DB, who, owned.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT photo.v27_native_initial_detector_presence($1,$2,$3,$4,$5,$6)", owned.TenantID, s.ProjectID, who.UserID, who.SessionID, owned.RunID, owned.RequestSHA).Scan(&value)
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && value == nil) {
		return "", v2input.ErrConflict
	}
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return decodeNativeStagePresence(*value)
}

// Caller holds the original parent lock. This is only the new-detector branch;
// existing checkpoints must use original-anchor restoration, never republish a
// newly selected snapshot or qualification after a failed restore.
func (b nativePreparationBootstrap) prepareAndStartNewInitialManualDetector(ctx context.Context, who auth.Identity, request Request, key string, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher, score float64, maxFaces int, pixels []v2input.Ref, diameter [2]float64) (detectorDraftPlan, v2input.JobTicket, error) {
	var zero detectorDraftPlan
	if err := b.validate(ctx); err != nil {
		return zero, v2input.JobTicket{}, err
	}
	if documents == nil {
		return zero, v2input.JobTicket{}, v2input.ErrInvalid
	}
	if err := validateInitialDetectorControls(score, maxFaces); err != nil {
		return zero, v2input.JobTicket{}, err
	}
	if err := validateInitialManualPixelSelection(pixels); err != nil {
		return zero, v2input.JobTicket{}, err
	}
	if err := validateInitialManualDiameter(diameter); err != nil {
		return zero, v2input.JobTicket{}, err
	}
	capacity = ownTransferPreparation(capacity)
	if _, err := initialNativeFaceBudget(capacity, reserved, v2qualification.Metrics{}); err != nil {
		return zero, v2input.JobTicket{}, err
	}
	owned, err := ownNativePlanRequest(ctx, who, request, key)
	if err != nil {
		return zero, v2input.JobTicket{}, err
	}
	selected := append([]v2input.Ref(nil), pixels...)
	presence, err := b.Reader.readInitialNativeDetectorPresence(ctx, who, owned, key)
	if err != nil {
		return zero, v2input.JobTicket{}, err
	}
	if presence != nativeStageAbsent {
		return zero, v2input.JobTicket{}, v2input.ErrConflict
	}
	// Verify original source-bound raster/vector closures before any intent,
	// snapshot or detector Params publication. Face binding is checked later
	// against actual completed native producers, never inferred here.
	b.Reader = b.Reader.withManualRasterScope()
	for _, pixel := range selected {
		if _, err := b.Reader.readOriginalManualTarget(ctx, who, owned.TenantID, owned.Intent.Reference, pixel); err != nil {
			return zero, v2input.JobTicket{}, err
		}
	}
	anchor, err := b.prepareInitialNativeAnchor(ctx, who, owned.TenantID, key, owned.Body, capacity, reserved)
	if err != nil {
		return zero, v2input.JobTicket{}, err
	}
	return b.bootstrapQualifiedInitialManualDetector(ctx, who, anchor, capacity, reserved, documents, score, maxFaces, selected, diameter)
}
