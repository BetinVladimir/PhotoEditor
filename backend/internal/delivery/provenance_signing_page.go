package delivery

import (
	"bytes"
	"context"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// ReadReadySigningPage observes one bounded page under the current parent
// lease. Only ready unsigned exports are returned; each signer admission must
// independently repeat its exact slot, source and policy authority in SQL.
func (p PreparePlan) ReadReadySigningPage(ctx context.Context,
	store MatrixObservationStore, lease ExpansionLease, cursor, limit int,
) ([]Slot, int, error) {
	if err := ctx.Err(); err != nil { return nil, 0, err }
	if store == nil || !lease.valid() || lease.ParentTask != p.task ||
		lease.Cursor != lease.SlotCount || lease.SlotCount != p.preparation.SlotCount ||
		cursor < 0 || cursor >= lease.SlotCount || limit < 1 || limit > 100 {
		return nil, 0, v2input.ErrInvalid
	}
	var proof PrepareProof
	var frozen FrozenInput
	if json.Unmarshal(p.proofJCS, &proof) != nil ||
		json.Unmarshal([]byte(proof.FrozenInputJCS), &frozen) != nil ||
		frozen.Policy.Provenance != "required" {
		return nil, 0, v2input.ErrInvalid
	}
	page, err := p.ExportChildren(lease.Run, cursor, limit)
	if err != nil { return nil, 0, err }
	read := func() ([]ChildObservation, error) {
		if err := store.Renew(ctx, lease); err != nil { return nil, err }
		values, err := store.ReadObservationPage(ctx, lease, p, cursor, limit)
		if err != nil { return nil, err }
		if len(values) != len(page.Items) { return nil, v2input.ErrInvalid }
		for i, value := range values {
			if value.SlotIndex != page.Items[i].SlotIndex {
				return nil, v2input.ErrInvalid
			}
			if value.Export.Artifact != nil {
				owned := *value.Export.Artifact
				values[i].Export.Artifact = &owned
			}
		}
		return values, ctx.Err()
	}
	first, err := read()
	if err != nil { return nil, 0, err }
	second, err := read()
	if err != nil { return nil, 0, err }
	a, err := canonical.Marshal(first)
	if err != nil { return nil, 0, err }
	b, err := canonical.Marshal(second)
	if err != nil { return nil, 0, err }
	if !bytes.Equal(a, b) {
		return nil, 0, domain.NewError("DELIVERY_MATRIX_CHANGED", 409)
	}
	ready := make([]Slot, 0, len(page.Items))
	for i, child := range page.Items {
		if err := ctx.Err(); err != nil { return nil, 0, err }
		exportID, err := p.NewChildExportID(lease.Run, child)
		if err != nil { return nil, 0, err }
		expected := SlotExpectation{SnapshotDigest: child.SnapshotDigest,
			Ordinal: child.Ordinal, Binding: child.Binding,
			Preset: child.Preset, ExportID: exportID}
		slot, err := ObserveExport(expected, second[i].Export, frozen.Policy)
		if err != nil { return nil, 0, err }
		if slot.State == "ready" { ready = append(ready, slot) }
	}
	if err := store.Renew(ctx, lease); err != nil { return nil, 0, err }
	return ready, page.NextCursor, nil
}
