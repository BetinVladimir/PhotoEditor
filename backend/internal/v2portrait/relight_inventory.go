package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"sort"
)

type RelightInputDocuments struct {
	Source, Depth, Subject, Protected []byte
}

type RelightInputPlan struct {
	Plan   LocalTaskPlan
	Inputs []v2input.Entry
}

// CompileRelightInputPlan resolves all direct document and array inputs needed
// by the operation. Original producer roots/Params and qualification evidence
// must still be verified and added by the trusted admission coordinator.
func CompileRelightInputPlan(ctx context.Context, binding v2input.SessionVersionBinding,
	documents RelightInputDocuments, paramsRaw []byte, runtimeSHA string, capacity LocalResourceEstimate,
	pin RelightQualificationPin) (RelightInputPlan, error) {
	var zero RelightInputPlan
	plan, err := CompilePinnedRelightTaskPlan(ctx, binding, documents.Source, paramsRaw, runtimeSHA, capacity, pin)
	if err != nil {
		return zero, err
	}
	var params RelightParams
	if json.Unmarshal(plan.Params, &params) != nil {
		return zero, v2input.ErrInvalid
	}
	roots, err := RelightRootInputs(params, pin)
	if err != nil {
		return zero, err
	}
	source, err := RelightSourceInputs(params, pin, documents.Source)
	if err != nil {
		return zero, err
	}
	depth, err := RelightDepthInputs(params, pin, documents.Source, documents.Depth)
	if err != nil {
		return zero, err
	}
	masks, err := RelightMaskInputs(params, pin, documents.Source, documents.Subject, documents.Protected)
	if err != nil {
		return zero, err
	}
	seen := map[uuid.UUID]v2input.Entry{}
	var entries []v2input.Entry
	for _, group := range [][]v2input.Entry{roots, source, depth, masks} {
		for _, entry := range group {
			if old, ok := seen[entry.Ref.ArtifactID]; ok {
				if old != entry {
					return zero, v2input.ErrInvalid
				}
				continue
			}
			seen[entry.Ref.ArtifactID] = entry
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String() })
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return RelightInputPlan{Plan: plan, Inputs: entries}, nil
}
