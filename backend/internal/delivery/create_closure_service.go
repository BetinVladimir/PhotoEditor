package delivery

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// ResolveCreatePrepare reads the pinned ledger before mutable catalog data,
// traverses version-pinned object documents outside SQL transactions, then
// repeats live selection authority. Final registration still needs its own
// current authority fence; this returned value is not a dispatch permission.
func (s Service) ResolveCreatePrepare(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key, bucket string, objects v2input.SessionAnalysisObjectReader, limits PrepareResourceLimits) (FrozenPrepareTask, error) {
	if err := ctx.Err(); err != nil {
		return FrozenPrepareTask{}, err
	}
	intent, err := FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	if s.DB == nil || bucket == "" || objects == nil {
		return FrozenPrepareTask{}, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	request = intent.Request()
	before, err := s.RecoverCreate(ctx, who, tid, request, key)
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	if !before.Found || before.Completed {
		return FrozenPrepareTask{}, domain.NewError("DELIVERY_CREATE_STALE", 409)
	}
	raw, err := canonical.Marshal(before.Pending)
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	frozen, err := intent.ResolvePrepareTask(ctx, who, raw, SQLCreateRecipeInputResolver{DB: s.DB, Bucket: bucket, Objects: objects}, limits)
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	after, err := s.RecoverCreate(ctx, who, tid, request, key)
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	if !after.Found || after.Completed {
		return FrozenPrepareTask{}, domain.NewError("DELIVERY_CREATE_STALE", 409)
	}
	current, err := canonical.Marshal(after.Pending)
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	if !bytes.Equal(raw, current) {
		return FrozenPrepareTask{}, domain.NewError("DELIVERY_SOURCE_STALE", 409)
	}
	return frozen, nil
}
