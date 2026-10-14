package delivery

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// DelegatedSigningSlotDispatcher installs all V31 child preparation gates for
// one current Delivery slot. It returns after queueing; signed-result adoption
// and complete package closure remain separate scheduler stages.
type DelegatedSigningSlotDispatcher struct {
	DB *pgxpool.Pool
	Reads provenance.SigningObjects
	Writes artifactjson.Objects
	Bucket string
	WorkICCSHA string
	Policies *provenance.SigningPolicyRegistry
	NegativePolicies *provenance.SigningNegativePolicyRegistry
	Resources DelegatedSigningResources
	Clock func() time.Time
}

func (d DelegatedSigningSlotDispatcher) Dispatch(ctx context.Context,
	lease ExpansionLease, slot Slot) error {
	if err := ctx.Err(); err != nil { return err }
	if d.DB == nil || d.Reads == nil || d.Writes == nil ||
		d.Policies == nil || d.NegativePolicies == nil ||
		d.Resources == nil || d.Clock == nil ||
		len(d.Bucket) < 3 || len(d.Bucket) > 63 ||
		!shaPattern.MatchString(d.WorkICCSHA) || !lease.valid() ||
		lease.Cursor != lease.SlotCount || slot.State != "ready" {
		return v2input.ErrInvalid
	}
	scope, ready, err := ReadDelegatedReadyExport(ctx, d.DB, lease,
		slot, d.Bucket)
	if err != nil { return err }
	store := SQLDelegatedLineageStore{DB: d.DB, Lease: lease, Slot: slot,
		Scope: scope, Ready: ready, Bucket: d.Bucket}
	lineageRef, lineageBody, err := CompileAndPublishCurrentDelegatedLineage(
		ctx, store, d.Reads, d.Writes, d.NegativePolicies,
		d.WorkICCSHA)
	if err != nil { return err }
	tenantPolicies, err := d.Policies.ForTenant(lease.Tenant)
	if err != nil { return err }
	currentScope, currentReady, intent, task, err :=
		CompileQualifiedCurrentDelegatedSignerChild(ctx, d.DB, lease,
			slot, d.Bucket, lineageRef, lineageBody,
			tenantPolicies, d.Resources, d.Clock())
	if err != nil { return err }
	if currentScope != scope || currentReady != ready {
		return v2input.ErrInvalid
	}
	store.Scope, store.Ready = currentScope, currentReady
	ticket, err := DispatchCurrentDelegatedSigner(ctx, store, lineageRef,
		lineageBody, intent, task, d.Writes, d.Resources)
	if err != nil { return err }
	if ticket.WorkflowID != scope.WorkflowID ||
		ticket.RunID != uuid.NewSHA1(scope.WorkflowID, []byte("run")) ||
		ticket.State != "queued" {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}
