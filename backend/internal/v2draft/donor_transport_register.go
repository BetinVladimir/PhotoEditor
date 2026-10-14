package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"time"
)

// RegisterTransport records an already-published closure for a qualified blocked
// task. The supplied cursor is ignored: registration uses owned compiler entries.
// Stage authority, batch registration and input sealing share one transaction.
// This does not seal the graph or queue execution. A retry after committed input
// sealing requires the independent retained-transport recovery path.
func (s SQLDonorDescriptorStore) RegisterTransport(ctx context.Context, who auth.Identity, tenant, run uuid.UUID,
	frozen FrozenDonorTask, transport v2input.GraphTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || run == uuid.Nil || frozen.qualification == nil {
		return v2input.ErrInvalid
	}
	if _, err := frozen.qualification.Qualification(); err != nil {
		return err
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return err
	}
	if transport.ID != draft.ID || transport.Built.Count != len(frozen.entries) || len(transport.Built.Chunks) != 1 || len(transport.Direct) != len(frozen.entries) {
		return v2input.ErrInvalid
	}
	sha, err := canonical.Hash(frozen.entries)
	if err != nil || transport.Built.SHA256 != sha {
		return v2input.ErrInvalid
	}
	entries := append([]v2input.Entry(nil), frozen.entries...)
	direct := make([]v2input.Ref, len(entries))
	for i, e := range entries {
		if transport.Direct[i] != e.Ref {
			return v2input.ErrInvalid
		}
		direct[i] = e.Ref
	}
	built := transport.Built
	built.Chunks = append([]v2input.Ref(nil), built.Chunks...)
	descriptor, err := frozen.Descriptor()
	if err != nil {
		return err
	}
	params := append([]byte(nil), frozen.params...)
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(bounded)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	defer rollbackDonorRecovery(bounded, tx)
	if _, err = tx.Exec(bounded, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	var role string
	if err = tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	if err = tx.QueryRow(bounded, "SELECT photo.stage_v28_donor_task($1,$2,$3,$4,$5,$6)", tenant, run, who.UserID, who.SessionID, string(descriptor), string(params)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	open := func(c context.Context) (v2input.Cursor, error) {
		if err := c.Err(); err != nil {
			return nil, err
		}
		return &donorInputCursor{entries: append([]v2input.Entry(nil), entries...)}, nil
	}
	if err = v2input.RegisterInTransaction(bounded, tx, tenant, run, draft.ID, built, open, direct); err != nil {
		return err
	}
	if err = bounded.Err(); err != nil {
		return err
	}
	return tx.Commit(bounded)
}
