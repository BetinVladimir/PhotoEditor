package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type donorTaskDescriptor struct {
	QualificationObjects []v2portrait.DonorQualificationObject `json:"qualification_objects"`
	SchemaVersion        int                                   `json:"schema_version"`
	Task                 v2input.DraftTask                     `json:"task"`
	Entries              []v2input.Entry                       `json:"entries"`
	Params               json.RawMessage                       `json:"params"`
	ReservedBytes        int64                                 `json:"reserved_bytes"`
}

// Descriptor is immutable recovery data, not a grant. Persist it with the
// owning workflow; re-load original evidence and live authority before recovery.
func (f FrozenDonorTask) Descriptor() ([]byte, error) {
	task, err := f.DraftTask()
	if err != nil {
		return nil, err
	}
	if len(f.params) == 0 || f.reservedBytes < 1 {
		return nil, v2input.ErrInvalid
	}
	var objects []v2portrait.DonorQualificationObject
	if f.qualification != nil {
		objects, err = f.qualification.Objects()
		if err != nil {
			return nil, err
		}
	}
	return canonical.Marshal(donorTaskDescriptor{SchemaVersion: 1, Task: task, Entries: append([]v2input.Entry(nil), f.entries...), Params: append(json.RawMessage(nil), f.params...), ReservedBytes: f.reservedBytes, QualificationObjects: objects})
}

// RestoreDonorTaskDescriptor recompiles from freshly loaded evidence and exact
// published Params, then compares the entire canonical descriptor. Stored data
// never bypasses geometry, inventory, publication-plan or qualification checks.
func RestoreDonorTaskDescriptor(ctx context.Context, raw []byte, workflow uuid.UUID, ordinal int,
	loaded LoadedDonorTaskPlan, prepared PreparedDonorInputEvidence, published v2portrait.PublishedDonorTaskPlan) (FrozenDonorTask, error) {
	var zero FrozenDonorTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 1<<20 {
		return zero, v2input.ErrInvalid
	}
	owned := append([]byte(nil), raw...)
	body, err := canonical.Bytes(owned)
	if err != nil || !bytes.Equal(body, owned) {
		return zero, v2input.ErrInvalid
	}
	frozen, err := FreezeDonorTask(ctx, workflow, ordinal, loaded, prepared, published)
	if err != nil {
		return zero, err
	}
	expected, err := frozen.Descriptor()
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(expected, owned) {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return frozen, nil
}

func RestoreQualifiedDonorTaskDescriptor(ctx context.Context, raw []byte, workflow uuid.UUID, ordinal int,
	loaded LoadedDonorTaskPlan, prepared PreparedDonorInputEvidence, published v2portrait.PublishedDonorTaskPlan) (FrozenDonorTask, error) {
	if _, err := verifyPreparedDonorQualification(loaded, prepared); err != nil {
		return FrozenDonorTask{}, err
	}
	return RestoreDonorTaskDescriptor(ctx, raw, workflow, ordinal, loaded, prepared, published)
}
