package delivery

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// DelegatedSigningClosureCoordinator verifies the complete frozen Delivery
// matrix without a persisted editor session. Only a fully adopted and
// physically checked set can be retained for later signed assembly admission.
type DelegatedSigningClosureCoordinator struct {
	Store ExpansionLeaseStore
	Observations MatrixObservationStore
	Policies *provenance.SigningPolicyRegistry
	Clock func() time.Time
	Objects SignedDependencyObjectReader
	Bucket string
}

// VerifiedDelegatedSigningSet contains only evidence produced by the current
// complete physical verifier. Signed publication still repeats SQL authority.
type VerifiedDelegatedSigningSet struct {
	Actor uuid.UUID
	Frozen FrozenInput
	SnapshotRaw []byte
	Slots []Slot
	Dependencies []SigningDependency
	Receipts []SignedDependencyReceipt
	Closure SignedAssemblyClosure
	FailedSlot *Slot
}

func (c DelegatedSigningClosureCoordinator) VerifyAndRetain(ctx context.Context,
	plan PreparePlan, lease ExpansionLease,
) (string, SignedAssemblyClosure, error) {
	state, evidence, err := c.VerifyAndRetainEvidence(ctx, plan, lease)
	return state, evidence.Closure, err
}

func (c DelegatedSigningClosureCoordinator) VerifyAndRetainEvidence(
	ctx context.Context, plan PreparePlan, lease ExpansionLease,
) (string, VerifiedDelegatedSigningSet, error) {
	var zero VerifiedDelegatedSigningSet
	if err := ctx.Err(); err != nil { return "", zero, err }
	if c.Store.DB == nil || c.Observations == nil || c.Policies == nil ||
		c.Clock == nil || c.Objects == nil || len(c.Bucket) < 3 ||
		len(c.Bucket) > 63 || !lease.valid() ||
		lease.Cursor != lease.SlotCount || lease.ParentTask != plan.TaskID() {
		return "", zero, v2input.ErrInvalid
	}
	var proof PrepareProof
	var frozen FrozenInput
	if json.Unmarshal(plan.ProofJCS(), &proof) != nil ||
		json.Unmarshal([]byte(proof.FrozenInputJCS), &frozen) != nil ||
		len(proof.SnapshotJCS) < 2 {
		return "", zero, v2input.ErrInvalid
	}
	choice, err := DeliverySigningChoice(frozen)
	if err != nil { return "", zero, err }
	actor, err := c.Store.ReadSigningParentActor(ctx, lease)
	if err != nil { return "", zero, err }
	observations, state, err := plan.ReadObservedMatrix(ctx,
		c.Observations, lease)
	if err != nil { return "", zero, err }
	if state == "pending" || state == "failed" { return state, zero, nil }
	if state != "assemble" { return "", zero, v2input.ErrInvalid }
	slots, currentState, err := plan.ObserveMatrix(ctx, lease.Run, observations)
	if err != nil { return "", zero, err }
	if currentState != "assemble" || len(slots) != lease.SlotCount {
		return "", zero, v2input.ErrInvalid
	}
	dependencies, err := CompileSigningDependencies(lease.Run,
		choice.PolicyID, slots)
	if err != nil { return "", zero, err }
	receipts := make([]SignedDependencyReceipt, 0, len(dependencies))
	bySlot := make(map[struct{ ordinal int; preset string }]Slot, len(slots))
	for _, slot := range slots {
		bySlot[struct{ ordinal int; preset string }{slot.Ordinal,
			slot.PresetID.String()}] = slot
	}
	for index, dependency := range dependencies {
		if err := ctx.Err(); err != nil { return "", zero, err }
		if index%25 == 0 {
			if err := c.Observations.Renew(ctx, lease); err != nil {
				return "", zero, err
			}
		}
		slot, exists := bySlot[struct{ ordinal int; preset string }{
			dependency.Ordinal, dependency.PresetID.String()}]
		if !exists { return "", zero, v2input.ErrInvalid }
		status, err := ReadDeliverySignerStatus(ctx, c.Store.DB,
			lease, actor, dependency)
		if err != nil { return "", zero, err }
		switch status.State {
		case "failed", "cancelled":
			failed := slot
			return "failed", VerifiedDelegatedSigningSet{FailedSlot: &failed}, nil
		case "not_started", "pending", "succeeded_unverified":
			return "pending", zero, nil
		case "adopted":
		default: return "", zero, v2input.ErrInvalid
		}
		read := func(call context.Context) (SignedDependencyReceipt, bool, error) {
			return ReadDelegatedSignedDependencyReceipt(call, c.Store.DB,
				lease, slot, dependency, c.Bucket)
		}
		receipt, err := VerifyDelegatedSigningDependencyCurrent(ctx,
			lease, actor, slot, dependency, choice.PolicySHA,
			c.Policies, c.Clock, c.Objects, read)
		if err != nil { return "", zero, err }
		receipts = append(receipts, receipt)
	}
	if err := c.Observations.Renew(ctx, lease); err != nil {
		return "", zero, err
	}
	closure, err := c.Store.RetainVerifiedSigningClosure(ctx, lease,
		actor, choice.PolicyID, choice.PolicySHA, slots,
		dependencies, receipts)
	if err != nil { return "", zero, err }
	if err := ctx.Err(); err != nil { return "", zero, err }
	return "adopted", VerifiedDelegatedSigningSet{Actor: actor,
		Frozen: frozen, SnapshotRaw: []byte(proof.SnapshotJCS),
		Slots: slots, Dependencies: dependencies, Receipts: receipts,
		Closure: closure}, nil
}
