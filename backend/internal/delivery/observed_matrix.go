package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// ChildObservation must come from a scoped repository using current run/export
// and immutable object evidence. This value is not SQL authorization by itself.
type ChildObservation struct {
	SlotIndex int               `json:"slot_index"`
	Export    ExportObservation `json:"export"`
}

// ObserveMatrix binds every observation to the exact frozen row-major intent.
// Partial, duplicate or substituted observations never receive assembly credit.
// New-child export identities match the current admission path; cache reuse
// requires an explicit persisted binding before extending this method.
func (p PreparePlan) ObserveMatrix(ctx context.Context, parent uuid.UUID, observations []ChildObservation) ([]Slot, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if len(observations) != p.preparation.SlotCount || len(observations) < 1 || len(observations) > 80000 {
		return nil, "", v2input.ErrInvalid
	}
	var proof PrepareProof
	var frozen FrozenInput
	if json.Unmarshal(p.proofJCS, &proof) != nil || json.Unmarshal([]byte(proof.FrozenInputJCS), &frozen) != nil {
		return nil, "", v2input.ErrInvalid
	}
	slots := make([]Slot, 0, len(observations))
	for cursor := 0; cursor < len(observations); {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		page, err := p.ExportChildren(parent, cursor, 100)
		if err != nil {
			return nil, "", err
		}
		for _, child := range page.Items {
			observed := observations[child.SlotIndex]
			if observed.SlotIndex != child.SlotIndex {
				return nil, "", v2input.ErrInvalid
			}
			exportID, err := p.NewChildExportID(parent, child)
			if err != nil {
				return nil, "", err
			}
			expected := SlotExpectation{SnapshotDigest: child.SnapshotDigest, Ordinal: child.Ordinal, Binding: child.Binding, Preset: child.Preset, ExportID: exportID}
			slot, err := ObserveExport(expected, observed.Export, frozen.Policy)
			if err != nil {
				return nil, "", err
			}
			slots = append(slots, slot)
		}
		cursor = page.NextCursor
	}
	state, err := MatrixState(slots)
	return slots, state, err
}

// BuildObservedAssemblyInput publishes only after the entire observed matrix is
// terminal and valid. The publisher must fence authority again for each write.
func (p PreparePlan) BuildObservedAssemblyInput(ctx context.Context, parent uuid.UUID, observations []ChildObservation, publish PublishDocument) (AssemblyInput, []byte, string, error) {
	slots, state, err := p.ObserveMatrix(ctx, parent, observations)
	if err != nil {
		return AssemblyInput{}, nil, "", err
	}
	if state != "assemble" {
		return AssemblyInput{}, nil, "", domain.NewError("DELIVERY_MATRIX_NOT_READY", 409)
	}
	var proof PrepareProof
	var frozen FrozenInput
	if json.Unmarshal(p.proofJCS, &proof) != nil || json.Unmarshal([]byte(proof.FrozenInputJCS), &frozen) != nil {
		return AssemblyInput{}, nil, "", v2input.ErrInvalid
	}
	return BuildAssemblyInput(ctx, frozen, []byte(proof.SnapshotJCS), slots, publish)
}
