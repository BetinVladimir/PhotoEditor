package delivery

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

type delegatedSignerCursor struct {
	entries []v2input.Entry
	index int
}

func (c *delegatedSignerCursor) Next(ctx context.Context) (v2input.Entry, bool, error) {
	if err := ctx.Err(); err != nil { return v2input.Entry{}, false, err }
	if c.index == len(c.entries) { return v2input.Entry{}, false, nil }
	entry := c.entries[c.index]
	c.index++
	return entry, true, nil
}

type DelegatedSignerTransport struct {
	entries []v2input.Entry
	built v2input.Built
}

// PublishDelegatedSignerInputs writes bounded transport documents after the
// blocked task commits. It does no input registration or activation.
func PublishDelegatedSignerInputs(ctx context.Context,
	store SQLDelegatedLineageStore, lineageRef v2input.Ref, lineageBody []byte,
	intent provenance.FrozenSigningIntent,
	task provenance.FrozenSigningTask,
	publisher v2input.Publisher) (DelegatedSignerTransport, error) {
	var zero DelegatedSignerTransport
	if err := ctx.Err(); err != nil { return zero, err }
	if publisher == nil { return zero, v2input.ErrInvalid }
	if _, err := BuildDelegatedSigningDraftDocuments(ctx, store.Scope, intent, task); err != nil {
		return zero, err
	}
	if err := store.VerifyReady(ctx, lineageRef, lineageBody); err != nil {
		return zero, err
	}
	entries := task.Inputs()
	built, err := v2input.BuildInputClosure(ctx, store.Scope.TaskID,
		&delegatedSignerCursor{entries: entries}, publisher)
	if err != nil { return zero, err }
	if built.Count != 3 || len(built.Chunks) != 1 ||
		built.SHA256 != task.Task().InputDigest ||
		built.Manifest.ArtifactID == uuid.Nil { return zero, v2input.ErrInvalid }
	seen := map[uuid.UUID]bool{task.Task().ParamsArtifactID: true,
		built.Manifest.ArtifactID: true}
	for _, ref := range built.Chunks {
		if seen[ref.ArtifactID] { return zero, v2input.ErrInvalid }
		seen[ref.ArtifactID] = true
	}
	for _, entry := range entries {
		if seen[entry.Ref.ArtifactID] { return zero, v2input.ErrInvalid }
		seen[entry.Ref.ArtifactID] = true
	}
	if err := store.VerifyReady(ctx, lineageRef, lineageBody); err != nil {
		return zero, err
	}
	return DelegatedSignerTransport{entries: append([]v2input.Entry(nil), entries...),
		built: built}, nil
}

// Register writes and seals the three task inputs in the caller's scheduler
// transaction. The live parent gate is repeated on both sides of registration.
func (t DelegatedSignerTransport) Register(ctx context.Context, tx pgx.Tx,
	store SQLDelegatedLineageStore, lineageRef v2input.Ref, lineageBody []byte,
	intent provenance.FrozenSigningIntent,
	task provenance.FrozenSigningTask) error {
	if err := ctx.Err(); err != nil { return err }
	if tx == nil { return v2input.ErrInvalid }
	docs, err := BuildDelegatedSigningDraftDocuments(ctx, store.Scope, intent, task)
	if err != nil { return err }
	expected, err := canonical.Marshal(task.Inputs())
	if err != nil { return err }
	actual, err := canonical.Marshal(t.entries)
	if err != nil || !bytes.Equal(expected, actual) ||
		digest(actual) != t.built.SHA256 || t.built.Count != 3 ||
		len(t.built.Chunks) != 1 || t.built.SHA256 != task.Task().InputDigest {
		return v2input.ErrInvalid
	}
	pins, err := store.scopePins(lineageRef, lineageBody)
	if err != nil { return err }
	taskBody, err := canonical.Marshal(task.Task())
	if err != nil { return err }
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`,
		store.Scope.TenantID.String()); err != nil { return err }
	check := func() error {
		var ready bool
		err := tx.QueryRow(ctx,`SELECT photo.v31_delivery_signer_transport_ready(
		 $1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9::jsonb)`,
			store.Lease.Expansion, store.Lease.Token, store.Slot.Ordinal,
			store.Slot.PresetID, string(pins), int64(len(lineageBody)),
			string(docs.Params), store.Bucket, string(taskBody)).Scan(&ready)
		if err != nil { return err }
		if !ready { return v2input.ErrInvalid }
		return nil
	}
	if err := check(); err != nil { return err }
	direct := []v2input.Ref{docs.ParamsRef}
	for _, entry := range t.entries { direct = append(direct, entry.Ref) }
	open := func(ctx context.Context) (v2input.Cursor, error) {
		if err := ctx.Err(); err != nil { return nil, err }
		return &delegatedSignerCursor{entries: append([]v2input.Entry(nil), t.entries...)}, nil
	}
	if err := v2input.RegisterInTransaction(ctx, tx, store.Scope.TenantID,
		intent.RunID(), store.Scope.TaskID, t.built, open, direct); err != nil {
		return err
	}
	if err := check(); err != nil { return err }
	return ctx.Err()
}
