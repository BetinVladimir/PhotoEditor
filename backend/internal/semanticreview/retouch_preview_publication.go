package semanticreview

import (
	"context"
	"photoeditor/backend/internal/v2input"
)

// PublishRetouchPreviewTask requires a durable retouch_region-scoped publisher.
// Object publication alone grants no quota, graph seal or dispatch authority.
func PublishRetouchPreviewTask(ctx context.Context, f FrozenRetouchPreviewTask, publisher v2input.Publisher) (PreparedExposureTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedExposureTask{}, err
	}
	if _, err := f.DraftTask(); err != nil {
		return PreparedExposureTask{}, err
	}
	return publishFrozenDocumentTask(ctx, f.frozen, publisher)
}

// VerifyRetouchPreviewPublication rebuilds transport from compiler-owned inputs.
func VerifyRetouchPreviewPublication(ctx context.Context, f FrozenRetouchPreviewTask, published PreparedExposureTask) (PreparedExposureTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedExposureTask{}, err
	}
	draft, err := f.DraftTask()
	if err != nil {
		return PreparedExposureTask{}, err
	}
	return verifyPublishedExposureInputs(ctx, draft, draft.ID, f.frozen.params, f.frozen.closure, published)
}

func PrepareRetouchPreviewCommitInputs(ctx context.Context, f FrozenRetouchPreviewTask, published PreparedExposureTask) (PreviewCommitInputs, error) {
	verified, err := VerifyRetouchPreviewPublication(ctx, f, published)
	if err != nil {
		return PreviewCommitInputs{}, err
	}
	return prepareVerifiedTaskCommitInputs(ctx, verified)
}
