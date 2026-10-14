package delivery

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// DelegatedSigningResources is the installed measurement and operator gate.
// A caller-supplied execution profile cannot satisfy this interface.
type DelegatedSigningResources interface {
	QualifyDelegatedSigningResources(context.Context, uuid.UUID,
		provenance.FrozenSigningIntent,
		provenance.ReadySigningExport) (provenance.SigningTaskResources, error)
}

// DispatchCurrentDelegatedSigner joins the durable stages of a previously
// compiled child. Every stage retains its own SQL parent-lease gate; this
// coordinator additionally brackets object IO and activation with fresh export
// and installed resource/operator checks. A returned ticket means queued only.
func DispatchCurrentDelegatedSigner(ctx context.Context,
	store SQLDelegatedLineageStore, lineageRef v2input.Ref, lineageBody []byte,
	intent provenance.FrozenSigningIntent, task provenance.FrozenSigningTask,
	objects artifactjson.Objects, resources DelegatedSigningResources,
) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil { return zero, err }
	if store.DB == nil || objects == nil || resources == nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := BuildDelegatedSigningDraftDocuments(ctx, store.Scope, intent, task); err != nil {
		return zero, err
	}
	// A previous call may have committed the seal before its acknowledgement
	// was lost. The draft function intentionally refuses a sealed run.
	if recovered, ok, err := RecoverDelegatedSignerTicket(ctx, store.DB,
		store.Lease, store.Slot, store.Scope); err != nil {
		return zero, err
	} else if ok {
		if recovered.WorkflowID != intent.WorkflowID() ||
			recovered.RunID != intent.RunID() {
			return zero, v2input.ErrInvalid
		}
		return recovered, nil
	}
	recheck := func() error {
		scope, ready, err := ReadDelegatedReadyExport(ctx, store.DB,
			store.Lease, store.Slot, store.Bucket)
		if err != nil { return err }
		if scope != store.Scope || ready != store.Ready {
			return v2input.ErrInvalid
		}
		if err := store.VerifyReady(ctx, lineageRef, lineageBody); err != nil {
			return err
		}
		measured, err := resources.QualifyDelegatedSigningResources(ctx,
			scope.ActorID, intent, ready)
		if err != nil { return err }
		if measured != task.Resources() { return v2input.ErrInvalid }
		return ctx.Err()
	}
	if err := recheck(); err != nil { return zero, err }
	if err := BeginDelegatedSigningDraft(ctx, store, lineageRef, lineageBody,
		intent, task); err != nil { return zero, err }
	if _, err := PublishDelegatedSigningParams(ctx, store, lineageRef,
		lineageBody, intent, task, objects); err != nil { return zero, err }
	if err := MaterializeDelegatedSignerTask(ctx, store, lineageRef,
		lineageBody, intent, task); err != nil { return zero, err }
	transport, err := PublishCurrentDelegatedSignerInputs(ctx, store,
		lineageRef, lineageBody, intent, task, objects)
	if err != nil { return zero, err }
	if err := recheck(); err != nil { return zero, err }
	tx, err := store.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return zero, err }
	defer tx.Rollback(ctx)
	ticket, err := ActivateDelegatedSignerInTransaction(ctx, tx, store,
		lineageRef, lineageBody, intent, task, transport)
	if err != nil { return zero, err }
	if err := tx.Commit(ctx); err != nil {
		// A lost acknowledgement may follow a committed seal. Never start a
		// second signer until the current parent slot is queried for that seal.
		if ctx.Err() == nil {
			recovered, ok, recoveryErr := RecoverDelegatedSignerTicket(ctx,
				store.DB, store.Lease, store.Slot, store.Scope)
			if recoveryErr == nil && ok && recovered == ticket {
				return recovered, nil
			}
		}
		return zero, err
	}
	return ticket, nil
}
