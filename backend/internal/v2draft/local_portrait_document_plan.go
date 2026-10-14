package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// LocalPortraitDocumentPlan is the exact saved compiler intent for recovery.
// It is not a grant to publish Params or create a durable graph owner.
type LocalPortraitDocumentPlan struct {
	SchemaVersion int                           `json:"schema_version"`
	Binding       v2input.SessionVersionBinding `json:"binding"`
	Task          v2input.DraftTask             `json:"task"`
	ParamsJCS     string                        `json:"params_jcs"`
	Entries       []v2input.Entry               `json:"entries"`
	ReservedBytes int64                         `json:"reserved_bytes"`
}

func (f FrozenLocalPortraitTask) DocumentPlan() (LocalPortraitDocumentPlan, error) {
	draft, err := f.DraftTask()
	if err != nil || len(f.params) == 0 || f.reservedBytes <= 0 {
		return LocalPortraitDocumentPlan{}, v2input.ErrInvalid
	}
	return LocalPortraitDocumentPlan{SchemaVersion: 1, Binding: f.binding, Task: draft, ParamsJCS: string(f.params), Entries: append([]v2input.Entry(nil), f.entries...), ReservedBytes: f.reservedBytes}, nil
}

// RestoreLocalPortraitTask rebuilds the digest/profile from saved intent and
// independently supplied source bytes/capacity. SQL must revalidate current
// source/manual producer/owner scope; this does not restore historical grants.
func RestoreLocalPortraitTask(ctx context.Context, workflow uuid.UUID, ordinal int, raw []byte,
	sourceRaw []byte, runtimeSHA string, capacity v2portrait.LocalResourceEstimate) (FrozenLocalPortraitTask, error) {
	var zero FrozenLocalPortraitTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	var plan LocalPortraitDocumentPlan
	if json.Unmarshal(raw, &plan) != nil || plan.SchemaVersion != 1 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(plan)
	normalized, e := canonical.Bytes(raw)
	if err != nil || e != nil || string(exact) != string(normalized) {
		return zero, v2input.ErrInvalid
	}
	prepared := PreparedLocalPortraitTask{}
	prepared.Published.ParamsRef = v2input.Ref{ArtifactID: plan.Task.ParamsArtifactID, SHA256: plan.Task.ParamsSHA}
	prepared.Published.Plan.Operation = plan.Task.Operation
	prepared.Published.Plan.Params = json.RawMessage(plan.ParamsJCS)
	prepared.Published.Plan.ParamsSHA = plan.Task.ParamsSHA
	prepared.Published.Plan.Profile = plan.Task.ExecutionProfile
	prepared.Published.Plan.ReservedBytes = plan.ReservedBytes
	prepared.Evidence.Entries = plan.Entries
	frozen, err := FreezeLocalPortraitTask(ctx, workflow, ordinal, prepared, plan.Binding, sourceRaw, runtimeSHA, capacity)
	if err != nil {
		return zero, err
	}
	rebuilt, err := frozen.DocumentPlan()
	if err != nil {
		return zero, err
	}
	rebuiltBytes, err := canonical.Marshal(rebuilt)
	if err != nil || string(rebuiltBytes) != string(exact) {
		return zero, v2input.ErrInvalid
	}
	return frozen, nil
}
