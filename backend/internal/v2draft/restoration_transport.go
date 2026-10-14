package v2draft

import (
	"context"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// PublishInputClosure emits one bounded chunk and a manifest after Stage has
// committed. The returned graph still requires SQL registration and sealing.
func (d RestorationResolvedDraft) PublishInputClosure(ctx context.Context,
	paramsRaw []byte, publisher v2input.Publisher) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if publisher == nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := d.Descriptor(paramsRaw); err != nil {
		return zero, err
	}
	entries := append([]v2input.Entry(nil), d.Draft.Entries...)
	open := func(c context.Context) (v2input.Cursor, error) {
		if err := c.Err(); err != nil {
			return nil, err
		}
		return &donorInputCursor{entries: append([]v2input.Entry(nil), entries...)}, nil
	}
	built, err := v2input.BuildInputClosure(ctx, d.Draft.Task.ID,
		&donorInputCursor{entries: append([]v2input.Entry(nil), entries...)}, publisher)
	if err != nil {
		return zero, err
	}
	if built.Count != len(entries) || len(built.Chunks) != 1 {
		return zero, v2input.ErrInvalid
	}
	refs := make([]v2input.Ref, len(entries))
	used := make(map[uuid.UUID]bool, len(entries))
	for i, entry := range entries {
		refs[i] = entry.Ref
		used[entry.Ref.ArtifactID] = true
	}
	if used[built.Manifest.ArtifactID] || used[built.Chunks[0].ArtifactID] ||
		built.Manifest.ArtifactID == built.Chunks[0].ArtifactID {
		return zero, v2input.ErrInvalid
	}
	return v2input.GraphTask{ID: d.Draft.Task.ID, Built: built, Open: open, Direct: refs}, nil
}

// RegisterTransport registers only the frozen descriptor inventory. Stage and
// exact input sealing run in one scheduler transaction; graph activation is a
// separate live-authority gate.
func (s SQLRestorationDescriptorStore) RegisterTransport(ctx context.Context,
	who auth.Identity, tenant, run uuid.UUID, prepared RestorationResolvedDraft,
	paramsRaw []byte, transport v2input.GraphTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	descriptor, err := prepared.Descriptor(paramsRaw)
	if err != nil {
		return err
	}
	entries := append([]v2input.Entry(nil), prepared.Draft.Entries...)
	if transport.ID != prepared.Draft.Task.ID || transport.Built.Count != len(entries) ||
		len(transport.Built.Chunks) != 1 || len(transport.Direct) != len(entries) {
		return v2input.ErrInvalid
	}
	sha, err := canonical.Hash(entries)
	if err != nil || transport.Built.SHA256 != sha {
		return v2input.ErrInvalid
	}
	direct := make([]v2input.Ref, len(entries))
	for i, entry := range entries {
		if transport.Direct[i] != entry.Ref {
			return v2input.ErrInvalid
		}
		direct[i] = entry.Ref
	}
	built := transport.Built
	built.Chunks = append([]v2input.Ref(nil), built.Chunks...)
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
	if err = tx.QueryRow(bounded, "SELECT photo.stage_v28_restoration_task($1,$2,$3,$4,$5,$6)",
		tenant, run, who.UserID, who.SessionID, string(descriptor), string(paramsRaw)).Scan(&ready); err != nil {
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
	if err = v2input.RegisterInTransaction(bounded, tx, tenant, run, prepared.Draft.Task.ID,
		built, open, direct); err != nil {
		return err
	}
	if err = bounded.Err(); err != nil {
		return err
	}
	return tx.Commit(bounded)
}

// PublishAndRegisterTransport stages first, publishes with current scoped
// document authority outside SQL, then registers the exact frozen inventory.
func (s SQLRestorationDescriptorStore) PublishAndRegisterTransport(ctx context.Context,
	who auth.Identity, tenant, run uuid.UUID, prepared RestorationResolvedDraft,
	paramsRaw []byte, documents v2input.DocumentPublisher) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	if documents == nil {
		return zero, v2input.ErrInvalid
	}
	if err := s.Stage(ctx, who, tenant, run, prepared, paramsRaw); err != nil {
		return zero, err
	}
	graph, err := prepared.PublishInputClosure(ctx, paramsRaw,
		v2input.ScopedPublisher{Documents: documents, Tenant: tenant, Run: run})
	if err != nil {
		return zero, err
	}
	if err = s.RegisterTransport(ctx, who, tenant, run, prepared, paramsRaw, graph); err != nil {
		return zero, err
	}
	return graph, nil
}
