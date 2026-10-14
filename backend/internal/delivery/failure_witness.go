package delivery

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// FailureWitness identifies one frozen slot whose current terminal observation
// forbids publication. It is local evidence only: mutation must recheck the
// child request, terminal state and omission policy under the SQL lease.
type FailureWitness struct {
	SlotIndex int
	ChildRun  uuid.UUID
	ExportID  uuid.UUID
	Reason    string
}

// FindFailureWitness validates the entire ordered matrix before selecting its
// first failed slot. Pending peers do not erase a terminal required failure.
func (p PreparePlan) FindFailureWitness(ctx context.Context, parent uuid.UUID, observations []ChildObservation) (*FailureWitness, error) {
	slots, state, err := p.ObserveMatrix(ctx, parent, observations)
	if err != nil {
		return nil, err
	}
	if state != "failed" {
		return nil, nil
	}
	for index, slot := range slots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if slot.State != "failed" {
			continue
		}
		if slot.Reason == nil || !reasonPattern.MatchString(*slot.Reason) {
			return nil, v2input.ErrInvalid
		}
		page, err := p.ExportChildren(parent, index, 1)
		if err != nil {
			return nil, err
		}
		if len(page.Items) != 1 || page.Items[0].SlotIndex != index {
			return nil, v2input.ErrInvalid
		}
		child := page.Items[0]
		export, err := p.NewChildExportID(parent, child)
		if err != nil {
			return nil, err
		}
		return &FailureWitness{SlotIndex: index, ChildRun: child.RunID, ExportID: export, Reason: *slot.Reason}, nil
	}
	return nil, v2input.ErrInvalid
}
