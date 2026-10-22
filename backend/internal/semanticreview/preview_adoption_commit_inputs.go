package semanticreview

import (
	"context"
	"encoding/json"
)

// ShownExposureCommitInputs belongs to the copy-operation admission path.
// Its type remains distinct from preview-run and old rerender admission payloads.
// SQL must verify the ready transport objects, human decision, receipt, quota and
// exact input lineage before atomically activating the Apply child.
type ShownExposureCommitInputs struct {
	Node      json.RawMessage
	Transport json.RawMessage
	Inventory json.RawMessage
	ParamsJCS json.RawMessage
}

func VerifyShownExposurePublication(ctx context.Context, frozen FrozenShownExposureTask,
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

func PrepareShownExposureCommitInputs(ctx context.Context, frozen FrozenShownExposureTask,
	published PreparedExposureTask) (ShownExposureCommitInputs, error) {
	verified, err := VerifyShownExposurePublication(ctx, frozen, published)
	if err != nil {
		return ShownExposureCommitInputs{}, err
	}
	payload, err := prepareVerifiedTaskCommitInputs(ctx, verified)
	if err != nil {
		return ShownExposureCommitInputs{}, err
	}
	return ShownExposureCommitInputs{Node: payload.Node, Transport: payload.Transport, Inventory: payload.Inventory, ParamsJCS: payload.ParamsJCS}, nil
}
