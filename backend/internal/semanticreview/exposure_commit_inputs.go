package semanticreview

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type ExposureCommitInputs struct {
	Review    ApprovedCommitInputs
	Nodes     json.RawMessage
	Transport json.RawMessage
	Inventory json.RawMessage
	Graphs    []v2input.GraphTask
}

// Array position is the exact v2_task_inputs ordinal. Transport documents are
// registered separately in the reserved transport ordinal range.
type ExposureInventoryBinding struct {
	TaskID  uuid.UUID       `json:"task_id"`
	Entries []v2input.Entry `json:"entries"`
}

type ExposureTransportBinding struct {
	TaskID      uuid.UUID     `json:"task_id"`
	EntryCount  int           `json:"entry_count"`
	EntriesSHA  string        `json:"entries_sha"`
	Manifest    v2input.Ref   `json:"manifest"`
	Chunks      []v2input.Ref `json:"chunks"`
	DirectRoots []v2input.Ref `json:"direct_roots"`
}

// PrepareApprovedExposureCommitInputs reconstructs private review/task evidence
// after all external publication IO. It returns only independently verified
// owned SQL projections and graph cursors, with no partial payload on error.
// The final transaction must still validate live scope, exact artifact bytes,
// resources/quota, SQL node qualification and the full registered graph.
func PrepareApprovedExposureCommitInputs(ctx context.Context, tenant, actor, child uuid.UUID, prepared PreparedReview,
	targets []LockedTarget, request ApplyDecision, decision PublishedDecision, inputs []ExposureTaskInputs, published []PreparedExposureTask) (ExposureCommitInputs, error) {
	var empty ExposureCommitInputs
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	frozen, err := CompileApprovedExposureTasks(child, prepared, targets, request, inputs)
	if err != nil {
		return empty, err
	}
	if len(frozen) == 0 || len(frozen) != len(published) {
		return empty, ErrInvalid
	}
	byTask := make(map[uuid.UUID]PreparedExposureTask, len(published))
	for _, task := range published {
		if _, duplicate := byTask[task.Graph.ID]; duplicate {
			return empty, ErrInvalid
		}
		byTask[task.Graph.ID] = task
	}
	params := make([]ApprovedTaskDraft, 0, len(frozen))
	nodes := make([]v2input.DraftTask, 0, len(frozen))
	graphs := make([]v2input.GraphTask, 0, len(frozen))
	transport := make([]ExposureTransportBinding, 0, len(frozen))
	inventory := make([]ExposureInventoryBinding, 0, len(frozen))
	for _, task := range frozen {
		actual, exists := byTask[task.task]
		if !exists {
			return empty, ErrInvalid
		}
		verified, err := VerifyExposurePublication(ctx, task, actual)
		if err != nil {
			return empty, err
		}
		cursor, err := verified.Graph.Open(ctx)
		if err != nil {
			return empty, err
		}
		entries := make([]v2input.Entry, 0, verified.Graph.Built.Count)
		for {
			if err := ctx.Err(); err != nil {
				return empty, err
			}
			entry, more, err := cursor.Next(ctx)
			if err != nil {
				return empty, err
			}
			if !more {
				break
			}
			if len(entries) >= 127 || len(entries) >= len(verified.Graph.Direct) || entry.Ref != verified.Graph.Direct[len(entries)] {
				return empty, ErrInvalid
			}
			entries = append(entries, entry)
		}
		entriesSHA, err := canonical.Hash(entries)
		if err != nil || len(entries) != verified.Graph.Built.Count || entriesSHA != verified.Graph.Built.SHA256 {
			return empty, ErrInvalid
		}
		inventory = append(inventory, ExposureInventoryBinding{TaskID: task.task, Entries: entries})
		params = append(params, ApprovedTaskDraft{TaskID: task.task, TargetID: task.target, Params: verified.Params, Body: verified.ParamsJCS, Kind: "local_exposure"})
		nodes = append(nodes, verified.Draft)
		graphs = append(graphs, verified.Graph)
		transport = append(transport, ExposureTransportBinding{TaskID: task.task, EntryCount: verified.Graph.Built.Count, EntriesSHA: verified.Graph.Built.SHA256,
			Manifest: verified.Graph.Built.Manifest, Chunks: append([]v2input.Ref(nil), verified.Graph.Built.Chunks...), DirectRoots: append([]v2input.Ref(nil), verified.Graph.Direct...)})
	}
	review, err := PrepareApprovedCommitInputs(tenant, actor, child, prepared, targets, request, decision, params)
	if err != nil {
		return empty, err
	}
	raw, err := canonical.Marshal(nodes)
	if err != nil {
		return empty, ErrInvalid
	}
	transportRaw, err := canonical.Marshal(transport)
	if err != nil {
		return empty, ErrInvalid
	}
	inventoryRaw, err := canonical.Marshal(inventory)
	if err != nil {
		return empty, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return ExposureCommitInputs{Review: review, Nodes: raw, Transport: transportRaw, Inventory: inventoryRaw, Graphs: graphs}, nil
}
