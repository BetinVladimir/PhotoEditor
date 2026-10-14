package delivery

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// VerifyDelegatedSigningDependencyCurrent independently opens the exact
// adopted signed export and C2PA manifest, bracketing object IO with current
// lease-scoped receipt and live operator-policy checks. It grants no package
// publication authority; assembly admission must repeat the closure gate.
func VerifyDelegatedSigningDependencyCurrent(ctx context.Context,
	lease ExpansionLease, actor uuid.UUID, slot Slot,
	dependency SigningDependency, policySHA string,
	registry *provenance.SigningPolicyRegistry, clock func() time.Time,
	objects SignedDependencyObjectReader, read SignedDependencyRead,
) (SignedDependencyReceipt, error) {
	var zero SignedDependencyReceipt
	if err := ctx.Err(); err != nil { return zero, err }
	if !lease.valid() || lease.Cursor != lease.SlotCount ||
		actor == uuid.Nil || slot.State != "ready" || slot.ExportID == nil ||
		slot.Export == nil || dependency.ExportID != *slot.ExportID ||
		dependency.Export != *slot.Export ||
		dependency.Ordinal != slot.Ordinal || dependency.PresetID != slot.PresetID ||
		dependency.IdempotencyKey != uuid.NewSHA1(lease.Run,
			[]byte(fmt.Sprintf("%d:%s", slot.Ordinal, slot.PresetID))).String() ||
		dependency.PolicyID == uuid.Nil || !shaPattern.MatchString(policySHA) ||
		registry == nil || clock == nil || objects == nil || read == nil {
		return zero, v2input.ErrInvalid
	}
	workflow := uuid.NewSHA1(lease.Tenant,
		[]byte("provenance:"+actor.String()+":"+dependency.IdempotencyKey))
	expectedTask := uuid.NewSHA1(workflow, []byte("provenance.sign:0"))
	first, found, err := read(ctx)
	if err != nil { return zero, err }
	if !found || !validSignedDependencyReceipt(dependency, first) ||
		first.TaskID != expectedTask {
		return zero, v2input.ErrInvalid
	}
	if _, err := registry.ResolveAdoptedPolicy(ctx, lease.Tenant,
		dependency.PolicyID, policySHA,
		first.SignedObject.ContentType, clock()); err != nil { return zero, err }
	if err := VerifySignedDependencyObjects(ctx, lease.Tenant, first,
		objects); err != nil { return zero, err }
	current, found, err := read(ctx)
	if err != nil { return zero, err }
	if !found || current != first { return zero, v2input.ErrInvalid }
	if _, err := registry.ResolveAdoptedPolicy(ctx, lease.Tenant,
		dependency.PolicyID, policySHA,
		current.SignedObject.ContentType, clock()); err != nil { return zero, err }
	if err := ctx.Err(); err != nil { return zero, err }
	return current, nil
}
