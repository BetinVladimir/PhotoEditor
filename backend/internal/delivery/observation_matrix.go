package delivery

import (
	"bytes"
	"context"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// MatrixObservationStore must enforce the parent lease and exact slot identity
// on each read. A gathered matrix is local evidence, not publication authority.
type MatrixObservationStore interface {
	Renew(context.Context, ExpansionLease) error
	ReadObservationPage(context.Context, ExpansionLease, PreparePlan, int, int) ([]ChildObservation, error)
}

// ReadObservedMatrix performs two complete passes so a changing child cannot be
// silently combined with old observations. SQL publication still must repeat
// authority under locks, closing the remaining read-to-publication race.
// Renew each bounded page and propagate cancellation/errors without any writes
// to assembly documents. A pending matrix skips the redundant second pass.
func (p PreparePlan) ReadObservedMatrix(ctx context.Context, store MatrixObservationStore, lease ExpansionLease) ([]ChildObservation, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if store == nil || !lease.valid() || lease.ParentTask != p.task || lease.Cursor != lease.SlotCount || lease.SlotCount != p.preparation.SlotCount {
		return nil, "", v2input.ErrInvalid
	}
	read := func() ([]ChildObservation, error) {
		observations := make([]ChildObservation, 0, lease.SlotCount)
		for cursor := 0; cursor < lease.SlotCount; {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := store.Renew(ctx, lease); err != nil {
				return nil, err
			}
			page, err := p.ExportChildren(lease.Run, cursor, 100)
			if err != nil {
				return nil, err
			}
			values, err := store.ReadObservationPage(ctx, lease, p, cursor, 100)
			if err != nil {
				return nil, err
			}
			if len(values) != len(page.Items) {
				return nil, v2input.ErrInvalid
			}
			for index, value := range values {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if value.SlotIndex != page.Items[index].SlotIndex {
					return nil, v2input.ErrInvalid
				}
				if value.Export.Artifact != nil {
					owned := *value.Export.Artifact
					value.Export.Artifact = &owned
				}
				observations = append(observations, value)
			}
			cursor = page.NextCursor
		}
		return observations, nil
	}
	first, err := read()
	if err != nil {
		return nil, "", err
	}
	_, state, err := p.ObserveMatrix(ctx, lease.Run, first)
	if err != nil {
		return nil, "", err
	}
	if state != "assemble" {
		return first, state, nil
	}
	second, err := read()
	if err != nil {
		return nil, "", err
	}
	a, err := canonical.Marshal(first)
	if err != nil {
		return nil, "", err
	}
	b, err := canonical.Marshal(second)
	if err != nil {
		return nil, "", err
	}
	if !bytes.Equal(a, b) {
		return nil, "", domain.NewError("DELIVERY_MATRIX_CHANGED", 409)
	}
	if err := store.Renew(ctx, lease); err != nil {
		return nil, "", err
	}
	return second, state, nil
}
