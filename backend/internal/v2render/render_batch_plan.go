package v2render

import (
	"encoding/json"
	"strconv"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type CompiledRenderBatchPart struct {
	RunID        uuid.UUID
	FirstOrdinal int
	Plan         CompiledRenderRun
}

// CompileRenderBatchPlans partitions a frozen selection into bounded immutable
// runs. IDs and boundaries are deterministic for the same ordered selection.
// This performs no persistence: the caller must durably record the batch and
// reconcile all parts before exposing aggregate completion.
func CompileRenderBatchPlans(batch, project, snapshot uuid.UUID, requests []RenderDraftRequest, resolved [][]v2input.Entry) ([]CompiledRenderBatchPart, error) {
	if batch == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || len(requests) < 1 || len(requests) > 5000 || len(resolved) != len(requests) {
		return nil, v2input.ErrInvalid
	}
	for _, request := range requests {
		if request.Tenant != requests[0].Tenant || request.Actor != requests[0].Actor {
			return nil, v2input.ErrInvalid
		}
	}
	runID := func(index int) uuid.UUID { return uuid.NewSHA1(batch, []byte("render.batch:"+strconv.Itoa(index))) }
	var parts []CompiledRenderBatchPart
	start, size, overhead := 0, 0, 0
	var total int64
	flush := func(end int) error {
		id := runID(len(parts))
		plan, err := CompileRenderRunPlan(id, project, snapshot, requests[start:end], resolved[start:end])
		if err != nil {
			return err
		}
		if total > 9007199254740991-plan.OutputBytes {
			return v2input.ErrInvalid
		}
		total += plan.OutputBytes
		parts = append(parts, CompiledRenderBatchPart{RunID: id, FirstOrdinal: start, Plan: plan})
		start, size = end, 0
		return nil
	}
	for i, request := range requests {
		one, err := CompileRenderRunPlan(runID(len(parts)), project, snapshot, []RenderDraftRequest{request}, resolved[i:i+1])
		if err != nil {
			return nil, err
		}
		var wire struct {
			Render struct {
				Nodes []RenderRunNode `json:"nodes"`
			} `json:"v26_render"`
		}
		if json.Unmarshal(one.Config, &wire) != nil || len(wire.Render.Nodes) != 1 {
			return nil, v2input.ErrInvalid
		}
		node, err := canonical.Marshal(wire.Render.Nodes[0])
		if err != nil {
			return nil, err
		}
		overhead = len(one.Config) - len(node)
		next := len(node)
		if i > start {
			next++
		}
		if overhead+size+next > 1048576 {
			if err := flush(i); err != nil {
				return nil, err
			}
			next = len(node)
		}
		size += next
	}
	if err := flush(len(requests)); err != nil {
		return nil, err
	}
	return parts, nil
}
