package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// LocalPortraitIntent precedes artifact reservation: no Params artifact ID or
// task input digest is fabricated before the scoped publisher reserves it.
// A durable owner must save this intent before publication and freeze the final
// task only after validating the published Params and current input closure.
type LocalPortraitIntent struct {
	SchemaVersion int                           `json:"schema_version"`
	TaskID        uuid.UUID                     `json:"task_id"`
	NodeKey       string                        `json:"node_key"`
	Operation     string                        `json:"operation"`
	Binding       v2input.SessionVersionBinding `json:"binding"`
	ParamsJCS     string                        `json:"params_jcs"`
	ParamsSHA     string                        `json:"params_sha"`
	Profile       json.RawMessage               `json:"profile"`
	ReservedBytes int64                         `json:"reserved_bytes"`
	ManualEntries []v2input.Entry               `json:"manual_entries"`
}

// FreezePublishedLocalPortraitIntent binds the actual scoped publication to the
// saved prepublication intent. Evidence must be freshly resolved by the caller.
// This does not authorize the durable owner or transactional registration.
func FreezePublishedLocalPortraitIntent(ctx context.Context, workflow uuid.UUID, ordinal int,
	intent LocalPortraitIntent, prepared PreparedLocalPortraitTask, sourceRaw []byte,
	runtimeSHA string, capacity v2portrait.LocalResourceEstimate) (FrozenLocalPortraitTask, error) {
	var zero FrozenLocalPortraitTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if intent.SchemaVersion != 1 || intent.Operation != prepared.Published.Plan.Operation ||
		intent.ParamsJCS != string(prepared.Published.Plan.Params) || intent.ParamsSHA != prepared.Published.Plan.ParamsSHA ||
		string(intent.Profile) != string(prepared.Published.Plan.Profile) || intent.ReservedBytes != prepared.Published.Plan.ReservedBytes {
		return zero, v2input.ErrInvalid
	}
	frozen, err := FreezeLocalPortraitTask(ctx, workflow, ordinal, prepared, intent.Binding, sourceRaw, runtimeSHA, capacity)
	if err != nil {
		return zero, err
	}
	if frozen.draft.ID != intent.TaskID || frozen.draft.NodeKey != intent.NodeKey {
		return zero, v2input.ErrInvalid
	}
	manual := make([]v2input.Entry, 0, len(frozen.entries)-1)
	for _, entry := range frozen.entries {
		if entry.Ref.ArtifactID != prepared.Published.ParamsRef.ArtifactID {
			manual = append(manual, entry)
		}
	}
	if len(manual) != len(intent.ManualEntries) {
		return zero, v2input.ErrInvalid
	}
	for i, entry := range manual {
		if entry != intent.ManualEntries[i] {
			return zero, v2input.ErrInvalid
		}
	}
	return frozen, nil
}

func (s ClosureResolver) CompileLocalPortraitIntent(ctx context.Context, who auth.Identity,
	tenant, workflow uuid.UUID, ordinal int, operation string, binding v2input.SessionVersionBinding,
	sourceRaw, paramsRaw []byte, runtimeSHA string, capacity v2portrait.LocalResourceEstimate,
	proof LocalPortraitRecoveryProof) (LocalPortraitIntent, error) {
	var zero LocalPortraitIntent
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tenant == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	task, err := v2input.TaskID(workflow, operation, ordinal)
	if err != nil {
		return zero, err
	}
	key, _ := v2input.NodeKey(operation, ordinal)
	sourceRaw = append([]byte(nil), sourceRaw...)
	proof.RootRaw = append([]byte(nil), proof.RootRaw...)
	proof.OriginalRaw = append([]byte(nil), proof.OriginalRaw...)
	proof.VectorsRaw = append([]byte(nil), proof.VectorsRaw...)
	plan, err := v2portrait.CompileLocalTaskPlan(ctx, operation, binding, sourceRaw, append([]byte(nil), paramsRaw...), runtimeSHA, capacity)
	if err != nil {
		return zero, err
	}
	var header struct {
		Mask      v2input.Ref `json:"mask"`
		Protected v2input.Ref `json:"protected"`
	}
	if json.Unmarshal(plan.Params, &header) != nil {
		return zero, v2input.ErrInvalid
	}
	evidence, err := s.ResolveManualRasterTaskInputEvidence(ctx, who, tenant, binding, sourceRaw,
		proof.Root, proof.RootRaw, proof.OriginalParams, proof.OriginalRaw, proof.VectorsRaw, header.Mask, header.Protected)
	if err != nil {
		return zero, err
	}
	// The published local Params adds exactly one further direct input.
	if len(evidence.Entries) == 0 || len(evidence.Entries) > 126 {
		return zero, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return LocalPortraitIntent{SchemaVersion: 1, TaskID: task, NodeKey: key, Operation: operation, Binding: binding,
		ParamsJCS: string(plan.Params), ParamsSHA: plan.ParamsSHA, Profile: append(json.RawMessage(nil), plan.Profile...),
		ReservedBytes: plan.ReservedBytes, ManualEntries: append([]v2input.Entry(nil), evidence.Entries...)}, nil
}
