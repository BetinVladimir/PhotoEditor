package v2portrait

import (
	"bytes"
	"context"

	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Descriptor is bounded immutable recovery data, not publication/admission
// authority. Recovery must freshly prepare original evidence and compare it.
func (f FrozenRelightTask) Descriptor() ([]byte, error) {
	task, err := f.DraftTask()
	if err != nil || f.reservedBytes < 1 {
		return nil, v2input.ErrInvalid
	}
	return canonical.Marshal(struct {
		SchemaVersion int               `json:"schema_version"`
		Task          v2input.DraftTask `json:"task"`
		Entries       []v2input.Entry   `json:"entries"`
		ReservedBytes int64             `json:"reserved_bytes"`
	}{1, task, f.entries, f.reservedBytes})
}

// StageRelightTask stores one blocked task and an output/transport reservation.
// Both full live producer authority and the exact published Params owner are
// checked in its transaction. No transport, event, outbox or receipt is created.
func (s RelightBootstrapCoordinator) StageRelightTask(ctx context.Context, prepared RelightBootstrapResult, frozen FrozenRelightTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || prepared.prepared == nil {
		return v2input.ErrInvalid
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return err
	}
	rebuilt, err := freezePreparedRelightTask(ctx, prepared, artifactjson.Ref{ID: draft.ParamsArtifactID, SHA256: draft.ParamsSHA})
	if err != nil {
		return err
	}
	descriptor, err := frozen.Descriptor()
	if err != nil {
		return err
	}
	expected, err := rebuilt.Descriptor()
	if err != nil || !bytes.Equal(descriptor, expected) {
		return v2input.ErrInvalid
	}
	w := prepared.prepared
	scope := artifactjson.Scope{Tenant: w.request.Tenant, Run: w.request.Run, Owner: w.intent.OwnerID, Purpose: "task_params"}
	proof, err := (relightParamsRepository{coordinator: s, prepared: prepared}).proof(scope, draft.ParamsSHA, int64(len(w.intent.ParamsJCS)))
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = s.RevalidatePrepared(ctx, tx, prepared); err != nil {
		return err
	}
	var ready bool
	if err = tx.QueryRow(ctx, "SELECT photo.stage_v28_relight_task($1,$2,$3,$4::jsonb)", w.request.Tenant, w.request.Run, string(descriptor), proof).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return tx.Commit(ctx)
}
