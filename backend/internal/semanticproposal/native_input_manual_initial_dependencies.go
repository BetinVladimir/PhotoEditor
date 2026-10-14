package semanticproposal

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type nativeInputManualInitialDependencies struct {
	dependencies nativeInputManualDependencies
	params       nativeInputPublishedManualParamsProof
}

// Initial path: does not require a saved defect stage. Original registered model
// transports and accepted Params evidence are reread after producer object IO.
func (s SQLTransferSourceReader) prepareNativeInputManualInitialDependencies(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, producers nativeInputRestoredFaceProducers, published nativeInputPublishedManualParams, color TransferColorProfile) (nativeInputManualInitialDependencies, error) {
	var zero nativeInputManualInitialDependencies
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
	params, err := s.readNativeInputPublishedManualParams(ctx, who, producers, published, color)
	if err != nil {
		return zero, err
	}
	dependencies, err := s.assembleNativeInputManualDependencies(ctx, who, producers, params.published.selection, color, before)
	if err != nil {
		return zero, err
	}
	current, err := s.readNativeInputPublishedManualParams(ctx, who, producers, published, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(current, params) {
		return zero, v2input.ErrConflict
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
	return nativeInputManualInitialDependencies{dependencies: dependencies, params: params}, nil
}
