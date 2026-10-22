package semanticreview

import (
	"context"
	"encoding/json"
)

// ShownBackgroundCleanupCommitInputs belongs to the copy-operation admission path.
// Its type remains distinct from preview-run and old rerender admission payloads.
// SQL must verify the ready transport objects, human decision, receipt, quota and
// exact input lineage before atomically activating the Apply child.
type ShownBackgroundCleanupCommitInputs struct {
	Node      json.RawMessage
	Transport json.RawMessage
	Inventory json.RawMessage
	ParamsJCS json.RawMessage
}

func VerifyShownBackgroundCleanupPublication(ctx context.Context, frozen FrozenShownBackgroundCleanupTask,
	published PreparedExposureTask) (PreparedExposureTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedExposureTask{}, err
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return PreparedExposureTask{}, err
	}
	return verifyPublishedExposureInputs(ctx, draft, draft.ID, frozen.frozen.params, frozen.frozen.closure, published)
}

func PrepareShownBackgroundCleanupCommitInputs(ctx context.Context, frozen FrozenShownBackgroundCleanupTask,
	published PreparedExposureTask) (ShownBackgroundCleanupCommitInputs, error) {
	verified, err := VerifyShownBackgroundCleanupPublication(ctx, frozen, published)
	if err != nil {
		return ShownBackgroundCleanupCommitInputs{}, err
	}
	payload, err := prepareVerifiedTaskCommitInputs(ctx, verified)
	if err != nil {
		return ShownBackgroundCleanupCommitInputs{}, err
	}
	return ShownBackgroundCleanupCommitInputs{Node: payload.Node, Transport: payload.Transport, Inventory: payload.Inventory, ParamsJCS: payload.ParamsJCS}, nil
}
