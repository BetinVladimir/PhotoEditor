package delivery

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// ActivateDelegatedSignerInTransaction registers the exact published closure
// and seals the child atomically. The caller must repeat operator/resource and
// physical export qualification before entering this scheduler transaction;
// the ticket is visible only after its commit.
func ActivateDelegatedSignerInTransaction(ctx context.Context, tx pgx.Tx,
	store SQLDelegatedLineageStore, lineageRef v2input.Ref, lineageBody []byte,
	intent provenance.FrozenSigningIntent, task provenance.FrozenSigningTask,
	transport DelegatedSignerTransport) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil { return zero, err }
	if tx == nil || store.DB == nil { return zero, v2input.ErrInvalid }
	docs, err := BuildDelegatedSigningDraftDocuments(ctx, store.Scope, intent, task)
	if err != nil { return zero, err }
	pins, err := store.scopePins(lineageRef, lineageBody)
	if err != nil { return zero, err }
	if transport.built.Count != 3 || len(transport.built.Chunks) != 1 ||
		transport.built.SHA256 != task.Task().InputDigest ||
		transport.built.Manifest.ArtifactID == uuid.Nil ||
		!shaPattern.MatchString(transport.built.Manifest.SHA256) {
		return zero, v2input.ErrInvalid
	}
	taskBody, err := canonical.Marshal(task.Task())
	if err != nil { return zero, err }
	manifest, err := canonical.Marshal(transport.built.Manifest)
	if err != nil { return zero, err }
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`,
		store.Scope.TenantID.String()); err != nil { return zero, err }
	activate := func() (bool, error) {
		var accepted bool
		err := tx.QueryRow(ctx,`SELECT photo.activate_v31_delivery_signer(
		 $1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9::jsonb,$10::jsonb)`,
			store.Lease.Expansion, store.Lease.Token, store.Slot.Ordinal,
			store.Slot.PresetID, string(pins), int64(len(lineageBody)),
			string(docs.Params), store.Bucket, string(taskBody),
			string(manifest)).Scan(&accepted)
		return accepted, err
	}
	accepted, err := activate()
	if err != nil { return zero, err }
	if !accepted {
		if err := transport.Register(ctx, tx, store, lineageRef, lineageBody,
			intent, task); err != nil { return zero, err }
		accepted, err = activate()
		if err != nil { return zero, err }
		if !accepted { return zero, v2input.ErrInvalid }
	}
	if err := ctx.Err(); err != nil { return zero, err }
	return v2input.JobTicket{RunID: intent.RunID(),
		WorkflowID: intent.WorkflowID(), State: "queued"}, nil
}
