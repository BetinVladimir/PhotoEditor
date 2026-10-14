package v2portrait

import (
	"bytes"
	"context"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
	"time"
)

// AdmitRelightGraph commits dedicated live authority, generic activation and
// queued receipt atomically. A commit error requires receipt recovery; callers
// must not assume that an unknown commit means no work was queued.
func (s RelightBootstrapCoordinator) AdmitRelightGraph(ctx context.Context, prepared RelightBootstrapResult, frozen FrozenRelightTask) error {
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

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	defer tx.Rollback(context.Background())
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", w.request.Tenant.String()); err != nil {
		return err
	}
	var ready bool
	if err = tx.QueryRow(ctx, "SELECT photo.seal_v28_relight_graph($1,$2,$3,$4,$5::jsonb)", w.request.Tenant, w.request.Run, draft.ID, string(descriptor), proof).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
