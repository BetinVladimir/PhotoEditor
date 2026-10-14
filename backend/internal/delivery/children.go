package delivery

import (
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// ExportChildIntent identifies an export admission, not a completed export.
// ExportID is assigned/verified by existing export admission; never assume that
// an export record ID is the same as its child run ID.
type ExportChildIntent struct {
	SlotIndex      int
	Ordinal        int
	RunID          uuid.UUID
	Binding        v2input.SessionVersionBinding
	Recipe         v2input.Ref
	Preset         FrozenPreset
	SnapshotDigest string
}

type ExportChildPage struct {
	Items      []ExportChildIntent
	NextCursor int
	Done       bool
}

// ExportChildren emits <=100 row-major slots, ordered by snapshot ordinal then
// frozen preset UUID. Cursor counts admitted slots (not snapshot items). Persist
// it only after each preceding child was idempotently admitted under a current
// fenced expansion lease. Cancellation/ACL/quota remain repository duties.
func (p PreparePlan) ExportChildren(parentRun uuid.UUID, cursor, limit int) (ExportChildPage, error) {
	var zero ExportChildPage
	workflow, err := v2input.WorkflowID(parentRun)
	if err != nil || workflow != p.workflow || p.task == uuid.Nil || len(p.items) < 1 || len(p.presets) < 1 ||
		p.preparation.SlotCount != len(p.items)*len(p.presets) || cursor < 0 || cursor > p.preparation.SlotCount || limit < 1 || limit > 100 {
		return zero, v2input.ErrInvalid
	}
	end := cursor + limit
	if end > p.preparation.SlotCount {
		end = p.preparation.SlotCount
	}
	page := ExportChildPage{Items: make([]ExportChildIntent, 0, end-cursor), NextCursor: end, Done: end == p.preparation.SlotCount}
	for index := cursor; index < end; index++ {
		ordinal := index / len(p.presets)
		item := p.items[ordinal]
		preset := p.presets[index%len(p.presets)]
		child := uuid.NewSHA1(parentRun, []byte(fmt.Sprintf("child:export:%d", index)))
		page.Items = append(page.Items, ExportChildIntent{SlotIndex: index, Ordinal: ordinal, RunID: child, Binding: item.Binding, Recipe: item.Recipe, Preset: preset, SnapshotDigest: p.preparation.SnapshotDigest})
	}
	return page, nil
}
