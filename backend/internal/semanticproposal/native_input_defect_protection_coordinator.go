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
func (s SQLTransferSourceReader) publishRegisteredNativeInputProtection(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan nativeDefectDraftPlan, color TransferColorProfile, publisher v2draft.Publisher, built v2input.Built) (registeredNativeInputProtection, error) {
	var zero registeredNativeInputProtection
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	published, err := s.publishNativeInputDefectProtection(ctx, who, plan, color, publisher)
	if err != nil {
		return zero, err
	}
	if err := s.registerNativeInputDefectProtection(ctx, scheduler, who, published, color, built); err != nil {
		return zero, err
	}
	return s.readRegisteredNativeInputProtection(ctx, who, published, color)
}
