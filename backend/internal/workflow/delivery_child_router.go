package workflow

import (
	"context"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// DeliveryChildRouter supports mixed frozen preset sets. A rejected request is
// never retried through another protocol. Each pipeline retains its own gates.
type DeliveryChildRouter struct {
	Ordinary          DeliveryChildProcessor
	Watermark         DeliveryChildProcessor
	OrdinaryObserver  DeliveryChildObserver
	WatermarkObserver DeliveryChildObserver
}

func (r DeliveryChildRouter) ProcessChild(ctx context.Context, l delivery.ExpansionLease, p delivery.PreparePlan, c delivery.ExportChildIntent) (DeliveryChildAdmission, error) {
	if err := ctx.Err(); err != nil {
		return DeliveryChildAdmission{}, err
	}
	watermark, err := p.ChildUsesWatermark(l.Run, c)
	if err != nil {
		return DeliveryChildAdmission{}, err
	}
	target := r.Ordinary
	if watermark {
		target = r.Watermark
	}
	if target == nil {
		return DeliveryChildAdmission{}, v2input.ErrInvalid
	}
	return target.ProcessChild(ctx, l, p, c)
}
func (r DeliveryChildRouter) ObserveChild(ctx context.Context, l delivery.ExpansionLease, p delivery.PreparePlan, c delivery.ExportChildIntent) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	watermark, err := p.ChildUsesWatermark(l.Run, c)
	if err != nil {
		return false, err
	}
	target := r.OrdinaryObserver
	if watermark {
		target = r.WatermarkObserver
	}
	if target == nil {
		return false, v2input.ErrInvalid
	}
	return target.ObserveChild(ctx, l, p, c)
}
func (r DeliveryChildRouter) PendingSlots(ctx context.Context, l delivery.ExpansionLease, limit int) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.OrdinaryObserver == nil || r.WatermarkObserver == nil || limit < 1 || limit > 100 || l.Cursor < 0 || l.Cursor > l.SlotCount {
		return nil, v2input.ErrInvalid
	}
	ordinary, err := r.OrdinaryObserver.PendingSlots(ctx, l, limit)
	if err != nil {
		return nil, err
	}
 if err:=ctx.Err();err!=nil{return nil,err}
	watermark, err := r.WatermarkObserver.PendingSlots(ctx, l, limit)
	if err != nil {
		return nil, err
	}
 if err:=ctx.Err();err!=nil{return nil,err}
	return mergeDeliveryPendingSlots(ordinary, watermark, l.Cursor, limit)
}
func mergeDeliveryPendingSlots(ordinary, watermark []int, cursor, limit int) ([]int, error) {
	if cursor < 0 || limit < 1 || limit > 100 {
		return nil, v2input.ErrInvalid
	}
	seen := map[int]bool{}
	for _, page := range [][]int{ordinary, watermark} {
		if len(page) > limit {
			return nil, v2input.ErrInvalid
		}
		previous := -1
		for _, slot := range page {
			if slot <= previous || slot < 0 || slot >= cursor || seen[slot] {
				return nil, v2input.ErrInvalid
			}
			previous = slot
			seen[slot] = true
		}
	}
	slots := make([]int, 0, len(seen))
	for slot := range seen {
		slots = append(slots, slot)
	}
	sort.Ints(slots)
	if len(slots) > limit {
		slots = slots[:limit]
	}
	return slots, nil
}

var _ DeliveryChildProcessor = DeliveryChildRouter{}
var _ DeliveryChildObserver = DeliveryChildRouter{}
