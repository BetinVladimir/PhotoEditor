package delivery

import (
	"context"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// MaterializeDelegatedSignerTask persists only a blocked, quota-reserved task.
// SQL independently rechecks the current parent, draft and ready Params.
func MaterializeDelegatedSignerTask(ctx context.Context,
	store SQLDelegatedLineageStore, lineageRef v2input.Ref, lineageBody []byte,
	intent provenance.FrozenSigningIntent,
	task provenance.FrozenSigningTask) error {
	if err := ctx.Err(); err != nil { return err }
	if store.DB == nil { return v2input.ErrInvalid }
	docs, err := BuildDelegatedSigningDraftDocuments(ctx, store.Scope, intent, task)
	if err != nil { return err }
	pins, err := store.scopePins(lineageRef, lineageBody)
	if err != nil { return err }
	if err := store.VerifyReady(ctx, lineageRef, lineageBody); err != nil {
		return err
	}
	taskBody, err := canonical.Marshal(task.Task())
	if err != nil { return err }
	var accepted bool
	err = store.DB.QueryRow(ctx,`SELECT photo.materialize_v31_delivery_signer_task(
	 $1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9::jsonb)`,
		store.Lease.Expansion, store.Lease.Token, store.Slot.Ordinal,
		store.Slot.PresetID, string(pins), int64(len(lineageBody)),
		string(docs.Params), store.Bucket, string(taskBody)).Scan(&accepted)
	if err != nil { return err }
	if !accepted { return v2input.ErrInvalid }
	return ctx.Err()
}
