package delivery

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
)

// PreparePinnedWatermarkDelivery starts from persisted selection, publishes and
// registers prepare transport on scheduler credentials, then saves verified
// source proof/package on API credentials. It does not activate the graph or
// complete public idempotency; child watermark admission must be installed first.
func (s Service) PreparePinnedWatermarkDelivery(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string, deps CreateDeliveryDependencies) (PreparedCreateDelivery, error) {
	if err := ctx.Err(); err != nil {
		return PreparedCreateDelivery{}, err
	}
	intent, err := FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return PreparedCreateDelivery{}, err
	}
	if err := checkCreateSigningPolicy(ctx, tid, intent.Request().Policy, deps.Signing); err != nil {
		return PreparedCreateDelivery{}, err
	}
	if !deliveryPoolsIsolated(s.DB, deps.Scheduler) || deps.Objects == nil || deps.Documents == nil || len(deps.Bucket) < 3 || len(deps.Bucket) > 63 || deps.Limits.RAMBytes <= 0 || deps.Limits.ScratchBytes <= 0 || deps.Limits.OutputBytes < 8<<20 {
		return PreparedCreateDelivery{}, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	request = intent.Request()
	// Use the persisted selection for MIME qualification before object effects.
	// The later transport and admission stages independently replay its authority.
	recovery, err := s.RecoverWatermarkSelection(ctx, who, tid, request, key)
	if err != nil {
		return PreparedCreateDelivery{}, err
	}
	if !recovery.Found || recovery.Completed {
		return PreparedCreateDelivery{}, domain.NewError("DELIVERY_CREATE_STALE", 409)
	}
	if err := checkCreateSigningMIMEs(ctx, tid, request.Policy, recovery.Pending.Catalog, deps.Signing); err != nil {
		return PreparedCreateDelivery{}, err
	}
	transport, err := s.StageWatermarkCreateTransport(ctx, who, tid, request, key, deps.Bucket, deps.Objects, deps.Documents, deps.Limits, deps.MetadataPreparer)
	if err != nil {
		return PreparedCreateDelivery{}, err
	}
	raw, err := canonical.Marshal(recovery.Pending)
	if err != nil {
		return PreparedCreateDelivery{}, err
	}
	pins, err := s.StageWatermarkSelectionSources(ctx, who, tid, request, key, raw, deps.Bucket, deps.Objects)
	if err != nil {
		return PreparedCreateDelivery{}, err
	}
	if err = database.TenantTransaction(ctx, deps.Scheduler, tid, func(tx pgx.Tx) error {
		return transport.Frozen.RegisterInputsInTransaction(ctx, tx, tid, intent.identity.Run, transport.Graph)
	}); err != nil {
		return PreparedCreateDelivery{}, err
	}
	if err = s.AdmitPreparedWatermarkCreate(ctx, who, tid, request, key, pins); err != nil {
		return PreparedCreateDelivery{}, err
	}
	return PreparedCreateDelivery{Identity: intent.identity, Transport: transport}, ctx.Err()
}
