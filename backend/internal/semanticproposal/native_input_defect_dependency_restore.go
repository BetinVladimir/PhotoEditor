package semanticproposal

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Restores original Params and completed native dependency packets. No object IO
// happens inside the transport-read transactions. Defect qualification, saved
// config matching, protected-result restoration and parent admission are later
// gates; this dependency packet alone grants none of them.
func (s SQLTransferSourceReader) restoreNativeInputDefectDependencies(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plans nativeInputRestoredFaceProducers, color TransferColorProfile, diameter [2]float64, permanent *review.ProtectionMask, additional []review.ProtectionMask, manuals []review.OriginalManualProtection) (nativeDefectDependencies, error) {
	var zero nativeDefectDependencies
	if err := verifyNativeInputFaceProducerPlans(ctx, who, plans); err != nil {
		return zero, err
	}
	if scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	before, err := recoverNativeInputFaceProducerTransports(ctx, scheduler, who, plans)
	if err != nil {
		return zero, err
	}
	d := plans.detector
	prepared, err := s.restoreNativeInputDefectFaceParams(ctx, who, d.request, d.published.Ref, d.anchor.snapshotRef, plans.landmarks, plans.skin, color, diameter, permanent, additional, manuals)
	if err != nil {
		return zero, err
	}
	dependencies, err := s.prepareNativeDefectDependencies(ctx, who, prepared, color, before.landmarks, before.skin, before.parser, before.detector)
	if err != nil {
		return zero, err
	}
	// Later physical producer IO must not leave earlier transport observations live.
	after, err := recoverNativeInputFaceProducerTransports(ctx, scheduler, who, plans)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(before, after) {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativePlanPolicy(ctx, who, d.request.TenantID, d.policy); err != nil {
		return zero, err
	}
	return dependencies, nil
}
