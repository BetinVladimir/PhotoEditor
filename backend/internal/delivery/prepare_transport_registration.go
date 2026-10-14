package delivery

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// RegisterInputsInTransaction uses a trusted scheduler transaction. It binds
// transport to the owned inventory, handles exact registered replay, and seals
// task inputs only. Caller must roll back on any error. Source proof/package
// admission and graph activation remain mandatory subsequent authority gates.
func (f FrozenPrepareTask) RegisterInputsInTransaction(ctx context.Context, tx pgx.Tx, tid, run uuid.UUID, graph v2input.GraphTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tid == uuid.Nil || run == uuid.Nil {
		return v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return err
	}
	expected, err := v2input.TaskID(workflow, "v2.delivery.prepare", 0)
	if err != nil || expected != f.draft.ID {
		return v2input.ErrInvalid
	}
	bound, err := f.BindPublishedInputs(ctx, graph)
	if err != nil {
		return err
	}
	draft, err := f.DraftTask()
	if err != nil {
		return err
	}
	taskRaw, err := canonical.Marshal(draft)
	if err != nil {
		return err
	}
	transportRaw, err := canonical.Marshal(struct {
		Manifest v2input.Ref   `json:"manifest"`
		Chunks   []v2input.Ref `json:"chunks"`
		Count    int           `json:"count"`
		SHA      string        `json:"sha256"`
	}{bound.Built.Manifest, bound.Built.Chunks, bound.Built.Count, bound.Built.SHA256})
	if err != nil {
		return err
	}
	ready := func() (bool, error) {
		var value bool
		err := tx.QueryRow(ctx, `SELECT photo.v30_delivery_prepare_transport_ready($1,$2,$3::jsonb,$4::jsonb)`, tid, run, string(taskRaw), string(transportRaw)).Scan(&value)
		return value, err
	}
	if ok, err := ready(); err != nil || ok {
		return err
	}
	if err := v2input.RegisterInTransaction(ctx, tx, tid, run, draft.ID, bound.Built, f.closure.Open, f.closure.Direct); err != nil {
		return err
	}
	ok, err := ready()
	if err != nil {
		return err
	}
	if !ok {
		return v2input.ErrInvalid
	}
	return nil
}
