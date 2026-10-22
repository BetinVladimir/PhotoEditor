package semanticreview

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// ShownRetouchCommitInputs belongs to the copy-operation admission path.
// Its type remains distinct from preview-run and old rerender admission payloads.
// SQL must verify the ready transport objects, human decision, receipt, quota and
// exact input lineage before atomically activating the Apply child.
type ShownRetouchCommitInputs struct {
	Node      json.RawMessage
	Transport json.RawMessage
	Inventory json.RawMessage
	ParamsJCS json.RawMessage
}

func VerifyShownRetouchPublication(ctx context.Context, frozen FrozenShownRetouchTask,
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

func PrepareShownRetouchCommitInputs(ctx context.Context, frozen FrozenShownRetouchTask,
	published PreparedExposureTask) (ShownRetouchCommitInputs, error) {
	verified, err := VerifyShownRetouchPublication(ctx, frozen, published)
	if err != nil {
		return ShownRetouchCommitInputs{}, err
	}
	payload, err := prepareVerifiedTaskCommitInputs(ctx, verified)
	if err != nil {
		return ShownRetouchCommitInputs{}, err
	}
	// The full frozen inventory remains unchanged. At the envelope boundary
	// Params travels in TaskCommand.params, so avoid repeating it among inputs.
	// Smaller inventories retain their existing transport representation.
	if verified.Graph.Built.Count == 512 {
		var transport ExposureTransportBinding
		if json.Unmarshal(payload.Transport, &transport) != nil {
			return ShownRetouchCommitInputs{}, ErrInvalid
		}
		roots := make([]v2input.Ref, 0, 511)
		paramsCount := 0
		for _, ref := range transport.DirectRoots {
			if ref == verified.Params {
				paramsCount++
			} else {
				roots = append(roots, ref)
			}
		}
		if paramsCount != 1 || len(roots) != 511 {
			return ShownRetouchCommitInputs{}, ErrInvalid
		}
		transport.DirectRoots = roots
		payload.Transport, err = canonical.Marshal(transport)
		if err != nil {
			return ShownRetouchCommitInputs{}, err
		}
	}
	return ShownRetouchCommitInputs{Node: payload.Node, Transport: payload.Transport, Inventory: payload.Inventory, ParamsJCS: payload.ParamsJCS}, nil
}
