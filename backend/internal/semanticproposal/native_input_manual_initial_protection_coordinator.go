package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Each phase repeats original authority; object IO never spans SQL transactions.
// This returns private registered provenance, not parent admission or approval.
func (s SQLTransferSourceReader) publishRegisteredNativeInputManualInitialProtection(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan nativeInputManualInitialDraftPlan, color TransferColorProfile, publisher v2draft.Publisher, built v2input.Built) (registeredNativeInputManualInitialProtection, error) {
	var zero registeredNativeInputManualInitialProtection
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	published, err := s.publishNativeInputManualInitialProtection(ctx, who, plan, color, publisher)
	if err != nil {
		return zero, err
	}
	if err := s.registerNativeInputManualInitialProtection(ctx, scheduler, who, published, color, built); err != nil {
		return zero, err
	}
	return s.readRegisteredNativeInputManualInitialProtection(ctx, who, published, color)
}
