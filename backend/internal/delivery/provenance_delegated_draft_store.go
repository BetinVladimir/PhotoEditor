package delivery

import (
	"context"

	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// BeginDelegatedSigningDraft creates only the empty run/workflow/snapshot
// containers under the current parent lease. Params publication, task
// materialization, transport and activation remain separate fenced stages.
func BeginDelegatedSigningDraft(ctx context.Context,
	store SQLDelegatedLineageStore, lineageRef v2input.Ref, lineageBody []byte,
	intent provenance.FrozenSigningIntent,
	task provenance.FrozenSigningTask) error {
	if err := ctx.Err(); err != nil { return err }
	if store.DB == nil { return v2input.ErrInvalid }
	docs, err := BuildDelegatedSigningDraftDocuments(ctx, store.Scope, intent, task)
	if err != nil { return err }
	pins, err := store.scopePins(lineageRef, lineageBody)
	if err != nil { return err }
	if err := store.VerifyReady(ctx, lineageRef, lineageBody); err != nil { return err }
	var accepted bool
	err = store.DB.QueryRow(ctx,`SELECT photo.begin_v31_delivery_signer_draft(
	 $1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10)`,
		store.Lease.Expansion, store.Lease.Token, store.Slot.Ordinal,
		store.Slot.PresetID, string(pins), int64(len(lineageBody)),
		string(docs.Config), string(docs.Inputs), string(docs.Params),
		store.Bucket).Scan(&accepted)
	if err != nil { return err }
	if !accepted { return v2input.ErrInvalid }
	return ctx.Err()
}
