package delivery

import (
	"context"

	"photoeditor/backend/internal/v2input"
)

// SigningSlotDispatch must perform the delegated V31 compiler, immutable
// lineage publication and current admission for this exact ready slot.
type SigningSlotDispatch func(context.Context, ExpansionLease, Slot) error

// ProcessCurrentSigningPage scans one persisted page and starts only children
// that are not yet terminal. Its result never grants package assembly credit:
// adopted SQL status still requires complete physical closure verification.
func ProcessCurrentSigningPage(ctx context.Context,
	store ExpansionLeaseStore, observations MatrixObservationStore,
	plan PreparePlan, lease ExpansionLease, limit int,
	dispatch SigningSlotDispatch) (int, error) {
	dispatched, failed, _, err := ProcessCurrentSigningPageWithFailure(ctx, store,
		observations, plan, lease, limit, dispatch)
	if failed != nil { return dispatched, v2input.ErrInvalid }
	return dispatched, err
}

// ProcessCurrentSigningPageWithFailure exposes a terminal signer observed in
// the bounded page and marks the end of one persisted round-robin sweep. The
// caller must recheck a failure in the fenced SQL finalizer and may run the
// full physical closure only at a sweep boundary.
func ProcessCurrentSigningPageWithFailure(ctx context.Context,
	store ExpansionLeaseStore, observations MatrixObservationStore,
	plan PreparePlan, lease ExpansionLease, limit int,
	dispatch SigningSlotDispatch) (int, *Slot, bool, error) {
	if err := ctx.Err(); err != nil { return 0, nil, false, err }
	if store.DB == nil || observations == nil || dispatch == nil ||
		!lease.valid() || lease.ParentTask != plan.TaskID() {
		return 0, nil, false, v2input.ErrInvalid
	}
	choice, err := plan.SigningChoice()
	if err != nil { return 0, nil, false, err }
	actor, err := store.ReadSigningParentActor(ctx, lease)
	if err != nil { return 0, nil, false, err }
	cursor, err := store.NextSigningPage(ctx, lease, limit)
	if err != nil { return 0, nil, false, err }
	slots, _, err := plan.ReadReadySigningPage(ctx, observations, lease,
		cursor, limit)
	if err != nil { return 0, nil, false, err }
	sweepComplete := cursor+limit >= lease.SlotCount
	dispatched := 0
	for _, slot := range slots {
		if err := ctx.Err(); err != nil { return dispatched, nil, false, err }
		dependencies, err := CompileSigningDependencies(lease.Run,
			choice.PolicyID, []Slot{slot})
		if err != nil || len(dependencies) != 1 { return dispatched, nil, false, v2input.ErrInvalid }
		status, err := ReadDeliverySignerStatus(ctx, store.DB, lease,
			actor, dependencies[0])
		if err != nil { return dispatched, nil, false, err }
		switch status.State {
		case "not_started", "pending":
			if err := observations.Renew(ctx, lease); err != nil { return dispatched, nil, false, err }
			if err := dispatch(ctx, lease, slot); err != nil { return dispatched, nil, false, err }
			dispatched++
		case "failed", "cancelled":
			failed := slot
			return dispatched, &failed, false, nil
		case "succeeded_unverified", "adopted":
		default:
			return dispatched, nil, false, v2input.ErrInvalid
		}
	}
	return dispatched, nil, sweepComplete, ctx.Err()
}
