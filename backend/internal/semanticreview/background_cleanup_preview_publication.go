package semanticreview

import (
	"context"
	"photoeditor/backend/internal/v2input"
)

// PublishBackgroundCleanupPreviewTask requires a durable background_cleanup-scoped publisher.
// Object publication alone grants no quota, graph seal or dispatch authority.
func PublishBackgroundCleanupPreviewTask(ctx context.Context, f FrozenBackgroundCleanupPreviewTask, publisher v2input.Publisher) (PreparedExposureTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedExposureTask{}, err
	}
	if _, err := f.DraftTask(); err != nil {
		return PreparedExposureTask{}, err
	}
	return publishFrozenDocumentTask(ctx, f.frozen, publisher)
}

// VerifyBackgroundCleanupPreviewPublication rebuilds transport from compiler-owned inputs.
func VerifyBackgroundCleanupPreviewPublication(ctx context.Context, f FrozenBackgroundCleanupPreviewTask, published PreparedExposureTask) (PreparedExposureTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedExposureTask{}, err
	}
	draft, err := f.DraftTask()
	if err != nil {
		return PreparedExposureTask{}, err
	}
	return verifyPublishedExposureInputs(ctx, draft, draft.ID, f.frozen.params, f.frozen.closure, published)
}

func PrepareBackgroundCleanupPreviewCommitInputs(ctx context.Context, f FrozenBackgroundCleanupPreviewTask, published PreparedExposureTask) (PreviewCommitInputs, error) {
	verified, err := VerifyBackgroundCleanupPreviewPublication(ctx, f, published)
	if err != nil {
		return PreviewCommitInputs{}, err
	}
	return prepareVerifiedTaskCommitInputs(ctx, verified)
}
