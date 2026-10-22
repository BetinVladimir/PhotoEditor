package semanticreview

import (
	"context"
	"photoeditor/backend/internal/v2input"
)

// PublishRemovalPreviewTask requires a durable removal-scoped publisher.
// Object publication alone grants no quota, graph seal or dispatch authority.
func PublishRemovalPreviewTask(ctx context.Context, f FrozenRemovalPreviewTask, publisher v2input.Publisher) (PreparedExposureTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedExposureTask{}, err
	}
	if _, err := f.DraftTask(); err != nil {
		return PreparedExposureTask{}, err
	}
	return publishFrozenDocumentTask(ctx, f.frozen, publisher)
}

// VerifyRemovalPreviewPublication rebuilds transport from compiler-owned inputs.
func VerifyRemovalPreviewPublication(ctx context.Context, f FrozenRemovalPreviewTask, published PreparedExposureTask) (PreparedExposureTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedExposureTask{}, err
	}
	draft, err := f.DraftTask()
	if err != nil {
		return PreparedExposureTask{}, err
	}
	return verifyPublishedExposureInputs(ctx, draft, draft.ID, f.frozen.params, f.frozen.closure, published)
}

func PrepareRemovalPreviewCommitInputs(ctx context.Context, f FrozenRemovalPreviewTask, published PreparedExposureTask) (PreviewCommitInputs, error) {
	verified, err := VerifyRemovalPreviewPublication(ctx, f, published)
	if err != nil {
		return PreviewCommitInputs{}, err
	}
	return prepareVerifiedTaskCommitInputs(ctx, verified)
}
