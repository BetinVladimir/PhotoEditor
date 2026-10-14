package semanticproposal

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Recovers original saved manual declarations and independent producer evidence.
// All physical IO stays outside scheduler transport-read transactions; this does
// not publish replacements or admit the defect task.
func (s SQLTransferSourceReader) restoreNativeInputManualDependencies(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, producers nativeInputRestoredFaceProducers, color TransferColorProfile) (nativeInputManualDependencies, error) {
	var zero nativeInputManualDependencies
	if err := verifyNativeInputFaceProducerPlans(ctx, who, producers); err != nil {
		return zero, err
	}
	if scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	before, err := recoverNativeInputFaceProducerTransports(ctx, scheduler, who, producers)
	if err != nil {
		return zero, err
	}
	selection, err := s.readNativeInputSavedManualInputs(ctx, who, producers, color)
	if err != nil {
		return zero, err
	}
	proof, err := s.prepareNativeInputManualDependencies(ctx, who, producers, selection, color, before)
	if err != nil {
		return zero, err
	}
	after, err := recoverNativeInputFaceProducerTransports(ctx, scheduler, who, producers)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(before, after) {
		return zero, v2input.ErrConflict
	}
	d := producers.detector
	if err := s.recheckNativePlanPolicy(ctx, who, d.request.TenantID, d.policy); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return proof, nil
}
