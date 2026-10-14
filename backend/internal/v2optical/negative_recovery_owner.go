package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// NegativeRecoveryOwner is server-assigned scope, never client authority.
// Persistent creation must bind it to live identity, quota and draft state.
type NegativeRecoveryOwner struct {
	TenantID      uuid.UUID `json:"tenant_id"`
	ProjectID     uuid.UUID `json:"project_id"`
	SnapshotID    uuid.UUID `json:"snapshot_id"`
	ActorID       uuid.UUID `json:"actor_id"`
	AuthSessionID uuid.UUID `json:"auth_session_id"`
	RunID         uuid.UUID `json:"run_id"`
	WorkflowID    uuid.UUID `json:"workflow_id"`
	TaskID        uuid.UUID `json:"task_id"`
}

func (o NegativeRecoveryOwner) valid() bool {
	for _, id := range []uuid.UUID{o.TenantID, o.ProjectID, o.SnapshotID, o.ActorID, o.AuthSessionID, o.RunID, o.WorkflowID, o.TaskID} {
		if id == uuid.Nil {
			return false
		}
	}
	return o.RunID != o.WorkflowID && o.RunID != o.TaskID && o.WorkflowID != o.TaskID
}

type negativeOwnedRecovery struct {
	SchemaVersion int                   `json:"schema_version"`
	Owner         NegativeRecoveryOwner `json:"owner"`
	DescriptorJCS string                `json:"descriptor_jcs"`
}

func FreezeOwnedNegativeRecovery(ctx context.Context, owner NegativeRecoveryOwner, plan NegativeTaskPlan, source []byte, qualified VerifiedNegativeQualification, params v2input.Ref) ([]byte, error) {
	if !owner.valid() {
		return nil, v2input.ErrInvalid
	}
	descriptor, err := FreezeNegativeTaskDescriptor(ctx, plan, source, qualified, params)
	if err != nil {
		return nil, err
	}
	raw, err := canonical.Marshal(negativeOwnedRecovery{1, owner, string(descriptor)})
	if err != nil || len(raw) > 2<<20 {
		return nil, v2input.ErrInvalid
	}
	return raw, ctx.Err()
}
func RestoreOwnedNegativeRecovery(ctx context.Context, raw []byte, expected NegativeRecoveryOwner, qualified VerifiedNegativeQualification) (NegativeTaskPlan, v2input.Ref, []byte, error) {
	fail := func() (NegativeTaskPlan, v2input.Ref, []byte, error) {
		return NegativeTaskPlan{}, v2input.Ref{}, nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return NegativeTaskPlan{}, v2input.Ref{}, nil, err
	}
	if !expected.valid() || len(raw) < 2 || len(raw) > 2<<20 {
		return fail()
	}
	var saved negativeOwnedRecovery
	if json.Unmarshal(raw, &saved) != nil || saved.SchemaVersion != 1 || saved.Owner != expected {
		return fail()
	}
	encoded, err := canonical.Marshal(saved)
	if err != nil || !bytes.Equal(raw, encoded) {
		return fail()
	}
	return RestoreNegativeTaskDescriptor(ctx, []byte(saved.DescriptorJCS), qualified)
}
